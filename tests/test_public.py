from pathlib import Path

import pytest
from fastapi.testclient import TestClient
from sqlmodel import Session, select

from gort.app import create_app
from gort.auth import hash_api_key
from gort.config import Settings
from gort.models import APIKey, Domain, Visit


@pytest.fixture
def client(tmp_path, postgres_dsn):
	settings = Settings(
		data_dir=tmp_path,
		db_connection=postgres_dsn,
		workers_enabled=False,
		auto_resolve_titles=False,
		initial_admin_password="test-password",
		rate_limit_per_minute=0,
		default_domain="testserver",
		track_skip_param="no-track",
	)
	with TestClient(create_app(settings)) as client:
		with Session(client.app.state.engine) as session:
			session.add(APIKey(key_hash=hash_api_key("test-key"), role="admin"))
			session.commit()
		client.headers["X-Api-Key"] = "test-key"
		yield client


def create(client, **values):
	response = client.post(
		"/rest/v1/short-urls", json={"longUrl": "https://example.com/path", **values}
	)
	assert response.status_code == 201, response.text
	return response.json()["shortCode"]


def test_redirect_tracks_anonymized_visit(client):
	code = create(client, redirectStatus=307)
	response = client.get("/" + code, follow_redirects=False)
	assert response.status_code == 307
	assert response.headers["location"] == "https://example.com/path"
	visits = client.get(f"/rest/v1/short-urls/{code}/visits").json()
	assert visits["pagination"]["totalItems"] == 1
	assert visits["data"][0]["device"] == "desktop"


def test_rules_query_forwarding_and_tracking_skip(client):
	code = create(client, customSlug="docs/intro")
	response = client.post(
		f"/rest/v1/short-urls/{code}/redirect-rules",
		json={
			"redirectRules": [
				{
					"longUrl": "https://example.com/mobile?fixed=1#part",
					"conditions": [{"type": "device", "matchValue": "android"}],
				}
			]
		},
	)
	assert response.status_code == 200, response.text
	response = client.get(
		f"/{code}?word=hello+world&no-track=1",
		headers={"User-Agent": "Android"},
		follow_redirects=False,
	)
	assert (
		response.headers["location"] == "https://example.com/mobile?fixed=1&word=hello%20world#part"
	)
	assert client.get(f"/rest/v1/short-urls/{code}/visits").json()["pagination"]["totalItems"] == 0


def test_expiry_and_domain_isolation(client):
	create(client, customSlug="limited", maxVisits=1)
	create(client, customSlug="limited", domain="another.test", longUrl="https://example.org/")
	assert client.get("/limited", follow_redirects=False).status_code == 302
	assert client.get("/limited", follow_redirects=False).status_code == 404
	response = client.get("/limited", headers={"Host": "another.test"}, follow_redirects=False)
	assert response.headers["location"] == "https://example.org/"
	with Session(client.app.state.engine) as session:
		assert session.exec(select(Visit).where(Visit.visit_type == "invalid_short_url")).first()


def test_qr_and_robots(client):
	code = create(client, crawlable=True)
	assert "Allow: /" + code in client.get("/robots.txt").text
	png = client.get(f"/{code}/qr-code")
	assert png.content.startswith(b"\x89PNG")
	svg = client.get(f"/{code}/qr-code?format=svg&size=80")
	assert 'width="80"' in svg.text
	assert svg.headers["content-type"].startswith("image/svg+xml")


def test_browser_security_and_head(client):
	assert (
		client.post(
			"/admin/login",
			data={"username": "admin", "password": "test-password"},
			headers={"Origin": "https://other.example"},
		).status_code
		== 403
	)
	assert client.get("/admin/login").headers["cache-control"] == "no-store"
	assert client.head("/rest/health").status_code == 200
	assert client.head("/rest/health").content == b""
	assert client.head("/admin/login").status_code == 200
	assert client.post("/admin/login", content=b"x" * ((1 << 20) + 1)).status_code == 413
	response = client.post("/graphql", content=b"x" * ((1 << 20) + 1))
	assert response.status_code == 413 and "errors" in response.json()
	response = client.post("/rest/v1/short-urls", content=b"x" * ((1 << 20) + 1))
	assert response.status_code == 400
	assert response.headers["content-type"] == "application/problem+json"


def test_untrusted_forwarded_ip_ignored(tmp_path, postgres_dsn):
	app = create_app(
		Settings(
			data_dir=Path(tmp_path),
			db_connection=postgres_dsn,
			initial_admin_password="test-password",
			workers_enabled=False,
		)
	)
	with TestClient(app, client=("203.0.113.47", 5000)) as client:
		client.get("/", headers={"X-Forwarded-For": "8.8.8.8"})
		with Session(app.state.engine) as session:
			assert session.exec(select(Visit)).first().remote_ip == "203.0.113.0"


def test_reconfigured_default_domain_is_applied_on_restart(tmp_path, postgres_dsn):
	for authority in ("old.example", "new.example"):
		settings = Settings(
			data_dir=tmp_path,
			db_connection=postgres_dsn,
			default_domain=authority,
			initial_admin_password="test-password",
			workers_enabled=False,
		)
		with TestClient(create_app(settings)) as client:
			with Session(client.app.state.engine) as session:
				defaults = session.exec(select(Domain).where(Domain.is_default.is_(True))).all()
				assert [domain.authority for domain in defaults] == [authority]
				if authority == "new.example":
					assert session.exec(
						select(Domain).where(Domain.authority == "old.example")
					).first()
