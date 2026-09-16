"""PostgreSQL migrations, constraints, and service persistence."""

import os
import subprocess
import sys
from datetime import UTC, datetime, timedelta
from pathlib import Path

import pytest
from sqlalchemy import inspect, text
from sqlmodel import Session, SQLModel, select

from alembic.autogenerate import compare_metadata
from alembic.migration import MigrationContext
from gort.config import Settings
from gort.db import create_engine_and_migrate
from gort.domain import ServiceError
from gort.models import Domain, ShortURL, ShortURLTag, Visit, Webhook, WebhookDelivery
from gort.services import create_short_url, delete_short_url, list_short_urls, update_short_url


def settings(tmp_path, postgres_dsn):
	return Settings(
		data_dir=tmp_path, db_connection=postgres_dsn, webhooks_enabled=True, workers_enabled=False
	)


def test_creation_patch_visibility_and_cascade_are_persistent(tmp_path, postgres_dsn, pg_engine):
	config = settings(tmp_path, postgres_dsn)
	engine = pg_engine
	with Session(engine) as session:
		session.add(Domain(authority=config.default_domain, is_default=True))
		session.add(
			Webhook(name="test", url="https://example.com", secret="secret", events="url.created")
		)
		session.commit()
		start = datetime(2026, 1, 1, tzinfo=UTC)
		link = create_short_url(
			session,
			config,
			{
				"longUrl": "https://example.com/a",
				"customSlug": "hello",
				"group": "/team",
				"tags": ["A", "a"],
				"title": "old",
				"validSince": start,
				"validUntil": start + timedelta(days=2),
			},
		)
		assert session.exec(select(WebhookDelivery)).one().event == "url.created"
		assert list_short_urls(session, config, {}, groups=[])["data"] == []
		assert list_short_urls(session, config, {}, groups=["team"])["data"][0]["tags"] == ["a"]
		with pytest.raises(ServiceError):
			update_short_url(session, config, link, {"validUntil": start - timedelta(days=1)})
		assert link.valid_until == start + timedelta(days=2)
		update_short_url(session, config, link, {"title": None, "group": None, "tags": []})
		assert link.title is None and link.group_name is None
		assert session.exec(select(ShortURLTag)).all() == []
		assert (
			list_short_urls(session, config, {"page": "bad", "itemsPerPage": "0", "group": ""})[
				"pagination"
			]["totalItems"]
			== 1
		)
		session.add(Visit(short_url_id=link.id))
		session.commit()
		link_id = link.id
		delete_short_url(session, link)
		assert session.get(ShortURL, link_id) is None
		assert session.exec(select(Visit)).all() == []
	engine.dispose()


def test_partial_update_preserves_omitted_fields_and_resets_nulls(
	tmp_path, postgres_dsn, pg_engine
):
	config = settings(tmp_path, postgres_dsn)
	engine = pg_engine
	try:
		with Session(engine) as session:
			session.add(Domain(authority=config.default_domain, is_default=True))
			session.commit()
			link = create_short_url(
				session,
				config,
				{
					"longUrl": "https://example.com",
					"customSlug": "nullable",
					"title": "original",
					"group": "team",
					"tags": ["a"],
					"maxVisits": 2,
					"crawlable": True,
				},
			)
			update_short_url(
				session, config, link, {"maxVisits": None, "forwardQuery": None, "crawlable": None}
			)
			assert link.max_visits is None
			assert link.forward_query is False and link.crawlable is False
			assert link.title == "original" and link.group_name == "team"
			assert list_short_urls(session, config, {})["data"][0]["tags"] == ["a"]
			for payload in ({"longUrl": None}, {"redirectStatus": None}):
				with pytest.raises(ServiceError):
					update_short_url(session, config, link, payload)
			assert link.long_url == "https://example.com" and link.redirect_status == 302
	finally:
		engine.dispose()


def test_postgres_migrations_and_utc_timestamps(pg_engine):
	instant = datetime(2026, 1, 2, 3, 4, 5, 123456, tzinfo=UTC)
	with Session(pg_engine) as session:
		session.add(Domain(authority="example.test", created_at=instant))
		session.commit()
		assert session.exec(select(Domain)).one().created_at == instant
	with pg_engine.connect() as connection:
		assert connection.execute(text("SELECT created_at FROM domains")).scalar() == instant
		assert (
			connection.execute(text("SELECT version_num FROM alembic_version")).scalar()
			== "0001_initial"
		)
	column = next(c for c in inspect(pg_engine).get_columns("domains") if c["name"] == "created_at")
	assert column["type"].timezone


def test_migrations_are_idempotent_and_preserve_records(tmp_path, postgres_dsn, pg_engine):
	with Session(pg_engine) as session:
		session.add(Domain(authority="preserved.test"))
		session.commit()
	second = create_engine_and_migrate(settings(tmp_path, postgres_dsn))
	try:
		with Session(second) as session:
			assert session.exec(select(Domain)).one().authority == "preserved.test"
	finally:
		second.dispose()


def test_alembic_metadata_matches_database(pg_engine):
	with pg_engine.connect() as connection:
		context = MigrationContext.configure(connection)
		assert compare_metadata(context, SQLModel.metadata) == []


def test_alembic_cli_uses_configured_database_and_detects_no_changes(tmp_path, postgres_dsn):
	root = Path(__file__).parent.parent
	env = {**os.environ, "GORT_DB_CONNECTION": postgres_dsn, "PYTHONPATH": str(root)}
	for command in (["upgrade", "head"], ["check"]):
		result = subprocess.run(
			[sys.executable, "-m", "alembic", "-c", str(root / "alembic.ini"), *command],
			cwd=tmp_path,
			env=env,
			capture_output=True,
			text=True,
			timeout=30,
		)
		assert result.returncode == 0, result.stdout + result.stderr
	assert not (tmp_path / "data").exists()


def test_environment_connection_options_preserve_schema(
	tmp_path, postgres_admin_dsn, postgres_dsn, pg_engine, monkeypatch
):
	from psycopg.conninfo import conninfo_to_dict

	options = conninfo_to_dict(postgres_dsn)["options"]
	monkeypatch.setenv("PGOPTIONS", options)
	with pg_engine.connect() as connection:
		expected = connection.execute(text("SELECT current_schema()")).scalar()
	engine = create_engine_and_migrate(settings(tmp_path, postgres_admin_dsn))
	try:
		with engine.connect() as connection:
			assert connection.execute(text("SELECT current_schema()")).scalar() == expected
			assert connection.execute(text("SHOW TIME ZONE")).scalar() == "UTC"
	finally:
		engine.dispose()


def test_explicit_connection_options_override_environment(
	tmp_path, postgres_dsn, pg_engine, monkeypatch
):
	monkeypatch.setenv("PGOPTIONS", "-csearch_path=not_the_test_schema")
	with pg_engine.connect() as connection:
		expected = connection.execute(text("SELECT current_schema()")).scalar()
	engine = create_engine_and_migrate(settings(tmp_path, postgres_dsn))
	try:
		with engine.connect() as connection:
			assert connection.execute(text("SELECT current_schema()")).scalar() == expected
			assert connection.execute(text("SHOW TIME ZONE")).scalar() == "UTC"
	finally:
		engine.dispose()
