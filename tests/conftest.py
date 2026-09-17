"""Each database test runs against a fresh PostgreSQL schema."""

import os
import uuid

import psycopg
import pytest
from psycopg import sql
from psycopg.conninfo import make_conninfo

from goto.config import Settings
from goto.db import create_engine_and_migrate


@pytest.fixture(scope="session")
def postgres_admin_dsn():
	dsn = os.environ.get("GOTO_TEST_POSTGRES_DSN", "")
	if not dsn:
		raise pytest.UsageError(
			"Database tests require PostgreSQL. Start a test server and set "
			"GOTO_TEST_POSTGRES_DSN=postgresql://user:password@localhost:5432/goto_test. "
			"The account must be able to create and drop schemas."
		)
	dsn = dsn.replace("postgresql+psycopg://", "postgresql://", 1)
	try:
		with psycopg.connect(dsn, connect_timeout=5) as connection:
			connection.execute("SELECT 1")
	except psycopg.Error as error:
		raise pytest.UsageError(
			"Cannot connect to the PostgreSQL test server. Check GOTO_TEST_POSTGRES_DSN "
			"and ensure the server is running."
		) from error
	return dsn


@pytest.fixture
def postgres_dsn(postgres_admin_dsn):
	schema = "goto_test_" + uuid.uuid4().hex
	with psycopg.connect(postgres_admin_dsn, autocommit=True) as connection:
		connection.execute(sql.SQL("CREATE SCHEMA {}").format(sql.Identifier(schema)))
	try:
		yield make_conninfo(postgres_admin_dsn, options=f"-csearch_path={schema}")
	finally:
		with psycopg.connect(postgres_admin_dsn, autocommit=True) as connection:
			connection.execute(sql.SQL("DROP SCHEMA {} CASCADE").format(sql.Identifier(schema)))


@pytest.fixture
def pg_engine(postgres_dsn, tmp_path):
	engine = create_engine_and_migrate(
		Settings(db_connection=postgres_dsn, data_dir=tmp_path, workers_enabled=False)
	)
	try:
		yield engine
	finally:
		engine.dispose()
