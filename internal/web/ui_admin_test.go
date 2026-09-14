package web

import (
	"fmt"
	"html/template"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/olmesm/gort/internal/data"
)

func dashboardAdmin(t *testing.T, app *App) *testClient {
	t.Helper()
	user, err := data.UserByUsername(t.Context(), app.DB, "admin")
	if err != nil {
		t.Fatal(err)
	}
	signedIn := httptest.NewRecorder()
	app.SignIn(signedIn, user)
	c := app.client(t)
	c.headers["Cookie"] = signedIn.Result().Cookies()[0].String()
	return c
}

func TestAdminCreationValidationMatchesAPIs(t *testing.T) {
	app := newTestAppWithConfig(t, map[string]string{"WEBHOOKS_ENABLED": "true"})
	ui, api := dashboardAdmin(t, app), app.adminClient(t)
	for _, tc := range []struct{ resource, form, body, graph, message string }{
		{"api-keys", "role=unknown", `{"role":"unknown"}`, `mutation {createAPIKey(input:{role:"unknown"}){id}}`, "Unknown role"},
		{"api-keys", "role=domain&domain=missing.test", `{"role":"domain","domain":"missing.test"}`, `mutation {createAPIKey(input:{role:"domain",domain:"missing.test"}){id}}`, "need an existing"},
		{"api-keys", "expiresAt=2000-01-01", `{"expiresAt":"2000-01-01T00:00:00Z"}`, `mutation {createAPIKey(input:{expiresAt:"2000-01-01T00:00:00Z"}){id}}`, "must be in the future"},
		{"webhooks", "url=https://example.com&event_url_created=true", `{"name":"","url":"https://example.com","events":["url.created"]}`, `mutation {createWebhook(input:{name:"",url:"https://example.com",events:["url.created"]}){id}}`, "name is required"},
		{"webhooks", "name=bad&url=ftp://example.com&event_url_created=true", `{"name":"bad","url":"ftp://example.com","events":["url.created"]}`, `mutation {createWebhook(input:{name:"bad",url:"ftp://example.com",events:["url.created"]}){id}}`, "absolute http(s) URL"},
		{"webhooks", "name=bad&url=https://example.com", `{"name":"bad","url":"https://example.com","events":[]}`, `mutation {createWebhook(input:{name:"bad",url:"https://example.com",events:[]}){id}}`, "at least one event"},
	} {
		t.Run(tc.resource+"/"+tc.message, func(t *testing.T) {
			r := ui.postForm("/admin/"+tc.resource, tc.form)
			if r.Code != 200 || !strings.Contains(r.Body.String(), tc.message) {
				t.Fatalf("UI: %d %s", r.Code, r.Body.String())
			}
			r = api.post("/rest/v1/"+tc.resource, tc.body)
			if r.Code != 400 || !strings.Contains(r.Body.String(), tc.message) {
				t.Fatalf("REST: %d %s", r.Code, r.Body.String())
			}
			graphDenied(t, api, tc.graph, 400)
		})
	}
	keys, err := data.ListAPIKeys(t.Context(), app.DB)
	if err != nil || len(keys) != 1 {
		t.Fatalf("invalid keys were saved: %v %v", keys, err)
	}
	hooks, err := data.ListWebhooks(t.Context(), app.DB)
	if err != nil || len(hooks) != 0 {
		t.Fatalf("invalid hooks were saved: %v %v", hooks, err)
	}
	if r := ui.postForm("/admin/api-keys", "expiresAt=not-a-date"); r.Code != 200 || !strings.Contains(r.Body.String(), "must be a valid date") {
		t.Fatal(r.Body.String())
	}
	// Valid form creations show each secret once and persist the selected scope/events.
	r := ui.postForm("/admin/api-keys", "name=ui-key&role=domain&domain=example.test")
	keys, err = data.ListAPIKeys(t.Context(), app.DB)
	if r.Code != 200 || err != nil || len(keys) != 2 {
		t.Fatalf("key creation: %d %v", r.Code, err)
	}
	key := keys[0]
	for _, k := range keys {
		if k.Name != nil && *k.Name == "ui-key" {
			key = k
		}
	}
	if key.Role != "domain" || key.DomainID == nil || !strings.Contains(r.Body.String(), "gort_") {
		t.Fatal("key scope or plaintext missing")
	}
	r = ui.postForm("/admin/webhooks", "name=ui-hook&url=https://example.com/hook&event_url_created=true")
	hooks, err = data.ListWebhooks(t.Context(), app.DB)
	if r.Code != 200 || err != nil || len(hooks) != 1 || hooks[0].Events != "url.created" || !strings.Contains(r.Body.String(), hooks[0].Secret) {
		t.Fatalf("hook creation: %d %v", r.Code, err)
	}
	if strings.Contains(ui.get("/admin/webhooks").Body.String(), hooks[0].Secret) {
		t.Fatal("secret shown again")
	}
	// Toggle only the requested hook, including repeated toggles and missing IDs.
	other, err := data.InsertWebhook(t.Context(), app.DB, "other", "https://example.com/other", "other-secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		if r = ui.postForm(fmt.Sprintf("/admin/webhooks/%d/toggle", hooks[0].ID), ""); r.Code != 302 {
			t.Fatal(r.Body.String())
		}
		rows, err := data.ListWebhooks(t.Context(), app.DB)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range rows {
			if h.ID == hooks[0].ID && h.Enabled != enabled || h.ID == other.ID && !h.Enabled {
				t.Fatal("toggle changed wrong state")
			}
		}
	}
	if r = ui.postForm("/admin/webhooks/999999/toggle", ""); r.Code != 302 {
		t.Fatal(r.Body.String())
	}
}

func TestDashboardRenderFailuresReturn500(t *testing.T) {
	app := newTestApp(t)
	ui, api := dashboardAdmin(t, app), app.adminClient(t)
	createShort(t, api, `{"longUrl":"https://example.com","customSlug":"render"}`)
	d, err := data.DefaultDomain(t.Context(), app.DB)
	if err != nil {
		t.Fatal(err)
	}
	link, err := data.ShortURLDetailByCode(t.Context(), app.DB, d.ID, "render")
	if err != nil {
		t.Fatal(err)
	}
	broken := template.Must(template.New("layout").Parse(`partial HTML {{.MissingField}}`))
	app.baseTemplates = broken
	for _, path := range []string{"/rest/docs", "/graphql/docs"} {
		if r := ui.get(path); r.Code != 500 {
			t.Fatalf("%s: %d", path, r.Code)
		}
	}
	for _, page := range []string{"shorturls", "tags", "visits_shorturl", "visits_orphan"} {
		app.pages[page] = broken
	}
	for _, path := range []string{"/admin/short-urls", "/admin/tags", "/admin/visits/orphan", fmt.Sprintf("/admin/short-urls/%d/visits", link.ID)} {
		r := ui.get(path)
		if r.Code != 500 || strings.Contains(r.Body.String(), "partial HTML") {
			t.Fatalf("%s: %d %s", path, r.Code, r.Body.String())
		}
	}
	r := ui.postForm("/admin/tags/rename", url.Values{"oldName": {"missing"}, "newName": {""}}.Encode())
	if r.Code != 500 || strings.Contains(r.Body.String(), "partial HTML") {
		t.Fatalf("rename: %d %s", r.Code, r.Body.String())
	}
}
