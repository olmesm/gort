"""Initial PostgreSQL schema."""

import sqlalchemy as sa

from alembic import op

revision = "0001_initial"
down_revision = None
branch_labels = None
depends_on = None


def upgrade():
	op.create_table(
		"domains",
		sa.Column("id", sa.BigInteger(), nullable=False),
		sa.Column("authority", sa.Text(), nullable=False),
		sa.Column("base_url_redirect", sa.Text(), nullable=True),
		sa.Column("regular_404_redirect", sa.Text(), nullable=True),
		sa.Column("invalid_short_url_redirect", sa.Text(), nullable=True),
		sa.Column("is_default", sa.Boolean(), nullable=False),
		sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
		sa.PrimaryKeyConstraint("id"),
		sa.UniqueConstraint("authority"),
	)
	op.create_table(
		"tags",
		sa.Column("id", sa.BigInteger(), nullable=False),
		sa.Column("name", sa.Text(), nullable=False),
		sa.PrimaryKeyConstraint("id"),
		sa.UniqueConstraint("name"),
	)
	op.create_table(
		"users",
		sa.Column("id", sa.BigInteger(), nullable=False),
		sa.Column("username", sa.Text(), nullable=False),
		sa.Column("password_hash", sa.Text(), nullable=False),
		sa.Column("role", sa.Text(), nullable=False),
		sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
		sa.Column("auth_source", sa.Text(), nullable=False),
		sa.Column("oidc_subject", sa.Text(), nullable=True),
		sa.PrimaryKeyConstraint("id"),
		sa.UniqueConstraint("username"),
	)
	op.create_index(
		"idx_users_oidc_subject",
		"users",
		["oidc_subject"],
		unique=True,
		postgresql_where=sa.text("oidc_subject IS NOT NULL"),
	)
	op.create_table(
		"webhooks",
		sa.Column("id", sa.BigInteger(), nullable=False),
		sa.Column("name", sa.Text(), nullable=False),
		sa.Column("url", sa.Text(), nullable=False),
		sa.Column("secret", sa.Text(), nullable=False),
		sa.Column("events", sa.Text(), nullable=False),
		sa.Column("enabled", sa.Boolean(), nullable=False),
		sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
		sa.PrimaryKeyConstraint("id"),
	)
	op.create_table(
		"api_keys",
		sa.Column("id", sa.BigInteger(), nullable=False),
		sa.Column("key_hash", sa.Text(), nullable=False),
		sa.Column("name", sa.Text(), nullable=True),
		sa.Column("role", sa.Text(), nullable=False),
		sa.Column("domain_id", sa.BigInteger(), nullable=True),
		sa.Column("enabled", sa.Boolean(), nullable=False),
		sa.Column("expires_at", sa.DateTime(timezone=True), nullable=True),
		sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
		sa.ForeignKeyConstraint(["domain_id"], ["domains.id"], ondelete="CASCADE"),
		sa.PrimaryKeyConstraint("id"),
		sa.UniqueConstraint("key_hash"),
	)
	op.create_table(
		"short_urls",
		sa.Column("id", sa.BigInteger(), nullable=False),
		sa.Column("short_code", sa.Text(), nullable=False),
		sa.Column("domain_id", sa.BigInteger(), nullable=False),
		sa.Column("long_url", sa.Text(), nullable=False),
		sa.Column("title", sa.Text(), nullable=True),
		sa.Column("title_was_auto_resolved", sa.Boolean(), nullable=False),
		sa.Column("redirect_status", sa.Integer(), nullable=False),
		sa.Column("forward_query", sa.Boolean(), nullable=False),
		sa.Column("crawlable", sa.Boolean(), nullable=False),
		sa.Column("max_visits", sa.BigInteger(), nullable=True),
		sa.Column("valid_since", sa.DateTime(timezone=True), nullable=True),
		sa.Column("valid_until", sa.DateTime(timezone=True), nullable=True),
		sa.Column("author_user_id", sa.BigInteger(), nullable=True),
		sa.Column("author_api_key_id", sa.BigInteger(), nullable=True),
		sa.Column("group_name", sa.Text(), nullable=True),
		sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
		sa.ForeignKeyConstraint(["author_user_id"], ["users.id"], ondelete="SET NULL"),
		sa.ForeignKeyConstraint(["domain_id"], ["domains.id"], ondelete="CASCADE"),
		sa.PrimaryKeyConstraint("id"),
		sa.UniqueConstraint("domain_id", "short_code"),
	)
	op.create_index("idx_short_urls_code", "short_urls", ["short_code"], unique=False)
	op.create_index("idx_short_urls_group", "short_urls", ["group_name"], unique=False)
	op.create_table(
		"webhook_deliveries",
		sa.Column("id", sa.BigInteger(), nullable=False),
		sa.Column("webhook_id", sa.BigInteger(), nullable=False),
		sa.Column("event", sa.Text(), nullable=False),
		sa.Column("payload", sa.Text(), nullable=False),
		sa.Column("attempts", sa.Integer(), nullable=False),
		sa.Column("next_attempt_at", sa.DateTime(timezone=True), nullable=False),
		sa.Column("status", sa.Text(), nullable=False),
		sa.Column("last_error", sa.Text(), nullable=True),
		sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
		sa.ForeignKeyConstraint(["webhook_id"], ["webhooks.id"], ondelete="CASCADE"),
		sa.PrimaryKeyConstraint("id"),
	)
	op.create_index(
		"idx_webhook_deliveries_due",
		"webhook_deliveries",
		["status", "next_attempt_at"],
		unique=False,
	)
	op.create_table(
		"redirect_rules",
		sa.Column("id", sa.BigInteger(), nullable=False),
		sa.Column("short_url_id", sa.BigInteger(), nullable=False),
		sa.Column("priority", sa.Integer(), nullable=False),
		sa.Column("long_url", sa.Text(), nullable=False),
		sa.ForeignKeyConstraint(["short_url_id"], ["short_urls.id"], ondelete="CASCADE"),
		sa.PrimaryKeyConstraint("id"),
	)
	op.create_table(
		"short_url_tags",
		sa.Column("short_url_id", sa.BigInteger(), nullable=False),
		sa.Column("tag_id", sa.BigInteger(), nullable=False),
		sa.ForeignKeyConstraint(["short_url_id"], ["short_urls.id"], ondelete="CASCADE"),
		sa.ForeignKeyConstraint(["tag_id"], ["tags.id"], ondelete="CASCADE"),
		sa.PrimaryKeyConstraint("short_url_id", "tag_id"),
	)
	op.create_table(
		"visits",
		sa.Column("id", sa.BigInteger(), nullable=False),
		sa.Column("short_url_id", sa.BigInteger(), nullable=True),
		sa.Column("visit_type", sa.Text(), nullable=False),
		sa.Column("visited_at", sa.DateTime(timezone=True), nullable=False),
		sa.Column("referer", sa.Text(), nullable=True),
		sa.Column("user_agent", sa.Text(), nullable=True),
		sa.Column("browser", sa.Text(), nullable=True),
		sa.Column("os", sa.Text(), nullable=True),
		sa.Column("device", sa.Text(), nullable=True),
		sa.Column("is_bot", sa.Boolean(), nullable=False),
		sa.Column("remote_ip", sa.Text(), nullable=True),
		sa.Column("visited_url", sa.Text(), nullable=True),
		sa.ForeignKeyConstraint(["short_url_id"], ["short_urls.id"], ondelete="CASCADE"),
		sa.PrimaryKeyConstraint("id"),
	)
	op.create_index("idx_visits_short_url", "visits", ["short_url_id", "visited_at"], unique=False)
	op.create_index("idx_visits_type", "visits", ["visit_type", "visited_at"], unique=False)
	op.create_table(
		"redirect_conditions",
		sa.Column("id", sa.BigInteger(), nullable=False),
		sa.Column("rule_id", sa.BigInteger(), nullable=False),
		sa.Column("cond_type", sa.Text(), nullable=False),
		sa.Column("match_key", sa.Text(), nullable=True),
		sa.Column("match_value", sa.Text(), nullable=False),
		sa.ForeignKeyConstraint(["rule_id"], ["redirect_rules.id"], ondelete="CASCADE"),
		sa.PrimaryKeyConstraint("id"),
	)


def downgrade():
	op.drop_table("redirect_conditions")
	op.drop_table("visits")
	op.drop_table("short_url_tags")
	op.drop_table("redirect_rules")
	op.drop_table("webhook_deliveries")
	op.drop_table("short_urls")
	op.drop_table("api_keys")
	op.drop_table("webhooks")
	op.drop_table("users")
	op.drop_table("tags")
	op.drop_table("domains")
