package web

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

func graphRequest(t *testing.T, c *testClient, query string, variables map[string]any) map[string]any {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		t.Fatal(err)
	}
	response := c.post("/graphql", string(body))
	if response.Code != 200 {
		t.Fatalf("GraphQL HTTP %d: %s", response.Code, response.Body.String())
	}
	return parseJSON(t, response.Body.String())
}
func graphOK(t *testing.T, c *testClient, query string, variables map[string]any) map[string]any {
	t.Helper()
	doc := graphRequest(t, c, query, variables)
	if doc["errors"] != nil {
		t.Fatalf("GraphQL errors: %v", doc["errors"])
	}
	return doc["data"].(map[string]any)
}
func graphDenied(t *testing.T, c *testClient, query string, status int) {
	t.Helper()
	doc := graphRequest(t, c, query, nil)
	errs, ok := doc["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Fatalf("expected denial, got %v", doc)
	}
	ext := errs[0].(map[string]any)["extensions"].(map[string]any)
	if ext["status"] != float64(status) {
		t.Fatalf("expected %d, got %v", status, doc)
	}
}

func TestGraphQLLinkLifecycleAndRESTParity(t *testing.T) {
	app := newTestApp(t)
	client := app.adminClient(t)
	doc := graphOK(t, client, `mutation Create($input: CreateShortURLInput!) { createShortURL(input:$input) { shortCode title group tags meta {maxVisits validSince} redirectStatus } }`, map[string]any{"input": map[string]any{"longUrl": "https://example.com/article", "customSlug": "graph-link", "title": "Article", "group": "docs", "tags": []string{"docs"}, "maxVisits": 12, "validSince": "2025-01-01T00:00:00Z", "redirectStatus": 307}})
	link := doc["createShortURL"].(map[string]any)
	if link["shortCode"] != "graph-link" || link["redirectStatus"] != float64(307) {
		t.Fatalf("create: %v", doc)
	}
	// Reads from REST see the same record and options.
	rest := client.get("/rest/v1/short-urls/graph-link")
	if rest.Code != 200 || !strings.Contains(rest.Body.String(), `"group":"docs"`) {
		t.Fatalf("REST: %s", rest.Body.String())
	}
	doc = graphOK(t, client, `mutation { updateShortURL(code:"graph-link",input:{title:null,group:null,maxVisits:null,tags:[],forwardQuery:false}) {title group tags meta {maxVisits validSince} longUrl forwardQuery} }`, nil)
	link = doc["updateShortURL"].(map[string]any)
	if link["title"] != nil || link["group"] != nil || link["meta"].(map[string]any)["maxVisits"] != nil || link["longUrl"] != "https://example.com/article" || link["forwardQuery"] != false || len(link["tags"].([]any)) != 0 {
		t.Fatalf("PATCH null/absent: %v", doc)
	}
	if link["meta"].(map[string]any)["validSince"] == nil {
		t.Fatal("omitted timestamp was cleared")
	}
	graphOK(t, client, `mutation {setRedirectRules(code:"graph-link",rules:[{longUrl:"https://example.com/mobile",conditions:[{type:"device",matchValue:"mobile"}]}]) {redirectRules {priority conditions {type matchValue}}}}`, nil)
	app.client(t).get("/graph-link")
	doc = graphOK(t, client, `query {shortURLs(filter:{group:"",itemsPerPage:1}){data {shortCode visits(filter:{itemsPerPage:1}){data{date potentialBot} pagination{totalItems}} redirectRules{redirectRules{longUrl}}} pagination{itemsPerPage totalItems}}}`, nil)
	page := doc["shortURLs"].(map[string]any)
	rows := page["data"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["visits"].(map[string]any)["pagination"].(map[string]any)["totalItems"] != float64(1) {
		t.Fatalf("nested query: %v", doc)
	}
	graphOK(t, client, `query {visitsOverview {visitsCount} visits(filter:{itemsPerPage:1}){pagination{totalItems}} domainVisits(authority:"example.test"){pagination{totalItems}} visitsPerDay(scope:{shortCode:"graph-link"}){date count} breakdown(by:"device",scope:{shortCode:"graph-link"}){value count}}`, nil)
	graphOK(t, client, `mutation {deleteShortURLVisits(code:"graph-link") deleteShortURL(code:"graph-link")}`, nil)
	if response := client.get("/rest/v1/short-urls/graph-link"); response.Code != 404 {
		t.Fatalf("delete: %d", response.Code)
	}
}

func TestGraphQLAdministrativeOperations(t *testing.T) {
	app := newTestAppWithConfig(t, map[string]string{"WEBHOOKS_ENABLED": "true"})
	c := app.adminClient(t)
	graphOK(t, c, `mutation {createDomain(domain:"links.example.com"){domain} updateDomainRedirects(input:{domain:"links.example.com",baseUrlRedirect:"https://example.com"}){redirects{baseUrlRedirect}}}`, nil)
	doc := graphOK(t, c, `mutation {createAPIKey(input:{name:"graph author",role:"author"}){id apiKey} createWebhook(input:{name:"events",url:"https://example.com/hook",events:["url.created"]}){id secret}}`, nil)
	key := doc["createAPIKey"].(map[string]any)
	hook := doc["createWebhook"].(map[string]any)
	if key["apiKey"] == "" || hook["secret"] == "" {
		t.Fatalf("missing one-time secrets: %v", doc)
	}
	lists := graphOK(t, c, `{apiKeys{id apiKey} webhooks{id secret} domains{domain} orphanVisits{pagination{totalItems}}}`, nil)
	for _, raw := range lists["apiKeys"].([]any) {
		if raw.(map[string]any)["apiKey"] != "" {
			t.Fatal("listed key leaked")
		}
	}
	for _, raw := range lists["webhooks"].([]any) {
		if raw.(map[string]any)["secret"] != "" {
			t.Fatal("listed webhook secret leaked")
		}
	}
	author := app.client(t)
	author.headers["X-Api-Key"] = key["apiKey"].(string)
	graphOK(t, author, `mutation {createShortURL(input:{longUrl:"https://example.com",customSlug:"author-link",tags:["before"]}){shortCode}}`, nil)
	graphOK(t, c, `mutation {renameTag(oldName:"before",newName:"after"){newName}}`, nil)
	tags := graphOK(t, c, `{tags(itemsPerPage:1){data{tag shortUrlsCount} pagination{totalItems}} tagVisits(tag:"after"){pagination{totalItems}}}`, nil)
	if tags["tags"].(map[string]any)["data"].([]any)[0].(map[string]any)["tag"] != "after" {
		t.Fatalf("tags: %v", tags)
	}
	graphOK(t, c, `mutation {deleteTags(tags:["after"]) deleteOrphanVisits deleteDomain(authority:"links.example.com")}`, nil)
	graphOK(t, c, fmt.Sprintf(`mutation {setAPIKeyEnabled(id:%.0f,enabled:false){enabled} setWebhookEnabled(id:%.0f,enabled:false){enabled}}`, key["id"], hook["id"]), nil)
	if response := author.post("/graphql", `{"query":"{__typename}"}`); response.Code != 401 {
		t.Fatal("disabled key authenticated")
	}
	graphOK(t, c, fmt.Sprintf(`mutation {deleteAPIKey(id:%.0f) deleteWebhook(id:%.0f)}`, key["id"], hook["id"]), nil)
}

func TestGraphQLScopedKeysAndDisabledWebhooks(t *testing.T) {
	app := newTestApp(t)
	admin := app.adminClient(t)
	createShort(t, admin, `{"longUrl":"https://example.com/private","customSlug":"private","tags":["private"]}`)
	if response := admin.post("/rest/v1/domains", `{"domain":"other.example.com"}`); response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	domain, err := data.DomainByAuthority(t.Context(), app.DB, "other.example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []core.APIKeyRole{core.AuthorRole(), core.DomainRole(domain.ID)} {
		c := app.client(t)
		c.headers["X-Api-Key"] = createAPIKey(t, app, role)
		graphDenied(t, c, `{shortURL(code:"private"){shortCode}}`, 404)
		for _, query := range []string{`{domainVisits(authority:"example.test"){pagination{totalItems}}}`, `{visitsOverview{visitsCount}}`, `{visits{pagination{totalItems}}}`, `{orphanVisits{pagination{totalItems}}}`, `{apiKeys{id}}`, `{tags{data{tag visitsCount}}}`, `{tagVisits(tag:"private"){pagination{totalItems}}}`, `{visitsPerDay(scope:{domain:"example.test"}){count}}`, `{breakdown(by:"device",scope:{tag:"private"}){count}}`, `mutation {renameTag(oldName:"private",newName:"stolen"){newName}}`, `mutation {deleteTags(tags:["private"])}`, `mutation {createDomain(domain:"no.example.com"){domain}}`, `mutation {createAPIKey(input:{}){id}}`, `mutation {deleteOrphanVisits}`} {
			graphDenied(t, c, query, 403)
		}
		domainName := "example.test"
		if role.Kind == core.RoleDomain {
			domainName = "other.example.com"
		}
		graphOK(t, c, `mutation Own($domain:String) {createShortURL(input:{longUrl:"https://example.com/owned",domain:$domain}){shortCode}}`, map[string]any{"domain": domainName})
		if role.Kind == core.RoleDomain {
			graphOK(t, c, `{domainVisits(authority:"other.example.com"){pagination{totalItems}} visitsPerDay(scope:{domain:"other.example.com"}){count}}`, nil)
		}
	}
	for _, query := range []string{`{webhooks{id}}`, `mutation {createWebhook(input:{name:"x",url:"https://example.com",events:["url.created"]}){id}}`, `mutation {setWebhookEnabled(id:1,enabled:true){id}}`, `mutation {deleteWebhook(id:1)}`} {
		graphDenied(t, admin, query, 404)
	}
}

func TestGraphQLTransportAndLimits(t *testing.T) {
	app := newTestApp(t)
	c := app.adminClient(t)
	for _, token := range []string{"", "invalid"} {
		u := app.client(t)
		u.headers["X-Api-Key"] = token
		if response := u.post("/graphql", `{"query":"{__typename}"}`); response.Code != 401 {
			t.Fatalf("auth: %d", response.Code)
		}
	}
	bearer := app.client(t)
	bearer.headers["Authorization"] = "Bearer " + c.headers["X-Api-Key"]
	graphOK(t, bearer, `{__schema{queryType{name} mutationType{name}}}`, nil)
	if response := c.get("/graphql?query=" + url.QueryEscape(`{shortURLs{pagination{totalItems}}}`)); response.Code != 200 {
		t.Fatalf("GET: %s", response.Body.String())
	}
	query := `mutation {createShortURL(input:{longUrl:"https://example.com",customSlug:"unsafe-get"}){shortCode}}`
	if response := c.get("/graphql?query=" + url.QueryEscape(query)); response.Code != 406 {
		t.Fatalf("GET mutation: %d %s", response.Code, response.Body.String())
	}
	if response := c.get("/rest/v1/short-urls/unsafe-get"); response.Code != 404 {
		t.Fatal("GET ran mutation")
	}
	for _, body := range []string{`{"query":"{"}`, `{"query":"{unknownField}"}`, `{"query":"{shortURLs(filter:{itemsPerPage:500}){data{visits(filter:{itemsPerPage:500}){data{date}}}}}"}`, `{"query":"{__typename}","variables":{"padding":"` + strings.Repeat("a", 1<<20) + `"}}`} {
		response := c.post("/graphql", body)
		if !strings.Contains(response.Body.String(), "errors") {
			t.Fatalf("expected rejection: %d %.300s", response.Code, response.Body.String())
		}
	}
	graphDenied(t, c, `mutation {createShortURL(input:{longUrl:"not-a-url"}){shortCode}}`, 400)
	// Confirm malformed input cannot change an existing record.
	createShort(t, c, `{"longUrl":"https://example.com","customSlug":"valid"}`)
	graphDenied(t, c, `mutation {updateShortURL(code:"valid",input:{redirectStatus:999}){shortCode}}`, 400)
	if got := c.get("/rest/v1/short-urls/valid"); !strings.Contains(got.Body.String(), `"redirectStatus":302`) {
		t.Fatal(got.Body.String())
	}
}

func TestGraphQLPostRateLimit(t *testing.T) {
	app := newTestAppWithConfig(t, map[string]string{"RATE_LIMIT_PER_MINUTE": "1"})
	c := app.adminClient(t)
	graphOK(t, c, `{__typename}`, nil)
	if response := c.post("/graphql", `{"query":"{__typename}"}`); response.Code != 429 {
		t.Fatalf("rate limit: %d", response.Code)
	}
}

func TestReservedGraphQLSlugRejected(t *testing.T) {
	app := newTestApp(t)
	c := app.adminClient(t)
	for _, slug := range []string{"graphql", "graphql/docs", "graphql/anything"} {
		graphDenied(t, c, fmt.Sprintf(`mutation {createShortURL(input:{longUrl:"https://example.com",customSlug:%q}){shortCode}}`, slug), 400)
		if r := c.post("/rest/v1/short-urls", fmt.Sprintf(`{"longUrl":"https://example.com","customSlug":%q}`, slug)); r.Code != 400 {
			t.Fatalf("reserved slug: %d", r.Code)
		}
	}
}
