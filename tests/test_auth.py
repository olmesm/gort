"""Dashboard authentication, permissions, and account management."""

import time
from datetime import UTC, datetime, timedelta
from types import SimpleNamespace

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient
from sqlmodel import Session, select

from gort import auth, ui
from gort.config import Settings
from gort.models import APIKey, Domain, ShortURL, User


@pytest.fixture
def client(tmp_path, pg_engine, postgres_dsn):
	engine = pg_engine
	app = FastAPI()
	app.state.engine = engine
	app.state.settings = Settings(
		data_dir=tmp_path,
		db_connection=postgres_dsn,
		default_domain="testserver",
		initial_admin_password="initial-password",
		workers_enabled=False,
	)
	app.state.session_key = b"k" * 32
	app.include_router(ui.router)
	with Session(engine) as session:
		session.add(
			User(
				username="admin", password_hash=auth.hash_password("initial-password"), role="admin"
			)
		)
		session.add(
			User(
				username="member", password_hash=auth.hash_password("member-password"), role="user"
			)
		)
		session.add(Domain(authority="testserver", is_default=True))
		session.commit()
	with TestClient(app) as result:
		yield result


def sign_in(client, username="admin", password="initial-password"):
	return client.post(
		"/admin/login",
		data={"username": username, "password": password, "returnUrl": "/admin"},
		follow_redirects=False,
	)


def test_login_returns_safe_location_and_rejects_wrong_password(client):
	assert client.get("/admin/users", follow_redirects=False).status_code == 302
	assert sign_in(client, password="incorrect").status_code == 401
	response = client.post(
		"/admin/login",
		data={"username": "admin", "password": "initial-password", "returnUrl": "//evil.test"},
		follow_redirects=False,
	)
	assert response.headers["location"] == "/admin"
	assert "HttpOnly" in response.headers["set-cookie"]
	assert client.get("/admin/users").status_code == 200


def test_password_change_revokes_session(client):
	sign_in(client)
	response = client.post(
		"/admin/users/1/password", data={"password": "changed-password"}, follow_redirects=False
	)
	assert response.status_code == 302
	assert (
		client.get("/admin", follow_redirects=False).headers["location"].startswith("/admin/login")
	)
	assert sign_in(client, password="changed-password").status_code == 302


def test_unknown_role_and_tampered_cookie_fail_closed(client):
	sign_in(client)
	cookie = client.cookies.get("gort_session")
	client.cookies.clear()
	client.cookies.set("gort_session", cookie + "a")
	assert (
		client.get("/admin", follow_redirects=False).headers["location"].startswith("/admin/login")
	)
	sign_in(client)
	with Session(client.app.state.engine) as session:
		user = session.get(User, 1)
		user.role = "superuser"
		session.add(user)
		session.commit()
	assert (
		client.get("/admin", follow_redirects=False).headers["location"].startswith("/admin/login")
	)


def test_regular_user_scope_and_admin_restrictions(client):
	sign_in(client, "member", "member-password")
	assert client.get("/admin/users").status_code == 403
	assert client.get("/admin", follow_redirects=False).headers["location"] == "/admin/short-urls"
	denied = client.post(
		"/admin/short-urls/new",
		data={"longUrl": "https://example.com", "customSlug": "private", "group": "other-team"},
	)
	assert denied.status_code == 400
	assert "only assign groups" in denied.text
	with Session(client.app.state.engine) as session:
		session.add(
			ShortURL(
				short_code="hidden",
				domain_id=1,
				long_url="https://example.com",
				group_name="other-team",
			)
		)
		session.commit()
	assert client.get("/admin/short-urls/1/edit").status_code == 404
	assert "testserver/hidden" not in client.get("/admin/short-urls").text


def test_ui_lifecycle_and_all_pages_render(client):
	sign_in(client)
	for path in [
		"/admin",
		"/admin/short-urls",
		"/admin/short-urls/new",
		"/admin/tags",
		"/admin/domains",
		"/admin/api-keys",
		"/admin/users",
		"/admin/visits/orphan",
	]:
		response = client.get(path)
		assert response.status_code == 200, (path, response.text)
	response = client.post(
		"/admin/short-urls/new",
		data={
			"longUrl": "https://example.com/path",
			"customSlug": "journey",
			"tags": "e2e, journey",
			"forwardQuery": "true",
		},
		follow_redirects=False,
	)
	assert response.status_code == 302, response.text
	response = client.get("/admin/short-urls")
	assert "journey" in response.text and "e2e" in response.text
	response = client.get("/admin/short-urls?search=not-present", headers={"HX-Request": "true"})
	assert "No short URLs match" in response.text and "<html" not in response.text
	assert client.get("/admin/short-urls/1/edit").status_code == 200
	assert client.get("/admin/short-urls/1/visits").status_code == 200
	response = client.post(
		"/admin/short-urls/1/rules/add",
		data={"ruleLongUrl": "https://example.com/mobile", "device": "android"},
	)
	assert response.status_code == 200, response.text
	assert "Device is android" in response.text
	response = client.post("/admin/short-urls/1/rules/delete", data={"priority": "1"})
	assert "Device is android" not in response.text


def test_last_admin_cannot_be_demoted_or_deleted(client):
	sign_in(client)
	client.post("/admin/users/1/role", data={"role": "user"})
	client.post("/admin/users/1/delete")
	with Session(client.app.state.engine) as session:
		assert session.get(User, 1).role == "admin"


def test_oidc_session_expiry_and_normalized_group_scope(client):
	with Session(client.app.state.engine) as session:
		user = User(
			username="sso",
			password_hash="!oidc",
			auth_source="oidc",
			oidc_subject="subject",
			role="user",
		)
		session.add(user)
		session.commit()
		session.refresh(user)
		uid = user.id
	payload = {
		"uid": uid,
		"exp": int(time.time()) + 500,
		"oidc_exp": int(time.time()) - 1,
		"g": ["/Team", "Team"],
	}
	client.cookies.set("gort_session", auth.sign_payload(client.app.state.session_key, payload))
	assert (
		client.get("/admin", follow_redirects=False).headers["location"].startswith("/admin/login")
	)
	payload["oidc_exp"] = int(time.time()) + 500
	client.cookies.set("gort_session", auth.sign_payload(client.app.state.session_key, payload))
	assert client.get("/admin/short-urls").status_code == 200
	assert auth.normalize_groups(["/Team", "Team", " /nested/team ", ""]) == ["Team", "nested/team"]


def test_api_key_expiry_and_unknown_roles(client):
	raw = auth.generate_api_key()
	with Session(client.app.state.engine) as session:
		row = APIKey(key_hash=auth.hash_api_key(raw))
		session.add(row)
		session.commit()
		request = SimpleNamespace(headers={"x-api-key": raw})
		assert auth.authenticate_api_key(request, session)
		row.role = "superuser"
		session.add(row)
		session.commit()
		assert auth.authenticate_api_key(request, session) is None
		row.role = "domain"
		session.add(row)
		session.commit()
		assert auth.authenticate_api_key(request, session) is None
		row.role = "admin"
		row.expires_at = datetime.now(UTC) - timedelta(seconds=1)
		session.add(row)
		session.commit()
		assert auth.authenticate_api_key(request, session) is None


def test_all_templates_compile_and_escape_user_content():
	for name in ui.env.list_templates():
		ui.env.get_template(name)
	response = ui.render("notfound", "<script>alert(1)</script>", 404)
	assert b"&lt;script&gt;" in response.body


def test_oidc_invalid_state_clears_cookie(client):
	client.app.state.settings.oidc_issuer = "https://issuer.example"
	client.app.state.settings.oidc_client_id = "gort"
	response = client.get("/admin/oidc/callback?state=attacker&code=test")
	assert response.status_code == 401
	assert "Max-Age=0" in response.headers["set-cookie"]


def test_oidc_pkce_signature_nonce_and_account_provisioning(client, monkeypatch):
	from urllib.parse import parse_qs, urlencode, urlsplit

	import httpx
	from authlib.jose import JsonWebKey, JsonWebToken
	from cryptography.hazmat.primitives.asymmetric import rsa
	from cryptography.hazmat.primitives.serialization import Encoding, NoEncryption, PrivateFormat

	cfg = client.app.state.settings
	cfg.oidc_issuer = "https://issuer.example"
	cfg.oidc_client_id = "gort"
	private = rsa.generate_private_key(public_exponent=65537, key_size=2048)
	pem = private.private_bytes(Encoding.PEM, PrivateFormat.PKCS8, NoEncryption())
	key = JsonWebKey.import_key(pem, {"kid": "test-key"})
	public = key.as_dict(is_private=False)
	issued = {}

	def provider(request):
		if request.url.path.endswith("/.well-known/openid-configuration"):
			return httpx.Response(
				200,
				json={
					"issuer": cfg.oidc_issuer,
					"authorization_endpoint": cfg.oidc_issuer + "/authorize",
					"token_endpoint": cfg.oidc_issuer + "/token",
					"jwks_uri": cfg.oidc_issuer + "/keys",
				},
			)
		if request.url.path == "/keys":
			return httpx.Response(200, json={"keys": [public]})
		if request.url.path == "/token":
			posted = parse_qs(request.content.decode())
			assert posted["code_verifier"][0] == issued["verifier"]
			claims = {
				"iss": cfg.oidc_issuer,
				"aud": cfg.oidc_client_id,
				"sub": "idp-user",
				"iat": int(time.time()),
				"exp": int(time.time()) + 300,
				"nonce": issued["nonce"],
				"preferred_username": "admin",
				"groups": ["/gort-admins", "/Team"],
			}
			token = JsonWebToken(["RS256"]).encode({"alg": "RS256", "kid": "test-key"}, claims, key)
			return httpx.Response(
				200,
				json={"id_token": token.decode(), "access_token": "access", "token_type": "Bearer"},
			)
		raise AssertionError(request.url)

	original = httpx.AsyncClient
	monkeypatch.setattr(
		ui.httpx,
		"AsyncClient",
		lambda **kwargs: original(transport=httpx.MockTransport(provider), **kwargs),
	)
	response = client.get("/admin/oidc/login?returnUrl=/admin/users", follow_redirects=False)
	assert response.status_code == 302
	params = parse_qs(urlsplit(response.headers["location"]).query)
	transient = auth.verify_payload(client.app.state.session_key, client.cookies.get("gort_oidc"))
	issued.update(verifier=transient["v"], nonce=params["nonce"][0])
	assert params["code_challenge_method"] == ["S256"]
	assert params["code_challenge"] == [
		auth.encode(__import__("hashlib").sha256(transient["v"].encode()).digest())
	]
	response = client.get(
		"/admin/oidc/callback?" + urlencode({"state": params["state"][0], "code": "valid-code"}),
		follow_redirects=False,
	)
	assert response.status_code == 302, response.text
	assert response.headers["location"] == "/admin/users"
	cookie = auth.verify_payload(client.app.state.session_key, client.cookies.get("gort_session"))
	assert cookie["oidc_exp"] <= int(time.time()) + 300
	assert cookie["g"] == ["gort-admins", "Team"]
	assert client.get("/admin/users").status_code == 200
	with Session(client.app.state.engine) as session:
		user = session.exec(select(User).where(User.oidc_subject == "idp-user")).one()
		assert user.username == "admin-idp-user"
		assert user.role == "admin"
		assert session.get(User, 1).auth_source == "local"
	response = client.get(
		"/admin/oidc/callback?" + urlencode({"state": params["state"][0], "code": "valid-code"})
	)
	assert response.status_code == 401


def test_analytics_and_overview_aggregate_without_loading_visit_history(client, monkeypatch):
	from sqlalchemy import event
	from starlette.datastructures import QueryParams

	from gort.models import Visit

	engine = client.app.state.engine
	today = datetime.now(UTC).replace(hour=12, minute=0, second=0, microsecond=0)
	with Session(engine) as session:
		links = [
			ShortURL(short_code=code, domain_id=1, long_url="https://example.com")
			for code in ["popular", "other"]
		]
		session.add_all(links)
		session.flush()
		target_id, other_id = [link.id for link in links]
		session.add_all(
			[
				Visit(
					short_url_id=target_id,
					visited_at=today if i < 1500 else today - timedelta(days=1),
					browser=f"Browser {i % 12}",
					is_bot=i < 1000,
				)
				for i in range(2000)
			]
		)
		session.add_all(
			[
				Visit(short_url_id=target_id, visited_at=today - timedelta(days=90))
				for _ in range(70)
			]
		)
		session.add_all([Visit(short_url_id=other_id, visited_at=today) for _ in range(19)])
		session.add_all([Visit(visit_type="regular_404", visited_at=today) for _ in range(8)])
		session.add_all([Visit(visit_type="invalid_short_url", visited_at=today) for _ in range(3)])
		session.commit()

	loaded = []
	statements = []

	def visit_loaded(target, _):
		loaded.append(target.id)

	def before_execute(_connection, _cursor, statement, parameters, _context, _many):
		statements.append((" ".join(statement.lower().split()), parameters))

	event.listen(Visit, "load", visit_loaded)
	event.listen(engine, "before_cursor_execute", before_execute)
	request = SimpleNamespace(
		app=client.app,
		url=SimpleNamespace(path=f"/admin/short-urls/{target_id}/visits"),
		query_params=QueryParams(
			{
				"startDate": (today - timedelta(days=3)).isoformat(),
				"endDate": (today + timedelta(days=1)).isoformat(),
				"page": "2",
			}
		),
	)
	try:
		with Session(engine) as session:
			model = ui.analytics(request, session, target_id)
		assert model["Pager"]["TotalItems"] == 2000
		assert model["Pager"]["CurrentPage"] == 2
		assert len(model["Visits"]) == 25
		assert sum(point["Count"] for point in model["Chart"]["Points"]) == 2000
		assert len(model["Breakdowns"][0]["Rows"]) == 8
		assert {row["Count"] for row in model["Breakdowns"][0]["Rows"]} == {167}
		assert len(loaded) == 25
		row_queries = [
			(sql, params) for sql, params in statements if sql.startswith("select visits.id,")
		]
		assert len(row_queries) == 1
		assert " limit " in row_queries[0][0] and " offset " in row_queries[0][0]
		assert list(row_queries[0][1].values())[-2:] == [25, 25]
		assert sum("group by" in sql for sql, _ in statements) == 4

		loaded.clear()
		statements.clear()
		monkeypatch.setattr(ui, "page", lambda request, user, name, title, data, status=200: data)
		with Session(engine) as session:
			model = ui.overview_page(request, session, auth.CurrentUser(1, "admin", "admin"))
		assert model["Stats"]["VisitCount"] == 2089
		assert model["Stats"]["OrphanVisitCount"] == 11
		assert model["Stats"]["BotVisitCount"] == 1000
		assert sum(point["Count"] for point in model["Chart"]["Points"]) == 2019
		assert {row["ShortCode"]: row["VisitCount"] for row in model["Recent"]} == {
			"popular": 2070,
			"other": 19,
		}
		assert loaded == []
		assert not any(sql.startswith("select visits.id,") for sql, _ in statements)

		request.query_params = QueryParams({"type": "regular_404", "page": "999"})
		with Session(engine) as session:
			model = ui.analytics(request, session, orphan=True)
		assert model["Pager"]["CurrentPage"] == 1
		assert model["Pager"]["TotalItems"] == 8
		assert len(model["Visits"]) == 8
		assert sum(point["Count"] for point in model["Chart"]["Points"]) == 8
	finally:
		event.remove(Visit, "load", visit_loaded)
		event.remove(engine, "before_cursor_execute", before_execute)
