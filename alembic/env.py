from sqlmodel import SQLModel

from alembic import context
from gort import models  # noqa: F401
from gort.config import Settings
from gort.db import create_engine

config = context.config
target_metadata = SQLModel.metadata


def migrate(connection):
	context.configure(connection=connection, target_metadata=target_metadata, compare_type=True)
	with context.begin_transaction():
		context.run_migrations()


if context.is_offline_mode():
	context.configure(
		dialect_name="postgresql", target_metadata=target_metadata, literal_binds=True
	)
	with context.begin_transaction():
		context.run_migrations()
elif config.attributes.get("connection") is not None:
	migrate(config.attributes["connection"])
else:
	engine = create_engine(Settings())
	try:
		with engine.begin() as connection:
			migrate(connection)
	finally:
		engine.dispose()
