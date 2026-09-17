"""Background title lookup and durable webhook delivery."""

import hashlib
import hmac
import html
import json
import logging
import re
import threading
from datetime import timedelta

from sqlalchemy import update
from sqlmodel import Session, select

from goto import outbound
from goto.models import ShortURL, Webhook, WebhookDelivery, utcnow

logger = logging.getLogger(__name__)
TITLE = re.compile(r"<title[^>]*>\s*([^<]{1,512})", re.I)


def enqueue_event(session, settings, event, data):
	if not settings.webhooks_enabled:
		return
	payload = json.dumps(
		{"event": event, "occurredAt": utcnow().isoformat().replace("+00:00", "Z"), "data": data},
		separators=(",", ":"),
	)
	hooks = session.exec(select(Webhook).where(Webhook.enabled.is_(True))).all()
	for hook in hooks:
		if event in [name.strip() for name in hook.events.split(",")]:
			session.add(WebhookDelivery(webhook_id=hook.id, event=event, payload=payload))
	session.commit()


class Workers:
	def __init__(self, engine, settings):
		self.engine = engine
		self.settings = settings
		self.stop_event = threading.Event()
		self.threads = []
		self.title_attempts = set()

	def start(self):
		jobs = []
		if self.settings.auto_resolve_titles:
			jobs.append((self.resolve_titles, 1))
		if self.settings.webhooks_enabled:
			jobs.append((self.deliver_webhooks, 1))
		for job, interval in jobs:
			thread = threading.Thread(
				target=self.loop, args=(job, interval), daemon=True, name=f"goto-{job.__name__}"
			)
			thread.start()
			self.threads.append(thread)

	def loop(self, job, interval):
		while not self.stop_event.is_set():
			try:
				job()
			except Exception:
				logger.exception("Background job %s failed", job.__name__)
			self.stop_event.wait(interval)

	def close(self):
		self.stop_event.set()
		for thread in self.threads:
			thread.join(timeout=20)

	def resolve_titles(self):
		with Session(self.engine) as session:
			links = session.exec(select(ShortURL).where(ShortURL.title.is_(None))).all()
			for link in links:
				identity = (link.id, link.long_url)
				if identity in self.title_attempts or self.stop_event.is_set():
					continue
				self.title_attempts.add(identity)
				try:
					response = outbound.request(
						link.long_url,
						headers={"User-Agent": "goto-title-resolver/1.0"},
						allow_private=self.settings.allow_private_outbound,
					)
					if (
						not 200 <= response.status < 300
						or "html" not in response.headers.get("content-type", "").lower()
					):
						continue
					match = TITLE.search(response.body.decode("utf-8", errors="replace"))
					if not match:
						continue
					title = html.unescape(match[1]).strip()
					if title:
						session.exec(
							update(ShortURL)
							.where(
								ShortURL.id == link.id,
								ShortURL.long_url == link.long_url,
								ShortURL.title.is_(None),
							)
							.values(title=title, title_was_auto_resolved=True)
						)
						session.commit()
				except (OSError, ValueError):
					logger.debug("Could not resolve title for link %s", link.id, exc_info=True)

	def deliver_webhooks(self):
		with Session(self.engine) as session:
			due = session.exec(
				select(WebhookDelivery)
				.where(
					WebhookDelivery.status == "pending", WebhookDelivery.next_attempt_at <= utcnow()
				)
				.order_by(WebhookDelivery.next_attempt_at)
				.limit(50)
			).all()
			for delivery in due:
				if self.stop_event.is_set():
					return
				# Claim with a lease. A stopped process leaves the delivery retryable.
				now = utcnow()
				claimed = session.exec(
					update(WebhookDelivery)
					.where(
						WebhookDelivery.id == delivery.id,
						WebhookDelivery.status == "pending",
						WebhookDelivery.next_attempt_at <= now,
					)
					.values(next_attempt_at=now + timedelta(minutes=5))
				)
				session.commit()
				if not claimed.rowcount:
					continue
				hook = session.get(Webhook, delivery.webhook_id)
				if hook is None:
					continue
				error = None
				try:
					signature = hmac.new(
						hook.secret.encode(), delivery.payload.encode(), hashlib.sha256
					).hexdigest()
					response = outbound.request(
						hook.url,
						method="POST",
						body=delivery.payload.encode(),
						timeout=15,
						headers={
							"Content-Type": "application/json; charset=utf-8",
							"X-Goto-Event": delivery.event,
							"X-Goto-Signature": "sha256=" + signature,
						},
						allow_private=self.settings.allow_private_outbound,
						max_bytes=4096,
					)
					if not 200 <= response.status < 300:
						error = f"HTTP {response.status}"
				except Exception as exc:
					error = str(exc)
				if error is None:
					delivery.status = "delivered"
				else:
					delivery.attempts += 1
					delivery.last_error = error
					delivery.status = "failed" if delivery.attempts >= 6 else "pending"
					delivery.next_attempt_at = utcnow() + timedelta(
						seconds=2**delivery.attempts * 15
					)
				session.add(delivery)
				session.commit()
