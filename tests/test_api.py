"""REST permissions, validation, pagination, and response fields."""

import hashlib

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient
from sqlmodel import Session, select

from gort import api, graphql
from gort.config import Settings
from gort.models import APIKey, Domain, ShortURL, Visit, WebhookDelivery


@pytest.fixture
def api_client(pg_engine, postgres_dsn):
	engine = pg_engine
	settings = Settings(
		default_domain="sho.rt",
		db_connection=postgres_dsn,
		auto_resolve_titles=False,
		webhooks_enabled=True,
	)
	with Session(engine) as session:
		default = Domain(authority="sho.rt", is_default=True)
		other = Domain(authority="other.test")
		session.add_all([default, other])
		session.commit()
		for token, role, domain in [
			("admin", "admin", None),
			("author", "author", None),
			("domain", "domain", other.id),
		]:
			session.add(
				APIKey(
					key_hash=hashlib.sha256(token.encode()).hexdigest(), role=role, domain_id=domain
				)
			)
		session.commit()
	app = FastAPI()
	app.state.engine, app.state.settings = engine, settings
	api.install_exception_handlers(app)
	app.include_router(api.router)
	app.include_router(graphql.router)
	with TestClient(app) as client:
		client.headers["X-Api-Key"] = "admin"
		yield client
	engine.dispose()


def create(client, code="hello", **payload):
	response = client.post(
		"/rest/v1/short-urls",
		json={"longUrl": "https://example.com/page", "customSlug": code, **payload},
	)
	assert response.status_code == 201, response.text
	return response.json()


def test_authentication_and_problem_envelopes(api_client):
	api_client.headers.pop("X-Api-Key")
	response = api_client.get("/rest/v1/short-urls")
	assert response.status_code == 401
	assert response.json()["type"] == "https://gort.dev/errors/missing-authentication"
	api_client.headers["Authorization"] = "Bearer admin"
	assert api_client.get("/rest/v1/short-urls").status_code == 200
	response = api_client.post("/rest/v1/short-urls", json={})
	assert response.status_code == 400
	assert response.headers["content-type"].startswith("application/problem+json")


def test_patch_omitted_and_null_and_unique_slug(api_client):
	created = create(api_client, title="Keep", tags=["One"], group="Team", maxVisits=3)
	assert created["tags"] == ["one"]
	response = api_client.patch("/rest/v1/short-urls/hello", json={"group": None})
	assert response.status_code == 200, response.text
	assert response.json()["title"] == "Keep"
	assert response.json().get("group") is None
	response = api_client.patch(
		"/rest/v1/short-urls/hello", json={"title": None, "tags": None, "maxVisits": None}
	)
	assert response.status_code == 200, response.text
	assert response.json().get("title") is None
	assert response.json()["tags"] == []
	assert response.json()["meta"].get("maxVisits") is None
	response = api_client.post(
		"/rest/v1/short-urls", json={"longUrl": "https://example.com", "customSlug": "hello"}
	)
	assert response.status_code == 409
	assert response.json()["type"].endswith("/non-unique-slug")


def test_author_and_domain_scope(api_client):
	create(api_client, "admin-link")
	api_client.headers["X-Api-Key"] = "author"
	create(api_client, "mine")
	assert api_client.get("/rest/v1/short-urls/admin-link").status_code == 404
	assert [row["shortCode"] for row in api_client.get("/rest/v1/short-urls").json()["data"]] == [
		"mine"
	]
	assert api_client.get("/rest/v1/visits").status_code == 403
	assert api_client.get("/rest/v1/stats/visits-per-day").status_code == 403
	assert api_client.get("/rest/v1/stats/visits-per-day?shortCode=mine").status_code == 200
	api_client.headers["X-Api-Key"] = "domain"
	assert (
		api_client.post("/rest/v1/short-urls", json={"longUrl": "https://example.com"}).status_code
		== 403
	)
	create(api_client, "scoped", domain="other.test")
	assert api_client.get("/rest/v1/short-urls/scoped?domain=other.test").status_code == 200
	assert api_client.get("/rest/v1/domains/sho.rt/visits").status_code == 403


def test_permissive_queries_and_empty_group(api_client):
	create(api_client, "ungrouped")
	create(api_client, "grouped", group="Team")
	response = api_client.get(
		"/rest/v1/short-urls?page=oops&itemsPerPage=oops&startDate=oops&group="
	)
	assert response.status_code == 200, response.text
	assert [row["shortCode"] for row in response.json()["data"]] == ["ungrouped"]
	assert response.json()["pagination"]["itemsPerPage"] == 20
	response = api_client.get("/rest/v1/short-urls?domain=missing.test")
	assert response.json()["data"] == []


def test_rules_visits_stats_and_cascade(api_client):
	create(api_client)
	rules = [
		{
			"longUrl": "https://mobile.example.com",
			"conditions": [{"type": "device", "matchValue": "mobile"}],
		}
	]
	response = api_client.post(
		"/rest/v1/short-urls/hello/redirect-rules", json={"redirectRules": rules}
	)
	assert response.status_code == 200, response.text
	assert response.json()["redirectRules"][0]["priority"] == 1
	with Session(api_client.app.state.engine) as session:
		link = session.exec(select(ShortURL)).first()
		session.add_all(
			[
				Visit(short_url_id=link.id, browser="Firefox"),
				Visit(short_url_id=link.id, browser="Robot", is_bot=True),
				Visit(visit_type="regular_404"),
			]
		)
		session.commit()
	assert (
		api_client.get("/rest/v1/short-urls/hello/visits?excludeBots=yes").json()["pagination"][
			"totalItems"
		]
		== 1
	)
	assert api_client.get("/rest/v1/visits").json()["visitsCount"] == 2
	assert (
		sum(row["count"] for row in api_client.get("/rest/v1/stats/visits-per-day").json()["data"])
		== 2
	)
	assert len(api_client.get("/rest/v1/stats/breakdown?by=browser").json()["data"]) == 2
	assert api_client.delete("/rest/v1/short-urls/hello").status_code == 204
	assert api_client.get("/rest/v1/visits/non-orphan").json()["data"] == []
	assert api_client.get("/rest/v1/visits/orphan").json()["pagination"]["totalItems"] == 1


def test_domain_tag_and_key_administration(api_client):
	assert (
		api_client.post("/rest/v1/domains", json={"domain": "NEW.test"}).json()["domain"]
		== "new.test"
	)
	assert api_client.post("/rest/v1/domains", json={"domain": "new.test"}).status_code == 409
	assert api_client.delete("/rest/v1/domains/sho.rt").status_code == 403
	create(api_client, tags=["old"])
	assert (
		api_client.put("/rest/v1/tags", json={"oldName": "old", "newName": "NEW"}).status_code
		== 200
	)
	assert api_client.get("/rest/v1/tags?withStats=true").json()["data"][0]["shortUrlsCount"] == 1
	assert api_client.delete("/rest/v1/tags?tags[]=new").json()["deletedTags"] == 1
	key = api_client.post("/rest/v1/api-keys", json={"role": "author"}).json()
	assert "apiKey" in key
	assert all("apiKey" not in row for row in api_client.get("/rest/v1/api-keys").json()["data"])
	assert (
		api_client.patch(f"/rest/v1/api-keys/{key['id']}", json={"enabled": False}).status_code
		== 200
	)
	api_client.headers["X-Api-Key"] = key["apiKey"]
	assert api_client.get("/rest/v1/short-urls").status_code == 401


def test_webhook_secret_and_outbox(api_client):
	response = api_client.post(
		"/rest/v1/webhooks",
		json={"name": "events", "url": "https://example.com/hook", "events": ["url.created"]},
	)
	assert response.status_code == 201, response.text
	assert response.json()["secret"]
	assert "secret" not in api_client.get("/rest/v1/webhooks").json()["data"][0]
	create(api_client)
	with Session(api_client.app.state.engine) as session:
		delivery = session.exec(select(WebhookDelivery)).one()
		assert delivery.event == "url.created"
	api_client.app.state.settings.webhooks_enabled = False
	assert api_client.get("/rest/v1/webhooks").status_code == 404


def test_openapi_documents_typed_responses_and_preserves_omission(api_client):
	spec = api_client.app.openapi()

	def resolve(schema):
		if "$ref" in schema:
			return spec["components"]["schemas"][schema["$ref"].rsplit("/", 1)[1]]
		return schema

	def success(path, method="get", status=200):
		return resolve(
			spec["paths"][path][method]["responses"][str(status)]["content"]["application/json"][
				"schema"
			]
		)

	# Every JSON success body has a real schema, including both tag list variants.
	for path, item in spec["paths"].items():
		for operation in item.values():
			for status, response in operation["responses"].items():
				if status.startswith("2") and "content" in response:
					schema = response["content"]["application/json"]["schema"]
					assert "$ref" in schema or "anyOf" in schema, (path, status, schema)
	assert "/graphql" not in spec["paths"]

	links = success("/rest/v1/short-urls")
	assert set(links["required"]) == {"data", "pagination"}
	pagination = resolve(links["properties"]["pagination"])
	assert set(pagination["required"]) == {
		"currentPage",
		"pagesCount",
		"itemsPerPage",
		"itemsInCurrentPage",
		"totalItems",
	}
	link_schema = resolve(links["properties"]["data"]["items"])
	assert set(link_schema["properties"]) == {
		"shortCode",
		"shortUrl",
		"domain",
		"longUrl",
		"title",
		"dateCreated",
		"tags",
		"group",
		"meta",
		"visitsSummary",
		"forwardQuery",
		"crawlable",
		"redirectStatus",
	}
	assert set(resolve(link_schema["properties"]["meta"])["properties"]) == {
		"validSince",
		"validUntil",
		"maxVisits",
	}
	assert set(resolve(link_schema["properties"]["visitsSummary"])["required"]) == {
		"total",
		"nonBots",
		"bots",
	}
	assert "title" not in link_schema["required"] and "group" not in link_schema["required"]

	created = create(api_client)
	assert created["meta"] == {}
	assert "title" not in created and "group" not in created
	assert set(created) == set(link_schema["required"])
	page = api_client.get("/rest/v1/short-urls").json()
	assert page["data"] == [created]
	assert set(page["pagination"]) == set(pagination["required"])
	assert set(success("/rest/v1/short-urls", "post", 201)["properties"]) == set(
		link_schema["properties"]
	)

	key_list = success("/rest/v1/api-keys")
	assert "apiKey" not in resolve(key_list["properties"]["data"]["items"])["properties"]
	assert "apiKey" in success("/rest/v1/api-keys", "post", 201)["required"]
	hook_list = success("/rest/v1/webhooks")
	assert "secret" not in resolve(hook_list["properties"]["data"]["items"])["properties"]
	assert "secret" in success("/rest/v1/webhooks", "post", 201)["required"]

	tag_variants = success("/rest/v1/tags")["anyOf"]
	assert len(tag_variants) == 2
	tag_page = api_client.get("/rest/v1/tags").json()
	stats_page = api_client.get("/rest/v1/tags?withStats=true").json()
	assert tag_page["data"] == stats_page["data"] == []


@pytest.mark.parametrize(
	"payload",
	[
		{"maxVisits": "3"},
		{"forwardQuery": "false"},
		{"crawlable": 1},
		{"shortCodeLength": 5.0},
		{"redirectStatus": "302"},
		{"validSince": "2099-01-01"},
		{"validUntil": 0},
	],
)
def test_rest_rejects_invalid_json_field_types(api_client, payload):
	response = api_client.post(
		"/rest/v1/short-urls", json={"longUrl": "https://example.com", **payload}
	)
	assert response.status_code == 400
	assert response.json()["type"] == "https://gort.dev/errors/invalid-data"
	assert set(response.json()) == {"type", "title", "detail", "status"}
	assert api_client.get("/rest/v1/short-urls").json()["data"] == []


def test_null_rules_clear_rules_and_null_enabled_disables_key(api_client):
	create(api_client)
	response = api_client.post(
		"/rest/v1/short-urls/hello/redirect-rules", json={"redirectRules": None}
	)
	assert response.status_code == 200
	assert response.json()["redirectRules"] == []
	key = api_client.post("/rest/v1/api-keys", json={"role": "author"}).json()
	response = api_client.patch(f"/rest/v1/api-keys/{key['id']}", json={"enabled": None})
	assert response.status_code == 200
	assert response.json() == {"id": key["id"], "enabled": False}


def test_error_precedence_and_domain_order(api_client):
	create(api_client, tags=["taken"])
	response = api_client.put("/rest/v1/tags", json={"oldName": "missing", "newName": "taken"})
	assert response.status_code == 409
	assert response.json()["type"].endswith("/tag-conflict")
	response = api_client.get("/rest/v1/short-urls/hello?domain=absent.test")
	assert response.status_code == 404
	assert response.json()["detail"] == "Domain 'absent.test' is not registered."
	response = api_client.patch("/rest/v1/api-keys/999", json={"enabled": False})
	assert response.status_code == 404
	assert response.json()["detail"] == "API key 999 was not found."
	assert api_client.get("/rest/v1/domains").json()["data"][0]["isDefault"] is True
	assert api_client.get("/rest/v1/tags?searchTerm=TAKEN").json()["data"] == ["taken"]
	assert api_client.get("/rest/v1/tags?searchTerm=%20TAKEN%20").json()["data"] == []


@pytest.mark.parametrize("raw", ["1_0", " 2", "999999999999999999999999", "١٠"])
def test_invalid_numeric_queries_use_list_defaults(api_client, raw):
	for path, expected in [("/short-urls", 20), ("/tags", 500), ("/visits/non-orphan", 20)]:
		response = api_client.get("/rest/v1" + path, params={"itemsPerPage": raw})
		assert response.status_code == 200
		assert response.json()["pagination"]["itemsPerPage"] == expected


def test_date_query_rejects_python_only_formats_and_clamps_last_page(api_client):
	create(api_client, tags=["one"])
	with Session(api_client.app.state.engine) as session:
		link = session.exec(select(ShortURL)).one()
		session.add(Visit(short_url_id=link.id))
		session.commit()
	for path in ("/short-urls", "/visits/non-orphan"):
		response = api_client.get(
			"/rest/v1" + path,
			params={"startDate": "2099-W01-1", "page": "999", "itemsPerPage": "1"},
		)
		assert response.json()["pagination"]["totalItems"] == 1
		assert response.json()["pagination"]["currentPage"] == 1
	response = api_client.get("/rest/v1/stats/visits-per-day?startDate=2099-W01-1")
	assert sum(day["count"] for day in response.json()["data"]) == 1
