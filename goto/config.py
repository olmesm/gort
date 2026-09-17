"""Environment configuration shared by the web app and workers."""

from ipaddress import ip_network
from pathlib import Path

from psycopg import ProgrammingError
from psycopg.conninfo import conninfo_to_dict
from pydantic import field_validator, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

from goto.domain import validate_domain


class Settings(BaseSettings):
	model_config = SettingsConfigDict(env_prefix="GOTO_", extra="ignore")

	port: int = 8080
	default_domain: str = ""
	use_https: bool = False
	data_dir: Path = Path("data")
	db_connection: str = "postgresql://localhost/goto"
	short_code_length: int = 5
	redirect_status: int = 302
	webhooks_enabled: bool = False
	auto_resolve_titles: bool = True
	allow_private_outbound: bool = False
	trusted_proxies: str = ""
	disable_tracking: bool = False
	disable_ip_tracking: bool = False
	anonymize_ips: bool = True
	track_skip_param: str = ""
	track_orphan_visits: bool = True
	base_url_redirect: str = ""
	regular_404_redirect: str = ""
	invalid_short_url_redirect: str = ""
	initial_admin_username: str = "admin"
	initial_admin_password: str = ""
	rate_limit_per_minute: int = 120
	oidc_issuer: str = ""
	oidc_client_id: str = ""
	oidc_client_secret: str = ""
	oidc_redirect_url: str = ""
	oidc_scopes: str = "profile email"
	oidc_groups_claim: str = "groups"
	oidc_admin_group: str = "goto-admins"
	oidc_provider_name: str = "SSO"
	oidc_only: bool = False
	workers_enabled: bool = True

	@field_validator("db_connection")
	@classmethod
	def database_connection(cls, value: str) -> str:
		value = value.strip()
		if value.startswith("postgresql+psycopg://"):
			value = value.replace("postgresql+psycopg://", "postgresql://", 1)
		if not value or "://" in value and not value.startswith(("postgresql://", "postgres://")):
			raise ValueError(
				"GOTO_DB_CONNECTION must be a PostgreSQL URL or libpq connection string"
			)
		try:
			conninfo_to_dict(value)
		except ProgrammingError as error:
			raise ValueError(
				"GOTO_DB_CONNECTION must be a PostgreSQL URL or libpq connection string"
			) from error
		return value

	@field_validator("trusted_proxies")
	@classmethod
	def valid_proxies(cls, value: str) -> str:
		for cidr in value.replace(",", " ").split():
			ip_network(cidr, strict=False)
		return value

	@model_validator(mode="after")
	def defaults(self):
		if not self.default_domain:
			self.default_domain = f"localhost:{self.port}"
		self.default_domain = validate_domain(self.default_domain)
		if self.redirect_status not in (301, 302, 307, 308):
			self.redirect_status = 302
		self.short_code_length = max(4, self.short_code_length)
		if self.oidc_issuer and not self.oidc_client_id:
			raise ValueError("GOTO_OIDC_CLIENT_ID is required with GOTO_OIDC_ISSUER")
		return self

	@property
	def default_redirect_status(self) -> int:
		return self.redirect_status

	@property
	def oidc_enabled(self) -> bool:
		return bool(self.oidc_issuer)

	def short_url_base(self, authority: str) -> str:
		return f"{'https' if self.use_https else 'http'}://{authority}"
