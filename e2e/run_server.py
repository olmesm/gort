"""Run browser tests with an isolated PostgreSQL schema and temporary session keys."""

import os
import signal
import subprocess
import sys
import tempfile
import uuid
from pathlib import Path

import psycopg
from psycopg import sql
from psycopg.conninfo import make_conninfo


def main():
	dsn = os.environ.get("GOTO_TEST_POSTGRES_DSN", "")
	if not dsn:
		raise SystemExit(
			"Browser tests require PostgreSQL. Set GOTO_TEST_POSTGRES_DSN to a running "
			"test database; the account must be able to create and drop schemas."
		)
	dsn = dsn.replace("postgresql+psycopg://", "postgresql://", 1)
	schema = "goto_browser_" + uuid.uuid4().hex
	try:
		with psycopg.connect(dsn, autocommit=True, connect_timeout=5) as connection:
			connection.execute(sql.SQL("CREATE SCHEMA {}").format(sql.Identifier(schema)))
	except psycopg.Error:
		raise SystemExit(
			"Cannot prepare the PostgreSQL browser-test schema. Check GOTO_TEST_POSTGRES_DSN, "
			"server availability, and the account's CREATE permission."
		) from None
	process = None

	def stop(_signum, _frame):
		if process is not None and process.poll() is None:
			process.terminate()

	signal.signal(signal.SIGTERM, stop)
	signal.signal(signal.SIGINT, stop)
	try:
		with tempfile.TemporaryDirectory(prefix="goto-browser-") as directory:
			port = os.environ.get("E2E_PORT", "18100")
			env = {
				**os.environ,
				"GOTO_DB_CONNECTION": make_conninfo(dsn, options=f"-csearch_path={schema}"),
				"GOTO_DATA_DIR": directory,
				"GOTO_PORT": port,
				"GOTO_DEFAULT_DOMAIN": f"localhost:{port}",
				"GOTO_AUTO_RESOLVE_TITLES": "false",
				"GOTO_RATE_LIMIT_PER_MINUTE": "10000",
				"GOTO_WEBHOOKS_ENABLED": "true",
				"GOTO_INITIAL_ADMIN_USERNAME": "admin",
				"GOTO_INITIAL_ADMIN_PASSWORD": "e2e-password-123",
			}
			process = subprocess.Popen(
				[sys.executable, "-m", "goto"], cwd=Path(__file__).resolve().parent.parent, env=env
			)
			return process.wait()
	finally:
		if process is not None and process.poll() is None:
			process.terminate()
			try:
				process.wait(timeout=10)
			except subprocess.TimeoutExpired:
				process.kill()
				process.wait()
		with psycopg.connect(dsn, autocommit=True, connect_timeout=5) as connection:
			connection.execute(sql.SQL("DROP SCHEMA {} CASCADE").format(sql.Identifier(schema)))


if __name__ == "__main__":
	raise SystemExit(main())
