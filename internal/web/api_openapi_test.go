package web

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

func jsonAt(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func TestOpenAPIDescribesEveryRESTOperation(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			app := newTestAppWithConfig(t, map[string]string{"WEBHOOKS_ENABLED": fmt.Sprint(enabled)})
			response := app.client(t).get("/rest/openapi.json")
			if response.Code != 200 {
				t.Fatal(response.Body.String())
			}
			spec := parseJSON(t, response.Body.String())
			if jsonAt(spec, "info", "version") != "0.2.0" || !strings.HasPrefix(spec["openapi"].(string), "3.1") {
				t.Fatalf("metadata: %v", spec["info"])
			}
			count := 0
			err := chi.Walk(app.mux, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
				if !strings.HasPrefix(path, "/rest/v1/") && path != "/rest/health" {
					return nil
				}
				if method == "HEAD" {
					return nil
				}
				op := jsonAt(spec, "paths", path, strings.ToLower(method))
				if jsonAt(op, "operationId") == nil || jsonAt(op, "responses") == nil {
					t.Errorf("undocumented method: %s %s", method, path)
				}
				count++
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			expected := 29
			if enabled {
				expected = 33
			}
			if count != expected {
				t.Fatalf("documented %d operations, want %d", count, expected)
			}
			if (jsonAt(spec, "paths", "/rest/v1/webhooks") != nil) != enabled {
				t.Fatal("webhooks feature flag not reflected in schema")
			}
			if len(spec["security"].([]any)) != 2 || jsonAt(spec, "components", "securitySchemes", "apiKey", "name") != "X-Api-Key" {
				t.Fatal("missing auth documentation")
			}
			if security := jsonAt(spec, "paths", "/rest/health", "get", "security"); security == nil || len(security.([]any)) != 0 {
				t.Fatal("health must override auth")
			}
			resolve := func(s any) any {
				if ref := jsonAt(s, "$ref"); ref != nil {
					return jsonAt(spec, "components", "schemas", strings.TrimPrefix(ref.(string), "#/components/schemas/"))
				}
				return s
			}
			create := resolve(jsonAt(spec, "paths", "/rest/v1/short-urls", "post", "requestBody", "content", "application/json", "schema"))
			required := jsonAt(create, "required").([]any)
			if len(required) != 1 || required[0] != "longUrl" {
				t.Fatalf("create required fields: %v", required)
			}
			patch := resolve(jsonAt(spec, "paths", "/rest/v1/short-urls/{code}", "patch", "requestBody", "content", "application/json", "schema"))
			if jsonAt(patch, "required") != nil {
				t.Fatalf("PATCH must allow omission: %v", jsonAt(patch, "required"))
			}
			if typ := jsonAt(patch, "properties", "title", "type"); !strings.Contains(fmt.Sprint(typ), "null") {
				t.Fatalf("nullable title schema: %v", typ)
			}
			if len(jsonAt(spec, "paths", "/rest/v1/tags", "get", "responses", "200", "content", "application/json", "schema", "oneOf").([]any)) != 2 {
				t.Fatal("tag response variants missing")
			}
			for _, path := range []string{"/rest/docs", "/graphql/docs", "/graphql/schema.graphql", "/rest/openapi.yaml", "/api-docs.js", "/scalar-1.68.0.js"} {
				if got := app.client(t).get(path); got.Code != 200 {
					t.Errorf("docs %s: %d", path, got.Code)
				}
			}
		})
	}
}

func TestRESTBindingKeepsProblemDetailsAndPatchSemantics(t *testing.T) {
	app := newTestApp(t)
	c := app.adminClient(t)
	for _, body := range []string{`{`, `{"longUrl":123}`, `{}`, `{"longUrl":"bad"}`} {
		r := c.post("/rest/v1/short-urls", body)
		if r.Code != 400 || !strings.HasPrefix(r.Header().Get("Content-Type"), "application/problem+json") {
			t.Fatalf("binding: %d %s", r.Code, r.Body.String())
		}
		doc := parseJSON(t, r.Body.String())
		if doc["status"] != float64(400) || doc["type"] != "https://gort.dev/errors/invalid-data" {
			t.Fatal(doc)
		}
	}
	createShort(t, c, `{"longUrl":"https://example.com","customSlug":"patch","title":"Keep","group":"clear","futureField":true}`)
	r := c.patch("/rest/v1/short-urls/patch", `{"group":null,"maxVisits":null}`)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	doc := parseJSON(t, r.Body.String())
	if doc["title"] != "Keep" || doc["group"] != nil {
		t.Fatalf("PATCH: %v", doc)
	}
	if r = c.patch("/rest/v1/api-keys/nope", `{"enabled":false}`); r.Code != 404 {
		t.Fatalf("ID binding: %d", r.Code)
	}
}

func TestRESTScopedKeysCannotAccessGlobalTagsOrOtherDomainStats(t *testing.T) {
	app := newTestApp(t)
	admin := app.adminClient(t)
	createShort(t, admin, `{"longUrl":"https://example.com","tags":["private"]}`)
	admin.post("/rest/v1/domains", `{"domain":"other.example.com"}`)
	d, err := data.DomainByAuthority(t.Context(), app.DB, "other.example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []core.APIKeyRole{core.AuthorRole(), core.DomainRole(d.ID)} {
		c := app.client(t)
		c.headers["X-Api-Key"] = createAPIKey(t, app, role)
		for _, path := range []string{"/rest/v1/stats/visits-per-day?domain=example.test", "/rest/v1/stats/breakdown?by=device&domain=example.test", "/rest/v1/stats/visits-per-day?tag=private", "/rest/v1/tags?withStats=true", "/rest/v1/tags/private/visits"} {
			if r := c.get(path); r.Code != 403 {
				t.Fatalf("%s: %d %s", path, r.Code, r.Body.String())
			}
		}
		if r := c.put("/rest/v1/tags", `{"oldName":"private","newName":"stolen"}`); r.Code != 403 {
			t.Fatalf("rename: %d", r.Code)
		}
		if r := c.delete("/rest/v1/tags?tags=private"); r.Code != 403 {
			t.Fatalf("delete: %d", r.Code)
		}
	}
}

func TestRESTPathStyleSlugsSurviveRouterMigration(t *testing.T) {
	app := newTestApp(t)
	c := app.adminClient(t)
	createShort(t, c, `{"longUrl":"https://example.com/guide","customSlug":"docs/intro"}`)
	if r := c.get("/rest/v1/short-urls/docs%2Fintro"); r.Code != 200 {
		t.Fatalf("encoded code: %d %s", r.Code, r.Body.String())
	}
	if r := app.client(t).get("/docs/intro"); r.Code != 302 {
		t.Fatalf("path redirect: %d", r.Code)
	}
	if r := c.patch("/rest/v1/short-urls/docs%2Fintro", `{"title":"Guide"}`); r.Code != 200 {
		t.Fatalf("encoded PATCH: %d", r.Code)
	}
	if r := c.delete("/rest/v1/short-urls/docs%2Fintro"); r.Code != 204 {
		t.Fatalf("encoded delete: %d", r.Code)
	}
}
