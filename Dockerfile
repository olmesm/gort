FROM python:3.12-slim AS runtime
WORKDIR /app
COPY --from=ghcr.io/astral-sh/uv:0.11.7 /uv /usr/local/bin/uv
COPY pyproject.toml uv.lock alembic.ini ./
COPY alembic ./alembic
COPY goto ./goto
RUN uv sync --frozen --no-dev --no-editable \
    && groupadd --gid 65532 goto \
    && useradd --uid 65532 --gid goto --create-home --no-log-init goto \
    && mkdir /data && chown goto:goto /data

ENV GOTO_PORT=8080 \
    GOTO_DATA_DIR=/data \
    PYTHONDONTWRITEBYTECODE=1 \
    PATH="/app/.venv/bin:$PATH"
USER goto
VOLUME /data
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD ["goto", "--healthcheck"]

ENTRYPOINT ["goto"]
