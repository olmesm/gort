"""Validation and redirect decisions, independent of storage and HTTP."""

import re
import secrets
import string
from datetime import UTC, datetime
from ipaddress import ip_address, ip_network
from urllib.parse import quote, urlsplit

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator


class ServiceError(ValueError):
	def __init__(self, message: str, status_code: int = 400, kind: str = "invalid-data"):
		super().__init__(message)
		self.status_code = status_code
		self.kind = kind


def validate_long_url(raw: str) -> str:
	raw = raw.strip()
	if not raw:
		raise ValueError("The long URL is required.")
	try:
		parsed = urlsplit(raw)
		if (
			parsed.scheme not in ("http", "https")
			or not parsed.netloc
			or any(c in raw for c in "\r\n\t")
		):
			raise ValueError()
		_ = parsed.port
	except ValueError:
		raise ValueError("The long URL must be an absolute http(s) URL.") from None
	if len(raw.encode()) > 2048:
		raise ValueError("The long URL cannot exceed 2048 UTF-8 bytes.")
	return raw


def validate_domain(raw: str) -> str:
	raw = raw.strip().lower()
	if not raw:
		raise ValueError("The domain authority is required.")
	try:
		parsed = urlsplit("http://" + raw)
		_ = parsed.port
		if (
			not parsed.hostname
			or parsed.netloc != raw
			or parsed.username is not None
			or re.search(r"[\s/?#]", raw)
		):
			raise ValueError()
	except ValueError:
		raise ValueError(
			"Use a hostname with an optional port, without a scheme or path."
		) from None
	return raw


def validate_slug(raw: str) -> str:
	raw = raw.strip().strip("/")
	if not raw:
		raise ValueError("Custom slug cannot be empty.")
	if len(raw) > 255:
		raise ValueError("Custom slug cannot be longer than 255 characters.")
	if "//" in raw:
		raise ValueError("Custom slug cannot contain empty path segments.")
	if not re.fullmatch(r"[a-zA-Z0-9_.~+/-]+", raw):
		raise ValueError("Custom slug contains an invalid character.")
	return raw


def generate_short_code(length: int = 5) -> str:
	return "".join(
		secrets.choice(string.digits + string.ascii_letters) for _ in range(max(4, length))
	)


def normalize_group(raw: str) -> str:
	return raw.strip().removeprefix("/")


def normalize_groups(groups: list[str]) -> list[str]:
	return list(dict.fromkeys(g for item in groups if (g := normalize_group(item))))


def validate_group(raw: str | None) -> str | None:
	if raw is None or not (value := normalize_group(raw)):
		return None
	if len(value.encode()) > 255 or "," in value:
		raise ValueError("Group names cannot contain commas or exceed 255 UTF-8 bytes.")
	return value


def validate_tag(raw: str) -> str:
	value = raw.strip().lower()
	if not value or len(value.encode()) > 255 or "," in value:
		raise ValueError(
			"Tag names cannot be empty or contain commas, and cannot exceed 255 UTF-8 bytes."
		)
	return value


def normalize_tags(tags: list[str]) -> list[str]:
	return list(dict.fromkeys(validate_tag(tag) for tag in tags))


def parse_date(value: str | datetime | None) -> datetime | None:
	if value is None or value == "":
		return None
	parsed = (
		value
		if isinstance(value, datetime)
		else datetime.fromisoformat(value.replace("Z", "+00:00"))
	)
	return parsed.replace(tzinfo=UTC) if parsed.tzinfo is None else parsed.astimezone(UTC)


class ShortURLSpec(BaseModel):
	model_config = ConfigDict(populate_by_name=True, extra="ignore")
	long_url: str = Field(alias="longUrl")
	custom_slug: str | None = Field(default=None, alias="customSlug")
	short_code_length: int | None = Field(default=None, alias="shortCodeLength")
	domain: str | None = None
	title: str | None = None
	tags: list[str] = Field(default_factory=list)
	group: str | None = None
	valid_since: datetime | None = Field(default=None, alias="validSince")
	valid_until: datetime | None = Field(default=None, alias="validUntil")
	max_visits: int | None = Field(default=None, alias="maxVisits", gt=0)
	redirect_status: int = Field(default=302, alias="redirectStatus")
	forward_query: bool = Field(default=True, alias="forwardQuery")
	crawlable: bool = False
	find_if_exists: bool = Field(default=False, alias="findIfExists")

	_url = field_validator("long_url")(validate_long_url)
	_tags = field_validator("tags")(normalize_tags)
	_group = field_validator("group")(validate_group)

	@field_validator("custom_slug")
	@classmethod
	def slug(cls, value):
		if value is None:
			return value
		value = validate_slug(value)
		if value == "graphql" or value.startswith("graphql/"):
			raise ValueError("The graphql path is reserved for the GraphQL API.")
		return value

	@field_validator("domain")
	@classmethod
	def authority(cls, value):
		return None if value is None else validate_domain(value)

	@field_validator("title")
	@classmethod
	def title_value(cls, value):
		return value.strip() or None if value is not None else None

	@field_validator("valid_since", "valid_until")
	@classmethod
	def utc_value(cls, value):
		return parse_date(value)

	@field_validator("redirect_status")
	@classmethod
	def status(cls, value):
		if value not in (301, 302, 307, 308):
			raise ValueError(
				f"'{value}' is not a supported redirect status. Use 301, 302, 307 or 308."
			)
		return value

	@model_validator(mode="after")
	def lifetime(self):
		if self.valid_since and self.valid_until and self.valid_since >= self.valid_until:
			raise ValueError("validSince must be earlier than validUntil.")
		return self


def check_active(link, now: datetime, visit_count: int) -> str | None:
	if link.valid_since and now < link.valid_since:
		return "not_yet_valid"
	if link.valid_until and now > link.valid_until:
		return "no_longer_valid"
	if link.max_visits is not None and visit_count >= link.max_visits:
		return "max_visits_reached"
	return None


def forward_query(target_url: str, incoming: list[tuple[str, str]]) -> str:
	if not incoming:
		return target_url
	query = "&".join(
		quote(k, safe="~") + ("=" + quote(v, safe="~") if v else "") for k, v in incoming
	)
	base, separator, fragment = target_url.partition("#")
	return base + ("&" if "?" in base else "?") + query + (separator + fragment)


def detect_device(user_agent: str) -> str:
	ua = user_agent.lower()
	if "android" in ua:
		return "android"
	if any(s in ua for s in ("iphone", "ipad", "ipod")):
		return "ios"
	return "mobile" if "mobile" in ua else "desktop"


def matches_language(wanted: str, accept_language: str) -> bool:
	wanted = wanted.strip().lower()
	for part in accept_language.split(","):
		lang = part.split(";")[0].strip().lower()
		if (
			lang
			and lang != "*"
			and (lang == wanted or "-" not in wanted and lang.split("-")[0] == wanted)
		):
			return True
	return False


def _canonical_ip(raw):
	ip = ip_address(raw)
	return getattr(ip, "ipv4_mapped", None) or ip


def ip_in_cidr(cidr: str, raw: str) -> bool:
	try:
		ip = _canonical_ip(raw)
		if "/" not in cidr:
			return ip == _canonical_ip(cidr)
		address, prefix = cidr.split("/")
		return ip in ip_network(f"{_canonical_ip(address)}/{prefix}", strict=False)
	except ValueError:
		return False


def anonymize_ip(raw: str) -> str:
	try:
		ip = _canonical_ip(raw)
		return str(
			ip_network(f"{ip}/{24 if ip.version == 4 else 48}", strict=False).network_address
		)
	except ValueError:
		return ""


def resolve_target(
	default_long_url: str,
	rules: list[dict],
	user_agent: str = "",
	accept_language: str = "",
	query: dict | None = None,
	remote_ip: str = "",
) -> str:
	query = query or {}
	device = detect_device(user_agent)

	def matches(condition):
		kind, value = condition["type"], condition["matchValue"]
		if kind == "device":
			return device == value or value == "mobile" and device in ("android", "ios", "mobile")
		if kind == "language":
			return matches_language(value, accept_language)
		if kind == "query-param":
			return query.get(condition.get("matchKey")) == value
		return kind == "ip-address" and ip_in_cidr(value, remote_ip)

	for rule in sorted(rules, key=lambda r: r["priority"]):
		if rule["conditions"] and all(matches(c) for c in rule["conditions"]):
			return rule["longUrl"]
	return default_long_url
