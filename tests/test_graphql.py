from test_api import create

pytest_plugins = ["test_api"]


def graph(client, query, variables=None):
	return client.post("/graphql", json={"query": query, "variables": variables})


def test_schema_and_introspection(api_client):
	assert "type Query" in api_client.get("/graphql/schema.graphql").text
	response = graph(api_client, "{ __schema { queryType { name } } }")
	assert response.json()["data"]["__schema"]["queryType"]["name"] == "Query"
	assert response.headers["cache-control"] == "no-store"


def test_mutation_variables_null_and_nested_fields(api_client):
	response = graph(
		api_client,
		"mutation($input:CreateShortURLInput!) {createShortURL(input:$input) {shortCode title tags}}",
		{"input": {"longUrl": "https://example.com", "customSlug": "graph", "title": "Keep"}},
	)
	assert response.json()["data"]["createShortURL"]["shortCode"] == "graph", response.text
	response = graph(
		api_client, 'mutation {updateShortURL(code:"graph",input:{title:null}){title}}'
	)
	assert response.json()["data"]["updateShortURL"]["title"] is None
	response = graph(
		api_client,
		'{shortURL(code:"graph"){visits{pagination{totalItems}} redirectRules{defaultLongUrl redirectRules{priority}}}}',
	)
	assert response.json()["data"]["shortURL"]["visits"]["pagination"]["totalItems"] == 0


def test_scopes_and_error_extensions(api_client):
	create(api_client)
	api_client.headers["X-Api-Key"] = "author"
	response = graph(api_client, '{shortURL(code:"hello"){shortCode}}')
	assert response.status_code == 200
	assert response.json()["errors"][0]["extensions"] == {"code": "not-found", "status": 404}
	assert (
		graph(api_client, "{visitsOverview{visitsCount}}").json()["errors"][0]["extensions"][
			"status"
		]
		== 403
	)


def test_validation_get_mutations_and_scalar_bounds(api_client):
	response = graph(api_client, "{noSuchField}")
	assert response.status_code == 422
	response = api_client.get("/graphql", params={"query": "mutation {deleteOrphanVisits}"})
	assert response.status_code == 406
	response = graph(api_client, "mutation {deleteAPIKey(id:9223372036854775808)}")
	assert response.status_code == 422
	response = graph(
		api_client,
		'mutation{createShortURL(input:{longUrl:"https://example.com",validSince:"not-a-date"}){shortCode}}',
	)
	assert response.status_code == 422


def test_complexity_counts_nested_pages_and_fragments(api_client):
	query = "{shortURLs(filter:{itemsPerPage:500}){data{...Nested}}} fragment Nested on ShortURL {visits(filter:{itemsPerPage:500}){data{date}}}"
	response = graph(api_client, query)
	assert response.status_code == 422
	assert "complexity" in response.json()["errors"][0]["message"]
	query = "query($n:Int=500){shortURLs(filter:{itemsPerPage:$n}){data{visits(filter:{itemsPerPage:$n}){data{date}}}}}"
	assert graph(api_client, query).status_code == 422


def test_keys_and_webhooks_reveal_secrets_once(api_client):
	response = graph(
		api_client,
		'mutation {createAPIKey(input:{role:"author"}){id apiKey} createWebhook(input:{name:"x",url:"https://example.com",events:["url.created"]}){id secret}}',
	)
	assert response.json()["data"]["createAPIKey"]["apiKey"]
	assert response.json()["data"]["createWebhook"]["secret"]
	response = graph(api_client, "{apiKeys{id apiKey} webhooks{id secret}}")
	assert all(row["apiKey"] == "" for row in response.json()["data"]["apiKeys"])
	assert all(row["secret"] == "" for row in response.json()["data"]["webhooks"])
