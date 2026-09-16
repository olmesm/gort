FROM python:3.12-slim AS runtime
WORKDIR /app
COPY --from=ghcr.io/astral-sh/uv:0.11.7 /uv /usr/local/bin/uv
COPY pyproject.toml uv.lock alembic.ini ./
COPY alembic ./alembic
COPY gort ./gort
RUN uv sync --frozen --no-dev --no-editable \
    && groupadd --gid 65532 gort \
    && useradd --uid 65532 --gid gort --create-home --no-log-init gort \
    && mkdir /data && chown gort:gort /data

ENV GORT_PORT=8080 \
    GORT_DATA_DIR=/data \
    PYTHONDONTWRITEBYTECODE=1 \
    PATH="/app/.venv/bin:$PATH"
USER gort
VOLUME /data
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD ["gort", "--healthcheck"]

ENTRYPOINT ["gort"]
