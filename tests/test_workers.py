import hashlib
import hmac
from unittest.mock import Mock

import pytest
from sqlmodel import Session

from goto import outbound
from goto.config import Settings
from goto.models import Domain, ShortURL, Webhook, WebhookDelivery, utcnow
from goto.workers import Workers


@pytest.mark.parametrize(
	"address",
	[
		"127.0.0.1",
		"10.0.0.1",
		"::1",
		"::ffff:127.0.0.1",
		"169.254.169.254",
		"168.63.129.16",
		"64:ff9b::a00:1",
	],
)
def test_private_outbound_addresses_rejected(address):
	assert not outbound.public_ip(address)


def test_resolved_ip_is_the_connected_ip(monkeypatch):
	socket = Mock()
	monkeypatch.setattr(
		outbound.socket,
		"getaddrinfo",
		lambda *args, **kwargs: [(2, 1, 6, "", ("93.184.216.34", 443))],
	)
	monkeypatch.setattr(outbound.socket, "socket", lambda *args: socket)
	assert outbound.checked_socket("example.com", 443, 10, False) is socket
	socket.connect.assert_called_once_with(("93.184.216.34", 443))


def test_mixed_dns_answers_rejected_before_connection(monkeypatch):
	monkeypatch.setattr(
		outbound.socket,
		"getaddrinfo",
		lambda *a, **kw: [(2, 1, 6, "", ("93.184.216.34", 443)), (2, 1, 6, "", ("127.0.0.1", 443))],
	)
	socket = Mock()
	monkeypatch.setattr(outbound.socket, "socket", socket)
	with pytest.raises(ValueError):
		outbound.checked_socket("example.com", 443, 10, False)
	socket.assert_not_called()


def test_persistent_webhook_retry_and_signature(tmp_path, monkeypatch, pg_engine, postgres_dsn):
	settings = Settings(data_dir=tmp_path, db_connection=postgres_dsn, webhooks_enabled=True)
	engine = pg_engine
	with Session(engine) as session:
		hook = Webhook(
			name="test", url="https://example.com/hook", secret="secret", events="url.created"
		)
		session.add(hook)
		session.commit()
		delivery = WebhookDelivery(
			webhook_id=hook.id, event="url.created", payload='{"hello":"world"}'
		)
		session.add(delivery)
		session.commit()
		delivery_id = delivery.id
	request = Mock(return_value=outbound.OutboundResponse(500, {}, b""))
	monkeypatch.setattr(outbound, "request", request)
	Workers(engine, settings).deliver_webhooks()
	with Session(engine) as session:
		delivery = session.get(WebhookDelivery, delivery_id)
		assert delivery.attempts == 1 and delivery.status == "pending"
		assert delivery.next_attempt_at > utcnow()
		delivery.next_attempt_at = utcnow()
		session.add(delivery)
		session.commit()
	request.return_value = outbound.OutboundResponse(204, {}, b"")
	Workers(engine, settings).deliver_webhooks()
	with Session(engine) as session:
		assert session.get(WebhookDelivery, delivery_id).status == "delivered"
	kwargs = request.call_args.kwargs
	assert kwargs["headers"]["X-Goto-Event"] == "url.created"
	assert (
		kwargs["headers"]["X-Goto-Signature"]
		== "sha256=" + hmac.new(b"secret", kwargs["body"], hashlib.sha256).hexdigest()
	)
	engine.dispose()


def test_title_resolution_does_not_overwrite_a_concurrent_edit(
	tmp_path, monkeypatch, pg_engine, postgres_dsn
):
	settings = Settings(data_dir=tmp_path, db_connection=postgres_dsn)
	engine = pg_engine
	with Session(engine) as session:
		domain = Domain(authority="example.test", is_default=True)
		session.add(domain)
		session.commit()
		link = ShortURL(domain_id=domain.id, short_code="title", long_url="https://example.com")
		session.add(link)
		session.commit()
		link_id = link.id

	def fetch(*args, **kwargs):
		with Session(engine) as session:
			link = session.get(ShortURL, link_id)
			link.title = "User edited this while the fetch was running"
			session.add(link)
			session.commit()
		return outbound.OutboundResponse(
			200, {"content-type": "text/html"}, b"<title>Fetched title</title>"
		)

	monkeypatch.setattr(outbound, "request", fetch)
	Workers(engine, settings).resolve_titles()
	with Session(engine) as session:
		link = session.get(ShortURL, link_id)
		assert link.title == "User edited this while the fetch was running"
		assert not link.title_was_auto_resolved
	engine.dispose()


def test_webhook_lease_excludes_another_worker(tmp_path, monkeypatch, pg_engine, postgres_dsn):
	settings = Settings(data_dir=tmp_path, db_connection=postgres_dsn, webhooks_enabled=True)
	engine = pg_engine
	with Session(engine) as session:
		hook = Webhook(
			name="lease", url="https://example.com/hook", secret="secret", events="url.created"
		)
		session.add(hook)
		session.commit()
		session.add(WebhookDelivery(webhook_id=hook.id, event="url.created", payload="{}"))
		session.commit()
	second = Workers(engine, settings)

	def concurrent_request(*args, **kwargs):
		second.deliver_webhooks()
		return outbound.OutboundResponse(200, {}, b"")

	request = Mock(side_effect=concurrent_request)
	monkeypatch.setattr(outbound, "request", request)
	Workers(engine, settings).deliver_webhooks()
	assert request.call_count == 1
	engine.dispose()
