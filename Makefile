.PHONY: run fmt check check-lint check-format check-test

run:
	uv run gort

fmt:
	uv run ruff check --fix gort tests alembic design e2e/run_server.py
	uv run ruff format gort tests alembic design e2e/run_server.py

check-lint:
	uv run ruff check gort tests alembic design e2e/run_server.py

check-format:
	uv run ruff format --check gort tests alembic design e2e/run_server.py

check-test:
	uv run pytest

check:
	$(MAKE) -j3 check-lint check-format check-test
