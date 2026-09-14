package web

import (
	"strings"
	"testing"

	"github.com/olmesm/gort/internal/data"
)

func TestShortURLEditRollsBackFieldsAndTags(t *testing.T) {
	app := newTestApp(t)
	c := app.adminClient(t)
	createShort(t, c, `{"longUrl":"https://example.com/original","customSlug":"atomic","title":"Original","tags":["before"]}`)
	// Fail after deleting the old tag links and inserting the new tag name.
	trigger := `CREATE TRIGGER reject_tag_link BEFORE INSERT ON short_url_tags BEGIN SELECT RAISE(ABORT, 'tag write failed'); END`
	dropTrigger := "DROP TRIGGER reject_tag_link"
	if app.DB.Dialect == data.Postgres {
		if _, err := app.DB.Exec(t.Context(), `CREATE FUNCTION reject_tag_link() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'tag write failed'; END $$`); err != nil {
			t.Fatal(err)
		}
		trigger = `CREATE TRIGGER reject_tag_link BEFORE INSERT ON short_url_tags FOR EACH ROW EXECUTE FUNCTION reject_tag_link()`
		dropTrigger += " ON short_url_tags"
	}
	if _, err := app.DB.Exec(t.Context(), trigger); err != nil {
		t.Fatal(err)
	}
	for _, transport := range []string{"REST", "GraphQL"} {
		t.Run(transport, func(t *testing.T) {
			if transport == "REST" {
				r := c.patch("/rest/v1/short-urls/atomic", `{"longUrl":"https://example.com/changed","title":"Changed","tags":["after"]}`)
				if r.Code != 500 {
					t.Fatalf("expected failure: %d %s", r.Code, r.Body.String())
				}
			} else {
				doc := graphRequest(t, c, `mutation {updateShortURL(code:"atomic",input:{longUrl:"https://example.com/changed",title:"Changed",tags:["after"]}){shortCode}}`, nil)
				errs, ok := doc["errors"].([]any)
				if !ok || len(errs) != 1 || jsonAt(errs[0], "extensions", "status") != float64(500) || jsonAt(errs[0], "message") != internalProblem.Detail {
					t.Fatalf("unsafe error response: %v", doc)
				}
			}
			r := c.get("/rest/v1/short-urls/atomic")
			doc := parseJSON(t, r.Body.String())
			if doc["longUrl"] != "https://example.com/original" || doc["title"] != "Original" || len(doc["tags"].([]any)) != 1 || doc["tags"].([]any)[0] != "before" {
				t.Fatalf("partial edit: %v", doc)
			}
			var count int
			if err := app.DB.QueryRow(t.Context(), "SELECT COUNT(*) FROM tags WHERE name = 'after'").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatal("failed edit left a new tag behind")
			}
		})
	}
	// Omitting tags must still work even when tag writes fail.
	if r := c.patch("/rest/v1/short-urls/atomic", `{"title":"Fields only"}`); r.Code != 200 || !strings.Contains(r.Body.String(), `"tags":["before"]`) {
		t.Fatal(r.Body.String())
	}
	if _, err := app.DB.Exec(t.Context(), dropTrigger); err != nil {
		t.Fatal(err)
	}
	if r := c.patch("/rest/v1/short-urls/atomic", `{"title":"Committed","tags":["after"]}`); r.Code != 200 || !strings.Contains(r.Body.String(), `"tags":["after"]`) {
		t.Fatal(r.Body.String())
	}
	if r := c.patch("/rest/v1/short-urls/atomic", `{"tags":[]}`); r.Code != 200 || !strings.Contains(r.Body.String(), `"tags":[]`) {
		t.Fatal(r.Body.String())
	}
}
