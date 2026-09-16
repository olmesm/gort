"""Application assembly and process lifecycle."""

import logging
import secrets
from contextlib import asynccontextmanager
from pathlib import Path

from fastapi import FastAPI
from fastapi.responses import FileResponse, JSONResponse, Response
from pydantic import BaseModel
from sqlalchemy import text, update
from sqlmodel import Session, select

from gort.config import Settings

logger = logging.getLogger(__name__)


class HealthStatus(BaseModel):
	status: str
	version: str


def create_app(settings: Settings | None = None) -> FastAPI:
	from gort import api, auth, graphql, public, ui
	from gort.db import create_engine_and_migrate
	from gort.middleware import SecurityMiddleware
	from gort.models import Domain, User
	from gort.workers import Workers

	settings = settings or Settings()

	@asynccontextmanager
	async def lifespan(app):
		settings.data_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
		engine = create_engine_and_migrate(settings)
		app.state.engine = engine
		app.state.session_key = auth.load_or_create_session_key(settings.data_dir)
		with Session(engine) as session:
			domain = session.exec(
				select(Domain).where(Domain.authority == settings.default_domain)
			).first()
			if domain is None:
				domain = Domain(authority=settings.default_domain)
			domain.is_default = True
			session.add(domain)
			session.exec(
				update(Domain)
				.where(Domain.authority != settings.default_domain)
				.values(is_default=False)
			)
			if session.exec(select(User).limit(1)).first() is None:
				password = settings.initial_admin_password or secrets.token_urlsafe(18)
				session.add(
					User(
						username=settings.initial_admin_username or "admin",
						password_hash=auth.hash_password(password),
						role="admin",
					)
				)
				if not settings.initial_admin_password:
					logger.warning(
						"Created initial admin user '%s' with generated password: %s",
						settings.initial_admin_username or "admin",
						password,
					)
			session.commit()
		app.state.workers = Workers(engine, settings)
		if settings.workers_enabled:
			app.state.workers.start()
		try:
			yield
		finally:
			app.state.workers.close()
			engine.dispose()

	app = FastAPI(
		title="Gort API",
		version="0.3.0",
		lifespan=lifespan,
		docs_url=None,
		redoc_url=None,
		openapi_url=None,
	)
	app.state.settings = settings
	app.add_middleware(SecurityMiddleware, settings=settings)
	api.install_exception_handlers(app)

	@app.exception_handler(Exception)
	async def unexpected_error(request, exc):
		logger.error("Unexpected request error", exc_info=exc)
		if request.url.path.startswith(("/rest/", "/graphql")):
			return api.problem_response(500, "An unexpected error occurred.")
		return Response("An unexpected error occurred.", status_code=500, media_type="text/plain")

	@app.get(
		"/rest/health",
		tags=["Health"],
		response_model=HealthStatus,
		responses={503: {"model": HealthStatus, "description": "Database unavailable"}},
	)
	def health():
		try:
			with app.state.engine.connect() as connection:
				connection.execute(text("SELECT 1"))
			return {"status": "pass", "version": "1.0.0"}
		except Exception:
			return JSONResponse({"status": "fail", "version": "1.0.0"}, status_code=503)

	@app.get("/rest/openapi.json", include_in_schema=False)
	def openapi_json():
		return JSONResponse(app.openapi())

	@app.get("/rest/openapi.yaml", include_in_schema=False)
	def openapi_yaml():
		# JSON is valid YAML 1.2 and avoids a second schema serialization format.
		import json

		return Response(json.dumps(app.openapi(), indent=2), media_type="application/yaml")

	@app.get("/rest/docs", include_in_schema=False)
	def rest_docs():
		return ui.render("rest-docs")

	@app.get("/graphql/docs", include_in_schema=False)
	def graphql_docs():
		return ui.render("graphql-docs")

	static = Path(__file__).parent / "static"

	def asset_route(name):
		def asset():
			headers = (
				{"Cache-Control": "public, max-age=86400"} if name == "inter-var.woff2" else None
			)
			return FileResponse(static / name, headers=headers)

		return asset

	for name in ("app.css", "htmx.min.js", "inter-var.woff2", "scalar-1.68.0.js", "api-docs.js"):
		app.add_api_route(
			"/" + name, asset_route(name), methods=["GET", "HEAD"], include_in_schema=False
		)

	for route in api.router.routes:
		if settings.webhooks_enabled or not route.path.startswith("/rest/v1/webhooks"):
			app.router.routes.append(route)
	app.include_router(graphql.router)
	app.include_router(ui.router)
	app.include_router(public.router)
	return app
