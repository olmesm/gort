package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

func TestListPagesPaginationAndFilters(t *testing.T) {
	app := newTestAppWithConfig(t, map[string]string{"WEBHOOKS_ENABLED": "true"})
	ctx := t.Context()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := app.DB.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	now := app.DB.BindTime(time.Now())
	defaultDomain, err := data.DefaultDomain(ctx, app.DB)
	if err != nil {
		t.Fatal(err)
	}
	var shortURLID int64
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("a-item-%03d", i)
		exec("INSERT INTO users (username, password_hash, role, created_at) VALUES (?, '', 'user', ?)", name, now)
		exec("INSERT INTO domains (authority, is_default, created_at) VALUES (?, ?, ?)", name+".test", false, now)
		exec("INSERT INTO api_keys (key_hash, name, role, enabled, created_at) VALUES (?, ?, 'author', ?, ?)", name, name, i%2 == 0, now)
		event := "url.created"
		if i%2 != 0 {
			event = "visit.recorded"
		}
		exec("INSERT INTO webhooks (name, url, secret, events, enabled, created_at) VALUES (?, ?, 'secret', ?, ?, ?)", name, "https://"+name+".test/hook", event, i%2 == 0, now)
		exec("INSERT INTO tags (name) VALUES (?)", name)
		var id int64
		if err := app.DB.QueryRow(ctx, "INSERT INTO short_urls (short_code, domain_id, long_url, created_at) VALUES (?, ?, ?, ?) RETURNING id", name, defaultDomain.ID.Value(), "https://example.test/"+name, now).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			shortURLID = id
		}
		exec("INSERT INTO visits (short_url_id, visit_type, visited_at) VALUES (?, 'valid', ?)", shortURLID, now)
		exec("INSERT INTO visits (visit_type, visited_at) VALUES ('invalid_short_url', ?)", now)
		exec("INSERT INTO redirect_rules (short_url_id, priority, long_url) VALUES (?, ?, ?)", shortURLID, i+1, "https://example.test/"+name)
	}
	// An expired key must match the expired filter, regardless of its enabled flag.
	exec("INSERT INTO api_keys (key_hash, name, role, enabled, expires_at, created_at) VALUES ('expired', 'expired-key', 'admin', ?, ?, ?)", false, app.DB.BindTime(time.Now().Add(-time.Hour)), now)
	admin, err := data.UserByUsername(ctx, app.DB, "admin")
	if err != nil {
		t.Fatal(err)
	}
	signedIn := httptest.NewRecorder()
	app.SignIn(signedIn, admin)
	client := app.client(t)
	client.headers["Cookie"] = signedIn.Result().Cookies()[0].String()

	bodyFor := func(path string) string {
		t.Helper()
		response := client.get(path)
		if response.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", path, response.Code, response.Body.String())
		}
		return response.Body.String()
	}
	for _, resource := range []string{"webhooks", "users", "domains", "api-keys", "tags", "short-urls", fmt.Sprintf("short-urls/%d/edit", shortURLID)} {
		t.Run(resource, func(t *testing.T) {
			base := "/admin/" + resource
			for page := 1; page <= 4; page++ {
				body := bodyFor(fmt.Sprintf("%s?search=a-item-&page=%d", base, page))
				want := fmt.Sprintf("Page %d of 4 · 100 items", page)
				if !strings.Contains(body, want) {
					t.Fatalf("missing %q", want)
				}
				table := strings.Split(strings.Split(body, "<tbody>")[1], "</tbody>")[0]
				if got := strings.Count(table, "<tr>"); got != 25 {
					t.Fatalf("page %d has %d rows", page, got)
				}
				if page < 4 && (!strings.Contains(body, fmt.Sprintf("page=%d", page+1)) || !strings.Contains(body, "search=a-item-")) {
					t.Fatal("next page drops search")
				}
			}
			empty := bodyFor(base + "?search=no-such-item")
			if !strings.Contains(empty, "Page 1 of 1 · 0 items") || !strings.Contains(empty, "No ") {
				t.Fatal("missing empty state")
			}
		})
	}
	for _, tc := range []struct{ path, want string }{
		{"/admin/webhooks?search=a-item-&status=disabled&event=visit.recorded&page=2", "Page 2 of 2 · 50 items"},
		{"/admin/webhooks?search=a-item-&status=enabled&event=visit.recorded", "Page 1 of 1 · 0 items"},
		{"/admin/webhooks?search=https%3A%2F%2Fa-item-099.test", "Page 1 of 1 · 1 items"},
		{"/admin/users?role=admin", "Page 1 of 1 · 1 items"},
		{"/admin/users?role=user&page=2", "Page 2 of 4 · 100 items"},
		{"/admin/domains?status=default", "Page 1 of 1 · 1 items"},
		{"/admin/domains?status=additional&page=2", "Page 2 of 4 · 100 items"},
		{"/admin/api-keys?status=disabled&role=author&page=2", "Page 2 of 2 · 50 items"},
		{"/admin/api-keys?status=expired", "Page 1 of 1 · 1 items"},
		{"/admin/api-keys?status=disabled&role=admin", "Page 1 of 1 · 0 items"},
		{"/admin/webhooks?search=A-ITEM-&page=-1", "Page 1 of 4 · 100 items"},
		{"/admin/webhooks?search=a-item-&page=99999999999999", "Page 4 of 4 · 100 items"},
	} {
		if body := bodyFor(tc.path); !strings.Contains(body, tc.want) {
			t.Errorf("%s: missing %q", tc.path, tc.want)
		}
	}
	body := bodyFor("/admin/webhooks?search=a-item-&status=disabled&event=visit.recorded")
	if !strings.Contains(body, "event=visit.recorded&amp;page=2&amp;search=a-item-&amp;status=disabled") {
		t.Fatal("paging drops combined filters")
	}
	// The sole admin appears after 100 regular users in name order.
	body = bodyFor("/admin/users?page=5")
	if !strings.Contains(body, `<select name="role" disabled>`) {
		t.Fatal("last admin protection missing on later page")
	}
	model, err := app.usersViewModel(ctx, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if model.LastAdminID != core.UserID(0) {
		t.Fatal("off-page admin unexpectedly present in model")
	}
	// Display pagination must never truncate the rules used by redirects.
	rules, err := data.RedirectRules(ctx, app.DB, core.ShortURLID(shortURLID))
	if err != nil || len(rules) != 100 {
		t.Fatalf("redirect engine rules: %d, %v", len(rules), err)
	}
	for _, path := range []string{fmt.Sprintf("/admin/short-urls/%d/visits", shortURLID), "/admin/visits/orphan"} {
		body := bodyFor(path + "?startDate=2000-01-01T00%3A00&page=2")
		if !strings.Contains(body, "Page 2 of 4 · 100 items") || !strings.Contains(body, "startDate=2000-01-01T00%3A00") {
			t.Fatalf("visit paging failed for %s", path)
		}
	}
	// Search via HTMX must produce the same filtered pagination links.
	client.headers["HX-Request"] = "true"
	body = bodyFor("/admin/tags?search=a-item-")
	if !strings.Contains(body, "page=2&amp;search=a-item-") {
		t.Fatal("HTMX tags drops search")
	}
}
