"""Dashboard sessions and API-key authentication."""

from __future__ import annotations

import base64
import hashlib
import hmac
import json
import secrets
import time
from dataclasses import dataclass, field
from datetime import UTC, datetime
from pathlib import Path

import bcrypt
from fastapi import Request
from sqlmodel import Session, select
from starlette.responses import Response

from .models import APIKey, User

SESSION_COOKIE = "goto_session"
SESSION_LIFETIME = 14 * 24 * 3600


def hash_password(password: str) -> str:
	return bcrypt.hashpw(password.encode(), bcrypt.gensalt(rounds=10)).decode()


def verify_password(password: str, password_hash: str) -> bool:
	try:
		return len(password.encode()) <= 72 and bcrypt.checkpw(
			password.encode(), password_hash.encode()
		)
	except (ValueError, TypeError):
		return False


def generate_api_key() -> str:
	return "goto_" + secrets.token_urlsafe(32)


def hash_api_key(key: str) -> str:
	return hashlib.sha256(key.encode()).hexdigest()


def normalize_group(group: str) -> str:
	return group.strip().removeprefix("/")


def normalize_groups(groups: list[str]) -> list[str]:
	return list(dict.fromkeys(normalize_group(g) for g in groups if normalize_group(g)))


def safe_return_url(url: str) -> str:
	return (
		url
		if url.startswith("/")
		and not url.startswith("//")
		and not any(c in url for c in "\\\r\n\t")
		else "/admin"
	)


def load_or_create_session_key(data_dir: str | Path) -> bytes:
	path = Path(data_dir) / "keys" / "session.key"
	path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
	try:
		key = path.read_bytes()
		if len(key) >= 32:
			return key
	except FileNotFoundError:
		pass
	key = secrets.token_bytes(32)
	# Exclusive creation prevents workers replacing one another's signing key.
	try:
		with path.open("xb") as stream:
			path.chmod(0o600)
			stream.write(key)
	except FileExistsError:
		key = path.read_bytes()
		if len(key) < 32:
			raise ValueError("Session key must contain at least 32 bytes")
	return key


def session_key(request: Request) -> bytes:
	return request.app.state.session_key


def encode(value: bytes) -> str:
	return base64.urlsafe_b64encode(value).decode().rstrip("=")


def decode(value: str) -> bytes:
	return base64.b64decode(value + "=" * (-len(value) % 4), altchars=b"-_", validate=True)


def sign_payload(key: bytes, payload: dict) -> str:
	raw = json.dumps(payload, separators=(",", ":")).encode()
	return encode(raw) + "." + encode(hmac.digest(key, raw, "sha256"))


def verify_payload(key: bytes, cookie: str) -> dict | None:
	try:
		raw, signature = map(decode, cookie.split(".", 1))
		if not hmac.compare_digest(signature, hmac.digest(key, raw, "sha256")):
			return None
		payload = json.loads(raw)
		return payload if isinstance(payload, dict) else None
	except (ValueError, TypeError):
		return None


def password_version(key: bytes, password_hash: str) -> str:
	return encode(
		hmac.digest(key, b"local-password-version\x00" + password_hash.encode(), "sha256")
	)


@dataclass
class CurrentUser:
	id: int
	username: str
	role: str
	groups: list[str] = field(default_factory=list)

	@property
	def is_admin(self) -> bool:
		return self.role == "admin"

	def can_see_group(self, group: str | None) -> bool:
		return self.is_admin or group is None or normalize_group(group) in self.groups

	def visible_groups(self) -> list[str] | None:
		return None if self.is_admin else self.groups


def current_user(request: Request, session: Session) -> CurrentUser | None:
	payload = verify_payload(session_key(request), request.cookies.get(SESSION_COOKIE, ""))
	if not payload or not isinstance(payload.get("exp"), int) or payload["exp"] <= time.time():
		return None
	uid = payload.get("uid")
	if not isinstance(uid, int):
		return None
	user = session.get(User, uid)
	if not user or user.role not in ("admin", "user"):
		return None
	cfg = request.app.state.settings
	if cfg.oidc_issuer and cfg.oidc_only and user.auth_source != "oidc":
		return None
	if user.auth_source == "oidc":
		if not isinstance(payload.get("oidc_exp"), int) or payload["oidc_exp"] <= time.time():
			return None
	elif user.auth_source == "local":
		if not isinstance(payload.get("pv"), str) or not hmac.compare_digest(
			payload["pv"], password_version(session_key(request), user.password_hash)
		):
			return None
	else:
		return None
	groups = payload.get("g", [])
	if not isinstance(groups, list) or not all(isinstance(g, str) for g in groups):
		return None
	return CurrentUser(user.id, user.username, user.role, normalize_groups(groups))


def sign_in(
	request: Request,
	response: Response,
	user: User,
	groups: list[str] | None = None,
	expires: int | None = None,
) -> None:
	expires = min(
		expires or int(time.time()) + SESSION_LIFETIME, int(time.time()) + SESSION_LIFETIME
	)
	payload = {"uid": user.id, "exp": expires, "g": groups or []}
	if user.auth_source == "oidc":
		payload["oidc_exp"] = expires
	else:
		payload["pv"] = password_version(session_key(request), user.password_hash)
	response.set_cookie(
		SESSION_COOKIE,
		sign_payload(session_key(request), payload),
		max_age=max(1, expires - int(time.time())),
		httponly=True,
		secure=request.app.state.settings.use_https,
		samesite="lax",
	)


def sign_out(request: Request, response: Response) -> None:
	response.delete_cookie(
		SESSION_COOKIE, httponly=True, secure=request.app.state.settings.use_https, samesite="lax"
	)


def authenticate_api_key(request: Request, session: Session) -> APIKey | None:
	raw = request.headers.get("x-api-key", "")
	if not raw:
		authorization = request.headers.get("authorization", "")
		if authorization.lower().startswith("bearer "):
			raw = authorization[7:].strip()
	if not raw:
		return None
	row = session.exec(select(APIKey).where(APIKey.key_hash == hash_api_key(raw))).first()
	if not row or not row.enabled or row.role not in ("admin", "author", "domain"):
		return None
	if row.role == "domain" and row.domain_id is None:
		return None
	if row.expires_at and row.expires_at.replace(tzinfo=UTC) <= datetime.now(UTC):
		return None
	return row
