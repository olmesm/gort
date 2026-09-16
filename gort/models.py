"""PostgreSQL records for links, accounts and visit tracking."""

from datetime import UTC, datetime

from sqlalchemy import (
	BigInteger,
	Boolean,
	Column,
	DateTime,
	Index,
	Text,
	UniqueConstraint,
	text,
)
from sqlmodel import Field, SQLModel


def utcnow() -> datetime:
	return datetime.now(UTC)


def timestamp(nullable=False):
	return Column(DateTime(timezone=True), nullable=nullable)


class User(SQLModel, table=True):
	__tablename__ = "users"
	__table_args__ = (
		Index(
			"idx_users_oidc_subject",
			"oidc_subject",
			unique=True,
			postgresql_where=text("oidc_subject IS NOT NULL"),
		),
	)
	id: int | None = Field(default=None, primary_key=True, sa_type=BigInteger)
	username: str = Field(unique=True, sa_type=Text)
	password_hash: str = Field(sa_type=Text)
	role: str = Field(default="user", sa_type=Text)
	created_at: datetime = Field(default_factory=utcnow, sa_column=timestamp())
	auth_source: str = Field(default="local", sa_type=Text)
	oidc_subject: str | None = Field(default=None, sa_type=Text)


class Domain(SQLModel, table=True):
	__tablename__ = "domains"
	id: int | None = Field(default=None, primary_key=True, sa_type=BigInteger)
	authority: str = Field(unique=True, sa_type=Text)
	base_url_redirect: str | None = Field(default=None, sa_type=Text)
	regular_404_redirect: str | None = Field(default=None, sa_type=Text)
	invalid_short_url_redirect: str | None = Field(default=None, sa_type=Text)
	is_default: bool = Field(default=False, sa_type=Boolean)
	created_at: datetime = Field(default_factory=utcnow, sa_column=timestamp())


class ShortURL(SQLModel, table=True):
	__tablename__ = "short_urls"
	__table_args__ = (
		UniqueConstraint("domain_id", "short_code"),
		Index("idx_short_urls_code", "short_code"),
		Index("idx_short_urls_group", "group_name"),
	)
	id: int | None = Field(default=None, primary_key=True, sa_type=BigInteger)
	short_code: str = Field(sa_type=Text)
	domain_id: int = Field(
		foreign_key="domains.id",
		ondelete="CASCADE",
		sa_type=BigInteger,
	)
	long_url: str = Field(sa_type=Text)
	title: str | None = Field(default=None, sa_type=Text)
	title_was_auto_resolved: bool = Field(default=False, sa_type=Boolean)
	redirect_status: int = 302
	forward_query: bool = Field(default=True, sa_type=Boolean)
	crawlable: bool = Field(default=False, sa_type=Boolean)
	max_visits: int | None = Field(default=None, sa_type=BigInteger)
	valid_since: datetime | None = Field(default=None, sa_column=timestamp(True))
	valid_until: datetime | None = Field(default=None, sa_column=timestamp(True))
	author_user_id: int | None = Field(
		default=None,
		foreign_key="users.id",
		ondelete="SET NULL",
		sa_type=BigInteger,
	)
	author_api_key_id: int | None = Field(default=None, sa_type=BigInteger)
	group_name: str | None = Field(default=None, sa_type=Text)
	created_at: datetime = Field(default_factory=utcnow, sa_column=timestamp())


class Tag(SQLModel, table=True):
	__tablename__ = "tags"
	id: int | None = Field(default=None, primary_key=True, sa_type=BigInteger)
	name: str = Field(unique=True, sa_type=Text)


class ShortURLTag(SQLModel, table=True):
	__tablename__ = "short_url_tags"
	short_url_id: int = Field(
		primary_key=True,
		foreign_key="short_urls.id",
		ondelete="CASCADE",
		sa_type=BigInteger,
	)
	tag_id: int = Field(
		primary_key=True,
		foreign_key="tags.id",
		ondelete="CASCADE",
		sa_type=BigInteger,
	)


class RedirectRule(SQLModel, table=True):
	__tablename__ = "redirect_rules"
	id: int | None = Field(default=None, primary_key=True, sa_type=BigInteger)
	short_url_id: int = Field(
		foreign_key="short_urls.id",
		ondelete="CASCADE",
		sa_type=BigInteger,
	)
	priority: int
	long_url: str = Field(sa_type=Text)


class RedirectCondition(SQLModel, table=True):
	__tablename__ = "redirect_conditions"
	id: int | None = Field(default=None, primary_key=True, sa_type=BigInteger)
	rule_id: int = Field(
		foreign_key="redirect_rules.id",
		ondelete="CASCADE",
		sa_type=BigInteger,
	)
	cond_type: str = Field(sa_type=Text)
	match_key: str | None = Field(default=None, sa_type=Text)
	match_value: str = Field(sa_type=Text)


class Visit(SQLModel, table=True):
	__tablename__ = "visits"
	__table_args__ = (
		Index("idx_visits_short_url", "short_url_id", "visited_at"),
		Index("idx_visits_type", "visit_type", "visited_at"),
	)
	id: int | None = Field(default=None, primary_key=True, sa_type=BigInteger)
	short_url_id: int | None = Field(
		default=None,
		foreign_key="short_urls.id",
		ondelete="CASCADE",
		sa_type=BigInteger,
	)
	visit_type: str = Field(default="valid", sa_type=Text)
	visited_at: datetime = Field(default_factory=utcnow, sa_column=timestamp())
	referer: str | None = Field(default=None, sa_type=Text)
	user_agent: str | None = Field(default=None, sa_type=Text)
	browser: str | None = Field(default=None, sa_type=Text)
	os: str | None = Field(default=None, sa_type=Text)
	device: str | None = Field(default=None, sa_type=Text)
	is_bot: bool = Field(default=False, sa_type=Boolean)
	remote_ip: str | None = Field(default=None, sa_type=Text)
	visited_url: str | None = Field(default=None, sa_type=Text)


class APIKey(SQLModel, table=True):
	__tablename__ = "api_keys"
	id: int | None = Field(default=None, primary_key=True, sa_type=BigInteger)
	key_hash: str = Field(unique=True, sa_type=Text)
	name: str | None = Field(default=None, sa_type=Text)
	role: str = Field(default="admin", sa_type=Text)
	domain_id: int | None = Field(
		default=None,
		foreign_key="domains.id",
		ondelete="CASCADE",
		sa_type=BigInteger,
	)
	enabled: bool = Field(default=True, sa_type=Boolean)
	expires_at: datetime | None = Field(default=None, sa_column=timestamp(True))
	created_at: datetime = Field(default_factory=utcnow, sa_column=timestamp())


class Webhook(SQLModel, table=True):
	__tablename__ = "webhooks"
	id: int | None = Field(default=None, primary_key=True, sa_type=BigInteger)
	name: str = Field(sa_type=Text)
	url: str = Field(sa_type=Text)
	secret: str = Field(sa_type=Text)
	events: str = Field(sa_type=Text)
	enabled: bool = Field(default=True, sa_type=Boolean)
	created_at: datetime = Field(default_factory=utcnow, sa_column=timestamp())


class WebhookDelivery(SQLModel, table=True):
	__tablename__ = "webhook_deliveries"
	__table_args__ = (Index("idx_webhook_deliveries_due", "status", "next_attempt_at"),)
	id: int | None = Field(default=None, primary_key=True, sa_type=BigInteger)
	webhook_id: int = Field(
		foreign_key="webhooks.id",
		ondelete="CASCADE",
		sa_type=BigInteger,
	)
	event: str = Field(sa_type=Text)
	payload: str = Field(sa_type=Text)
	attempts: int = 0
	next_attempt_at: datetime = Field(default_factory=utcnow, sa_column=timestamp())
	status: str = Field(default="pending", sa_type=Text)
	last_error: str | None = Field(default=None, sa_type=Text)
	created_at: datetime = Field(default_factory=utcnow, sa_column=timestamp())
