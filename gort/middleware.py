"""Trusted proxy handling, browser form protection, and request limits."""

import ipaddress
import time
from collections import defaultdict, deque
from urllib.parse import urlsplit

from starlette.responses import JSONResponse, PlainTextResponse


class SecurityMiddleware:
	def __init__(self, app, settings):
		self.app = app
		self.settings = settings
		self.proxies = [
			ipaddress.ip_network(cidr, strict=False)
			for cidr in settings.trusted_proxies.replace(",", " ").split()
		]
		self.attempts = defaultdict(deque)
		self.last_prune = 0.0

	def trusted(self, value):
		try:
			address = ipaddress.ip_address(value)
			if isinstance(address, ipaddress.IPv6Address) and address.ipv4_mapped:
				address = address.ipv4_mapped
			return any(address in network for network in self.proxies)
		except ValueError:
			return False

	async def __call__(self, scope, receive, send):
		if scope["type"] != "http":
			return await self.app(scope, receive, send)
		head = scope["method"] == "HEAD"
		if head:
			scope = {**scope, "method": "GET"}
		headers = {k.decode().lower(): v.decode("latin1") for k, v in scope["headers"]}
		peer = scope.get("client") or ("", 0)
		remote, scheme = peer[0], scope["scheme"]
		if self.trusted(remote):
			if headers.get("x-forwarded-proto") in ("http", "https"):
				scheme = headers["x-forwarded-proto"]
			for hop in reversed(headers.get("x-forwarded-for", "").split(",")):
				if not self.trusted(remote):
					break
				try:
					remote = str(ipaddress.ip_address(hop.strip()))
				except ValueError:
					remote = peer[0]
					break
		scope.setdefault("state", {}).update(remote_ip=remote, scheme=scheme)
		admin = scope["path"] == "/admin" or scope["path"].startswith("/admin/")

		async def secured_send(message):
			if head and message["type"] == "http.response.body":
				message = {**message, "body": b""}
			if message["type"] == "http.response.start":
				extra = [
					(b"x-content-type-options", b"nosniff"),
					(b"x-frame-options", b"DENY"),
					(b"referrer-policy", b"strict-origin-when-cross-origin"),
					(
						b"content-security-policy",
						b"frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
					),
				]
				if admin:
					extra.append((b"cache-control", b"no-store"))
				existing = {k for k, _ in message.get("headers", [])}
				message["headers"] = message.get("headers", []) + [
					item for item in extra if item[0] not in existing
				]
			await send(message)

		unsafe = scope["method"] not in ("GET", "HEAD", "OPTIONS")
		if admin and unsafe:
			origin = headers.get("origin")
			cross_origin = headers.get("sec-fetch-site") == "cross-site"
			if origin:
				parsed = urlsplit(origin)
				cross_origin |= (
					parsed.netloc.lower() != headers.get("host", "").lower()
					or parsed.scheme != scheme
				)
			if cross_origin:
				return await PlainTextResponse("Cross-origin form submission is not allowed.", 403)(
					scope, receive, secured_send
				)

		limited = (
			(scope["path"].startswith("/rest/") and unsafe)
			or (scope["path"] == "/graphql" and scope["method"] == "POST")
			or (scope["path"] == "/admin/login" and unsafe)
		)
		now = time.monotonic()
		if now - self.last_prune > 60:
			self.attempts = defaultdict(
				deque,
				{
					ip: bucket
					for ip, bucket in self.attempts.items()
					if bucket and bucket[-1] > now - 60
				},
			)
			self.last_prune = now
		if limited and self.settings.rate_limit_per_minute > 0:
			bucket = self.attempts[remote]
			while bucket and bucket[0] <= now - 60:
				bucket.popleft()
			if len(bucket) >= self.settings.rate_limit_per_minute:
				return await PlainTextResponse(
					"Rate limit exceeded.", 429, headers={"Retry-After": "60"}
				)(scope, receive, secured_send)
			bucket.append(now)

		# Read bounded request bodies before dispatch, including chunked requests.
		if admin or scope["path"].startswith("/rest/") or scope["path"] == "/graphql":
			body = bytearray()
			while True:
				message = await receive()
				if message["type"] == "http.disconnect":
					return
				body.extend(message.get("body", b""))
				if len(body) > 1 << 20:
					if scope["path"] == "/graphql":
						response = JSONResponse(
							{"errors": [{"message": "Request body too large."}]}, status_code=413
						)
					elif scope["path"].startswith("/rest/"):
						from gort.api import problem_response

						response = problem_response(400, "Request body too large.")
					else:
						response = PlainTextResponse("Request body too large.", 413)
					return await response(scope, receive, secured_send)
				if not message.get("more_body", False):
					break
			delivered = False

			async def replay():
				nonlocal delivered
				if not delivered:
					delivered = True
					return {"type": "http.request", "body": bytes(body), "more_body": False}
				return await receive()

			return await self.app(scope, replay, secured_send)
		return await self.app(scope, receive, secured_send)
