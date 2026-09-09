package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/olmesm/gort/internal/core"
)

func parseJson(t *testing.T, body string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("invalid JSON %q: %s", body, err)
	}
	return doc
}

func TestHealthEndpointNeedsNoAuth(t *testing.T) {
	app := newTestApp(t)
	resp := app.client(t).get("/rest/health")
	if resp.Code != http.StatusOK {
		t.Fatalf("status %d", resp.Code)
	}
	if !strings.Contains(resp.Body.String(), `"pass"`) {
		t.Errorf("body: %s", resp.Body.String())
	}
}

func TestApiRequestsWithoutAKeyGetProblemDetails401(t *testing.T) {
	app := newTestApp(t)
	resp := app.client(t).get("/rest/v1/short-urls")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.Code)
	}
	if ct := resp.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("content type: %s", ct)
	}
	doc := parseJson(t, resp.Body.String())
	if doc["status"].(float64) != 401 {
		t.Errorf("problem status: %v", doc["status"])
	}
}

func TestShortUrlRoundTrip(t *testing.T) {
	app := newTestApp(t)
	client := app.adminClient(t)

	// create
	create := client.post("/rest/v1/short-urls",
		`{"longUrl":"https://example.com/round-trip","tags":["one","two"],"title":"Round trip"}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", create.Code, create.Body.String())
	}
	created := parseJson(t, create.Body.String())
	code := created["shortCode"].(string)
	if created["domain"] != "example.test" {
		t.Errorf("domain: %v", created["domain"])
	}
	if created["shortUrl"] != "http://example.test/"+code {
		t.Errorf("shortUrl: %v", created["shortUrl"])
	}

	// get
	get := client.get("/rest/v1/short-urls/" + code)
	if get.Code != http.StatusOK {
		t.Fatalf("get status %d", get.Code)
	}
	fetched := parseJson(t, get.Body.String())
	if fetched["title"] != "Round trip" {
		t.Errorf("title: %v", fetched["title"])
	}
	if tags := fetched["tags"].([]any); len(tags) != 2 {
		t.Errorf("tags: %v", tags)
	}

	// list with search
	list := client.get("/rest/v1/short-urls?searchTerm=round-trip")
	listed := parseJson(t, list.Body.String())
	if items := listed["data"].([]any); len(items) != 1 {
		t.Errorf("list items: %d", len(items))
	}

	// edit
	edit := client.patch("/rest/v1/short-urls/"+code,
		`{"longUrl":"https://example.com/edited","tags":["three"],"maxVisits":9}`)
	if edit.Code != http.StatusOK {
		t.Fatalf("edit status %d: %s", edit.Code, edit.Body.String())
	}
	edited := parseJson(t, edit.Body.String())
	if edited["longUrl"] != "https://example.com/edited" {
		t.Errorf("longUrl: %v", edited["longUrl"])
	}
	if edited["meta"].(map[string]any)["maxVisits"].(float64) != 9 {
		t.Errorf("maxVisits: %v", edited["meta"])
	}
	if edited["tags"].([]any)[0] != "three" {
		t.Errorf("tags: %v", edited["tags"])
	}

	// delete
	del := client.delete("/rest/v1/short-urls/" + code)
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete status %d", del.Code)
	}
	gone := client.get("/rest/v1/short-urls/" + code)
	if gone.Code != http.StatusNotFound {
		t.Fatalf("gone status %d", gone.Code)
	}
}

func TestCustomSlugsConflictWith409(t *testing.T) {
	app := newTestApp(t)
	client := app.adminClient(t)

	first := client.post("/rest/v1/short-urls",
		`{"longUrl":"https://example.com/a","customSlug":"taken-slug"}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status %d", first.Code)
	}
	second := client.post("/rest/v1/short-urls",
		`{"longUrl":"https://example.com/b","customSlug":"taken-slug"}`)
	if second.Code != http.StatusConflict {
		t.Fatalf("second status %d", second.Code)
	}
	if !strings.Contains(second.Body.String(), "taken-slug") {
		t.Errorf("body: %s", second.Body.String())
	}
}

func TestFindIfExistsReturnsTheExistingMapping(t *testing.T) {
	app := newTestApp(t)
	client := app.adminClient(t)

	first := parseJson(t, client.post("/rest/v1/short-urls",
		`{"longUrl":"https://example.com/find-me"}`).Body.String())
	second := parseJson(t, client.post("/rest/v1/short-urls",
		`{"longUrl":"https://example.com/find-me","findIfExists":true}`).Body.String())
	if first["shortCode"] != second["shortCode"] {
		t.Errorf("codes differ: %v vs %v", first["shortCode"], second["shortCode"])
	}
}

func TestInvariantsHoldAtTheApiBoundaryToo(t *testing.T) {
	app := newTestApp(t)
	client := app.adminClient(t)

	// A zero visit budget would create a link that is born expired.
	zero := client.post("/rest/v1/short-urls", `{"longUrl":"https://example.com/z","maxVisits":0}`)
	if zero.Code != http.StatusBadRequest {
		t.Fatalf("zero status %d", zero.Code)
	}
	if !strings.Contains(zero.Body.String(), "greater than zero") {
		t.Errorf("zero body: %s", zero.Body.String())
	}

	// An inverted validity window can never be valid.
	inverted := client.post("/rest/v1/short-urls",
		`{"longUrl":"https://example.com/w","validSince":"2026-05-01T00:00:00Z","validUntil":"2026-04-01T00:00:00Z"}`)
	if inverted.Code != http.StatusBadRequest {
		t.Fatalf("inverted status %d", inverted.Code)
	}
	if !strings.Contains(inverted.Body.String(), "earlier than") {
		t.Errorf("inverted body: %s", inverted.Body.String())
	}

	// Unsupported redirect statuses are named in the rejection.
	badStatus := client.post("/rest/v1/short-urls",
		`{"longUrl":"https://example.com/s","redirectStatus":418}`)
	if badStatus.Code != http.StatusBadRequest {
		t.Fatalf("bad status %d", badStatus.Code)
	}
	if !strings.Contains(badStatus.Body.String(), "418") {
		t.Errorf("bad status body: %s", badStatus.Body.String())
	}

	// PATCH validates the merged result, not just the patch.
	created := parseJson(t, client.post("/rest/v1/short-urls",
		`{"longUrl":"https://example.com/patch-me"}`).Body.String())
	code := created["shortCode"].(string)
	patch := client.patch("/rest/v1/short-urls/"+code, `{"maxVisits":-3}`)
	if patch.Code != http.StatusBadRequest {
		t.Fatalf("patch status %d", patch.Code)
	}
}

func TestKeysWithUnparseableStoredRolesAreRejectedNotAdmin(t *testing.T) {
	app := newTestApp(t)

	// Simulate a corrupt row: role text nothing recognizes.
	plain := GenerateApiKey()
	_, err := app.Db.Exec(t.Context(),
		`INSERT INTO api_keys (key_hash, name, role, domain_id, enabled, expires_at, created_at)
		 VALUES (?, 'corrupt', 'superuser', NULL, 1, NULL, ?)`,
		HashApiKey(plain), app.Db.BindTime(time.Now()))
	if err != nil {
		t.Fatal(err)
	}

	client := app.client(t)
	client.headers["X-Api-Key"] = plain
	resp := client.get("/rest/v1/short-urls")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.Code)
	}
}

func TestInvalidLongUrlsAreRejectedWith400(t *testing.T) {
	app := newTestApp(t)
	resp := app.adminClient(t).post("/rest/v1/short-urls", `{"longUrl":"nope"}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status %d", resp.Code)
	}
	if !strings.Contains(resp.Body.String(), "absolute http") {
		t.Errorf("body: %s", resp.Body.String())
	}
}

func TestAuthorKeysOnlySeeTheirOwnShortUrls(t *testing.T) {
	app := newTestApp(t)
	authorClient := app.client(t)
	authorClient.headers["X-Api-Key"] = createApiKey(t, app, core.AuthorRole())
	otherClient := app.client(t)
	otherClient.headers["X-Api-Key"] = createApiKey(t, app, core.AuthorRole())

	created := parseJson(t, authorClient.post("/rest/v1/short-urls",
		`{"longUrl":"https://example.com/mine-only"}`).Body.String())
	code := created["shortCode"].(string)

	// The other author cannot see it.
	otherGet := otherClient.get("/rest/v1/short-urls/" + code)
	if otherGet.Code != http.StatusNotFound {
		t.Fatalf("other get status %d", otherGet.Code)
	}
	otherList := parseJson(t, otherClient.get("/rest/v1/short-urls?searchTerm=mine-only").Body.String())
	if items := otherList["data"].([]any); len(items) != 0 {
		t.Errorf("other list items: %d", len(items))
	}

	// The creator can.
	mineGet := authorClient.get("/rest/v1/short-urls/" + code)
	if mineGet.Code != http.StatusOK {
		t.Fatalf("mine get status %d", mineGet.Code)
	}
}

func TestNonAdminKeysCannotManageApiKeys(t *testing.T) {
	app := newTestApp(t)
	client := app.client(t)
	client.headers["X-Api-Key"] = createApiKey(t, app, core.AuthorRole())
	resp := client.get("/rest/v1/api-keys")
	if resp.Code != http.StatusForbidden {
		t.Fatalf("status %d", resp.Code)
	}
}

func TestApiKeysCanBeMintedOverTheApiAndThenUsed(t *testing.T) {
	app := newTestApp(t)
	admin := app.adminClient(t)

	create := admin.post("/rest/v1/api-keys", `{"name":"minted","role":"author"}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", create.Code, create.Body.String())
	}
	doc := parseJson(t, create.Body.String())
	key := doc["apiKey"].(string)

	minted := app.client(t)
	minted.headers["X-Api-Key"] = key
	list := minted.get("/rest/v1/short-urls")
	if list.Code != http.StatusOK {
		t.Fatalf("list status %d", list.Code)
	}
}

func TestDisabledApiKeysStopWorking(t *testing.T) {
	app := newTestApp(t)
	admin := app.adminClient(t)

	doc := parseJson(t, admin.post("/rest/v1/api-keys", `{"name":"to-disable","role":"author"}`).Body.String())
	key := doc["apiKey"].(string)
	id := int64(doc["id"].(float64))

	patch := admin.patch("/rest/v1/api-keys/"+strconv.FormatInt(id, 10), `{"enabled":false}`)
	if patch.Code != http.StatusOK {
		t.Fatalf("patch status %d", patch.Code)
	}

	disabled := app.client(t)
	disabled.headers["X-Api-Key"] = key
	resp := disabled.get("/rest/v1/short-urls")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.Code)
	}
}

func TestTagsCanBeListedRenamedAndDeleted(t *testing.T) {
	app := newTestApp(t)
	client := app.adminClient(t)

	client.post("/rest/v1/short-urls",
		`{"longUrl":"https://example.com/tagged","tags":["rename-me","keep-me"]}`)

	rename := client.put("/rest/v1/tags", `{"oldName":"rename-me","newName":"renamed"}`)
	if rename.Code != http.StatusOK {
		t.Fatalf("rename status %d: %s", rename.Code, rename.Body.String())
	}

	listed := parseJson(t, client.get("/rest/v1/tags?withStats=true&searchTerm=renamed").Body.String())
	if items := listed["data"].([]any); len(items) != 1 {
		t.Errorf("list items: %d", len(items))
	}

	del := client.delete("/rest/v1/tags?tags=renamed")
	if del.Code != http.StatusOK {
		t.Fatalf("delete status %d", del.Code)
	}
	after := parseJson(t, client.get("/rest/v1/tags?searchTerm=renamed").Body.String())
	if items := after["data"].([]any); len(items) != 0 {
		t.Errorf("after items: %d", len(items))
	}
}

func TestRedirectRulesAreValidatedAndPersisted(t *testing.T) {
	app := newTestApp(t)
	client := app.adminClient(t)

	created := parseJson(t, client.post("/rest/v1/short-urls",
		`{"longUrl":"https://example.com/ruled"}`).Body.String())
	code := created["shortCode"].(string)

	bad := client.post("/rest/v1/short-urls/"+code+"/redirect-rules",
		`{"redirectRules":[{"longUrl":"https://example.com/x","conditions":[{"type":"nonsense","matchValue":"x"}]}]}`)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad status %d", bad.Code)
	}

	ok := client.post("/rest/v1/short-urls/"+code+"/redirect-rules",
		`{"redirectRules":[
		    {"longUrl":"https://example.com/android","conditions":[{"type":"device","matchValue":"android"}]},
		    {"longUrl":"https://example.com/fr","conditions":[{"type":"language","matchValue":"fr"}]}]}`)
	if ok.Code != http.StatusOK {
		t.Fatalf("ok status %d: %s", ok.Code, ok.Body.String())
	}

	rules := parseJson(t, client.get("/rest/v1/short-urls/"+code+"/redirect-rules").Body.String())
	if items := rules["redirectRules"].([]any); len(items) != 2 {
		t.Errorf("rules: %d", len(items))
	}
}

func TestDomainsCanBeRegisteredAndListed(t *testing.T) {
	app := newTestApp(t)
	client := app.adminClient(t)

	create := client.post("/rest/v1/domains", `{"domain":"extra.test"}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status %d", create.Code)
	}
	dup := client.post("/rest/v1/domains", `{"domain":"extra.test"}`)
	if dup.Code != http.StatusConflict {
		t.Fatalf("dup status %d", dup.Code)
	}
	list := client.get("/rest/v1/domains")
	if !strings.Contains(list.Body.String(), "extra.test") || !strings.Contains(list.Body.String(), "example.test") {
		t.Errorf("list body: %s", list.Body.String())
	}
}
