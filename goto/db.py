"""PostgreSQL connections and schema migrations."""

import os
from pathlib import Path

from psycopg.conninfo import conninfo_to_dict
from sqlalchemy import Engine
from sqlmodel import create_engine as sqlalchemy_create_engine

from alembic import command
from alembic.config import Config


def create_engine(settings) -> Engine:
	connection = conninfo_to_dict(settings.db_connection)
	connection["options"] = (
		connection.get("options", os.environ.get("PGOPTIONS", "")) + " -c timezone=UTC"
	).strip()
	return sqlalchemy_create_engine(
		"postgresql+psycopg://", connect_args=connection, pool_pre_ping=True
	)


def migrate(engine: Engine):
	package = Path(__file__).resolve().parent
	source_config = package.parent / "alembic.ini"
	config = Config(str(source_config if source_config.exists() else package / "alembic.ini"))
	if not source_config.exists():
		config.set_main_option("script_location", str(package / "migrations"))
	with engine.begin() as connection:
		config.attributes["connection"] = connection
		command.upgrade(config, "head")


def create_engine_and_migrate(settings) -> Engine:
	engine = create_engine(settings)
	try:
		migrate(engine)
	except Exception:
		engine.dispose()
		raise
	return engine
