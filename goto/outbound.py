"""HTTP requests that resolve and connect to the same validated destination."""

import http.client
import ipaddress
import socket
import ssl
from dataclasses import dataclass
from urllib.parse import urljoin, urlsplit

SPECIAL_NETWORKS = tuple(
	ipaddress.ip_network(value)
	for value in (
		"0.0.0.0/8",
		"100.64.0.0/10",
		"168.63.129.16/32",
		"192.0.0.0/24",
		"192.0.2.0/24",
		"192.88.99.0/24",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"240.0.0.0/4",
		"64:ff9b::/96",
		"64:ff9b:1::/48",
		"100::/64",
		"2001::/23",
		"2001:db8::/32",
		"2002::/16",
	)
)


def public_ip(value: str) -> bool:
	address = ipaddress.ip_address(value)
	if isinstance(address, ipaddress.IPv6Address) and address.ipv4_mapped:
		address = address.ipv4_mapped
	return (
		address.is_global
		and not address.is_multicast
		and not any(address in network for network in SPECIAL_NETWORKS)
	)


def checked_socket(host: str, port: int, timeout: float, allow_private: bool):
	addresses = socket.getaddrinfo(host, port, type=socket.SOCK_STREAM)
	if not addresses:
		raise OSError("No addresses for outbound host")
	if not allow_private and any(not public_ip(item[4][0]) for item in addresses):
		raise ValueError("Outbound destination resolves to a non-public address")
	last_error = None
	for family, kind, proto, _, address in addresses:
		connection = socket.socket(family, kind, proto)
		connection.settimeout(timeout)
		try:
			connection.connect(address)
			return connection
		except OSError as exc:
			connection.close()
			last_error = exc
	raise last_error or OSError("Unable to connect")


class CheckedConnection(http.client.HTTPConnection):
	def __init__(self, host, port, *, secure, timeout, allow_private):
		super().__init__(host, port, timeout=timeout)
		self.secure = secure
		self.allow_private = allow_private

	def connect(self):
		connection = checked_socket(self.host, self.port, self.timeout, self.allow_private)
		try:
			self.sock = (
				ssl.create_default_context().wrap_socket(connection, server_hostname=self.host)
				if self.secure
				else connection
			)
		except Exception:
			connection.close()
			raise


@dataclass(frozen=True)
class OutboundResponse:
	status: int
	headers: dict[str, str]
	body: bytes


def request(
	url: str,
	*,
	method="GET",
	body=None,
	headers=None,
	timeout=10,
	allow_private=False,
	max_bytes=65536,
) -> OutboundResponse:
	headers = dict(headers or {})
	for _ in range(11):
		parsed = urlsplit(url)
		if parsed.scheme not in ("http", "https") or not parsed.hostname or parsed.username:
			raise ValueError("Outbound URL must be an absolute HTTP(S) URL without credentials")
		connection = CheckedConnection(
			parsed.hostname,
			parsed.port or (443 if parsed.scheme == "https" else 80),
			secure=parsed.scheme == "https",
			timeout=timeout,
			allow_private=allow_private,
		)
		try:
			path = parsed.path or "/"
			if parsed.query:
				path += "?" + parsed.query
			connection.request(method, path, body=body, headers=headers)
			response = connection.getresponse()
			response_headers = {k.lower(): v for k, v in response.getheaders()}
			result = OutboundResponse(response.status, response_headers, response.read(max_bytes))
		finally:
			connection.close()
		if result.status not in (301, 302, 303, 307, 308) or "location" not in result.headers:
			return result
		next_url = urljoin(url, result.headers["location"])
		if urlsplit(next_url).netloc != parsed.netloc:
			headers = {
				k: v for k, v in headers.items() if k.lower() not in ("authorization", "cookie")
			}
		if result.status in (301, 302, 303) and method != "HEAD":
			method, body = "GET", None
			headers = {
				k: v
				for k, v in headers.items()
				if k.lower() not in ("content-type", "content-length")
			}
		url = next_url
	raise ValueError("Too many outbound redirects")
