package web

import (
	"fmt"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

func TestListFilterRESTGraphQLParity(t *testing.T) {
	app := newTestAppWithConfig(t, map[string]string{"WEBHOOKS_ENABLED": "true"})
	c := app.adminClient(t)
	for _, body := range []string{
		`{"longUrl":"https://example.com/a","customSlug":"aaa","tags":["one","two"]}`,
		`{"longUrl":"https://example.com/b","customSlug":"bbb","tags":["one"],"group":"ops","maxVisits":1}`,
		`{"longUrl":"https://example.com/c","customSlug":"ccc","tags":["two"],"validUntil":"2000-01-01T00:00:00Z"}`,
	} {
		createShort(t, c, body)
	}
	d, err := data.DefaultDomain(t.Context(), app.DB)
	if err != nil {
		t.Fatal(err)
	}
	date := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	for _, code := range []string{"aaa", "bbb"} {
		link, err := data.ShortURLDetailByCode(t.Context(), app.DB, d.ID, code)
		if err != nil {
			t.Fatal(err)
		}
		for _, bot := range []bool{false, true} {
			if _, err := data.InsertVisit(t.Context(), app.DB, data.NewVisit{ShortURLID: &link.ID, VisitType: core.VisitValidShortURL, VisitedAt: date, IsBot: bot, Device: core.DeviceDesktop}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tc := range []struct {
		query, filter string
		total         int
	}{
		{"page=bad&itemsPerPage=bad&startDate=bad&endDate=bad", `{page:1,itemsPerPage:20,startDate:"bad",endDate:"bad"}`, 3},
		{"group=", `{group:""}`, 2},
		{"group=%20ops%20", `{group:" ops "}`, 1},
		{"tags=one&tags[]=two&tagsMode=ALL", `{tags:["one","two"],tagsMode:"ALL"}`, 1},
		{"tags[]=one&tags[]=two&tagsMode=any&orderBy=shortCode-DESC&page=999&itemsPerPage=1", `{tags:["one","two"],tagsMode:"any",orderBy:"shortCode-DESC",page:999,itemsPerPage:1}`, 3},
		{"excludeMaxVisitsReached=YES&excludePastValidUntil=1", `{excludeMaxVisitsReached:true,excludePastValidUntil:true}`, 1},
		{"domain=missing.test", `{domain:"missing.test"}`, 0},
		{"orderBy=visits-DESC&page=0&itemsPerPage=0", `{orderBy:"visits-DESC",page:0,itemsPerPage:0}`, 3},
	} {
		t.Run(tc.query, func(t *testing.T) {
			r := c.get("/rest/v1/short-urls?" + tc.query)
			if r.Code != 200 {
				t.Fatal(r.Body.String())
			}
			rest := parseJSON(t, r.Body.String())
			graph := graphOK(t, c, `{shortURLs(filter:`+tc.filter+`){data{shortCode} pagination{currentPage pagesCount itemsPerPage itemsInCurrentPage totalItems}}}`, nil)["shortURLs"].(map[string]any)
			if !reflect.DeepEqual(rest["pagination"], graph["pagination"]) || jsonAt(rest, "pagination", "totalItems") != float64(tc.total) {
				t.Fatalf("pages differ: REST %v GraphQL %v", rest, graph)
			}
			restRows, graphRows := rest["data"].([]any), graph["data"].([]any)
			for i, row := range restRows {
				if jsonAt(row, "shortCode") != jsonAt(graphRows[i], "shortCode") {
					t.Fatalf("order differs: REST %v GraphQL %v", rest, graph)
				}
			}
		})
	}
	// All visit routes preserve dates, bot exclusion and bounded pagination.
	q := "startDate=2025-01-15&endDate=2025-01-16&excludeBots=YeS&page=999&itemsPerPage=1"
	filter := `{startDate:"2025-01-15",endDate:"2025-01-16",excludeBots:true,page:999,itemsPerPage:1}`
	for _, tc := range []struct {
		path, field string
		nested      bool
		total       int
	}{
		{"/rest/v1/visits/non-orphan", `visits(filter:` + filter + `)`, false, 2},
		{"/rest/v1/short-urls/aaa/visits", `shortURL(code:"aaa"){visits(filter:` + filter + `)`, true, 1},
		{"/rest/v1/tags/one/visits", `tagVisits(tag:"one",filter:` + filter + `)`, false, 2},
		{"/rest/v1/domains/example.test/visits", `domainVisits(authority:"example.test",filter:` + filter + `)`, false, 2},
	} {
		r := c.get(tc.path + "?" + q)
		rest := parseJSON(t, r.Body.String())
		if r.Code != 200 || jsonAt(rest, "pagination", "totalItems") != float64(tc.total) {
			t.Fatalf("%s: %s", tc.path, r.Body.String())
		}
		query := `{` + tc.field + `{data{potentialBot} pagination{currentPage totalItems}}}`
		if tc.nested {
			query += "}"
		}
		graph := graphOK(t, c, query, nil)
		for _, row := range rest["data"].([]any) {
			if jsonAt(row, "potentialBot") != false {
				t.Fatal("bot included")
			}
		}
		// Each GraphQL field yields one page, with an extra parent for shortURL.
		if tc.nested {
			graph = graph["shortURL"].(map[string]any)
		}
		for _, page := range graph {
			if jsonAt(page, "pagination", "totalItems") != float64(tc.total) || jsonAt(page, "pagination", "currentPage") != float64(tc.total) {
				t.Fatalf("GraphQL visits: %v", graph)
			}
		}
	}
	for _, kind := range []core.VisitType{core.VisitOrphanBaseURL, core.VisitOrphanInvalidShort} {
		if _, err := data.InsertVisit(t.Context(), app.DB, data.NewVisit{VisitType: kind, VisitedAt: date}); err != nil {
			t.Fatal(err)
		}
	}
	orphan := c.get("/rest/v1/visits/orphan?type=invalid_short_url&" + q)
	if jsonAt(parseJSON(t, orphan.Body.String()), "pagination", "totalItems") != float64(1) {
		t.Fatal(orphan.Body.String())
	}
	orphanGraph := graphOK(t, c, `{orphanVisits(type:"invalid_short_url",filter:`+filter+`){pagination{totalItems}}}`, nil)
	if jsonAt(orphanGraph, "orphanVisits", "pagination", "totalItems") != float64(1) {
		t.Fatal(orphanGraph)
	}
	stats := c.get("/rest/v1/stats/visits-per-day?orphan=YES&startDate=2025-01-15&endDate=2025-01-16")
	days := graphOK(t, c, `{visitsPerDay(scope:{orphan:true,startDate:"2025-01-15",endDate:"2025-01-16"}){date count}}`, nil)
	if !reflect.DeepEqual(parseJSON(t, stats.Body.String())["data"], days["visitsPerDay"]) {
		t.Fatal("orphan statistics differ")
	}
	for _, limit := range []string{"invalid", "0", "999"} {
		n := 25
		if limit == "0" {
			n = 0
		} else if limit == "999" {
			n = 999
		}
		rest := c.get("/rest/v1/stats/breakdown?by=device&shortCode=aaa&startDate=2025-01-15&endDate=2025-01-16&limit=" + limit)
		graph := graphOK(t, c, fmt.Sprintf(`{breakdown(by:"device",scope:{shortCode:"aaa",startDate:"2025-01-15",endDate:"2025-01-16"},limit:%d){value count}}`, n), nil)
		if !reflect.DeepEqual(parseJSON(t, rest.Body.String())["data"], graph["breakdown"]) {
			t.Fatal("breakdown differs")
		}
	}
	// Tag defaults differ from link/visit defaults, and both aliases still delete tags.
	r := c.get("/rest/v1/tags?withStats=1&itemsPerPage=bad")
	rest := parseJSON(t, r.Body.String())
	graph := graphOK(t, c, `{tags{data{tag shortUrlsCount visitsCount} pagination{itemsPerPage}}}`, nil)["tags"].(map[string]any)
	if jsonAt(rest, "pagination", "itemsPerPage") != float64(core.MaxPageSize) || !reflect.DeepEqual(rest["data"], graph["data"]) {
		t.Fatalf("tags: %v %v", rest, graph)
	}
	r = c.delete("/rest/v1/tags?tags=" + url.QueryEscape("one, missing") + "&tags[]=two")
	if r.Code != 200 || jsonAt(parseJSON(t, r.Body.String()), "deletedTags") != float64(2) {
		t.Fatal(r.Body.String())
	}
	scoped := app.client(t)
	scoped.headers["X-Api-Key"] = createAPIKey(t, app, core.AuthorRole())
	for _, resource := range []string{"api-keys", "webhooks"} {
		// Malformed IDs retain 404 for admins and do not skip scope checks.
		for _, id := range []string{"invalid", "999999999999999999999999"} {
			if r := scoped.patch(fmt.Sprintf("/rest/v1/%s/%s", resource, id), `{"enabled":false}`); r.Code != 403 {
				t.Fatalf("scope check: %d %s", r.Code, r.Body.String())
			}
			if r := c.patch(fmt.Sprintf("/rest/v1/%s/%s", resource, id), `{"enabled":false}`); r.Code != 404 {
				t.Fatalf("ID: %d %s", r.Code, r.Body.String())
			}
		}
	}
}
