package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func createShort(t *testing.T, client *testClient, body string) string {
	t.Helper()
	resp := client.post("/rest/v1/short-urls", body)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", resp.Code, resp.Body.String())
	}
	return parseJson(t, resp.Body.String())["shortCode"].(string)
}

func (c *testClient) getWithHeaders(target string, headers map[string]string) *httptest.ResponseRecorder {
	c.t.Helper()
	if !strings.Contains(target, "://") {
		target = "http://example.test" + target
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	c.app.Handler().ServeHTTP(rec, req)
	return rec
}

func TestValidShortUrlsRedirectAndRecordAVisit(t *testing.T) {
	app := newTestApp(t)
	admin := app.adminClient(t)
	code := createShort(t, admin, `{"longUrl":"https://example.com/target"}`)

	visitor := app.client(t)
	resp := visitor.getWithHeaders("/"+code, map[string]string{
		"User-Agent": "Mozilla/5.0 (Macintosh) Firefox",
		"Referer":    "https://google.com/",
	})
	if resp.Code != http.StatusFound {
		t.Fatalf("status %d", resp.Code)
	}
	if loc := resp.Header().Get("Location"); loc != "https://example.com/target" {
		t.Fatalf("location %q", loc)
	}

	visits := parseJson(t, admin.get("/rest/v1/short-urls/"+code+"/visits").Body.String())
	visit := visits["data"].([]any)[0].(map[string]any)
	if visit["referer"] != "https://google.com/" {
		t.Errorf("referer: %v", visit["referer"])
	}
	if visit["device"] != "desktop" {
		t.Errorf("device: %v", visit["device"])
	}
	if visit["potentialBot"].(bool) {
		t.Error("should not be a bot")
	}
}

func TestConfiguredRedirectStatusIsUsed(t *testing.T) {
	app := newTestApp(t)
	admin := app.adminClient(t)
	code := createShort(t, admin, `{"longUrl":"https://example.com/permanent","redirectStatus":301}`)

	resp := app.client(t).get("/" + code)
	if resp.Code != http.StatusMovedPermanently {
		t.Fatalf("status %d", resp.Code)
	}
}

func TestQueryParamsAreForwardedWhenEnabled(t *testing.T) {
	app := newTestApp(t)
	admin := app.adminClient(t)

	code := createShort(t, admin, `{"longUrl":"https://example.com/p?fixed=1"}`)
	resp := app.client(t).get("/" + code + "?utm_source=mail")
	if loc := resp.Header().Get("Location"); loc != "https://example.com/p?fixed=1&utm_source=mail" {
		t.Errorf("location %q", loc)
	}

	code2 := createShort(t, admin, `{"longUrl":"https://example.com/q","forwardQuery":false}`)
	resp2 := app.client(t).get("/" + code2 + "?utm_source=mail")
	if loc := resp2.Header().Get("Location"); loc != "https://example.com/q" {
		t.Errorf("location %q", loc)
	}
}

func TestDeviceRulesPickTheRightTarget(t *testing.T) {
	app := newTestApp(t)
	admin := app.adminClient(t)
	code := createShort(t, admin, `{"longUrl":"https://example.com/default"}`)
	admin.post("/rest/v1/short-urls/"+code+"/redirect-rules",
		`{"redirectRules":[{"longUrl":"https://example.com/droid","conditions":[{"type":"device","matchValue":"android"}]}]}`)

	visitor := app.client(t)
	android := visitor.getWithHeaders("/"+code, map[string]string{"User-Agent": "Mozilla/5.0 (Linux; Android 14)"})
	if loc := android.Header().Get("Location"); loc != "https://example.com/droid" {
		t.Errorf("android location %q", loc)
	}
	desktop := visitor.getWithHeaders("/"+code, map[string]string{"User-Agent": "Mozilla/5.0 (Macintosh)"})
	if loc := desktop.Header().Get("Location"); loc != "https://example.com/default" {
		t.Errorf("desktop location %q", loc)
	}
}

func TestMaxVisitsExhaustsTheShortUrl(t *testing.T) {
	app := newTestApp(t)
	admin := app.adminClient(t)
	code := createShort(t, admin, `{"longUrl":"https://example.com/limited","maxVisits":2}`)

	visitor := app.client(t)
	first := visitor.get("/" + code)
	second := visitor.get("/" + code)
	third := visitor.get("/" + code)
	if first.Code != http.StatusFound || second.Code != http.StatusFound {
		t.Fatalf("first/second: %d/%d", first.Code, second.Code)
	}
	if third.Code != http.StatusNotFound {
		t.Fatalf("third: %d", third.Code)
	}
}

func TestValidityWindowIsEnforced(t *testing.T) {
	app := newTestApp(t)
	admin := app.adminClient(t)
	expired := createShort(t, admin, `{"longUrl":"https://example.com/old","validUntil":"2000-01-01T00:00:00Z"}`)
	notYet := createShort(t, admin, `{"longUrl":"https://example.com/future","validSince":"2100-01-01T00:00:00Z"}`)

	visitor := app.client(t)
	if resp := visitor.get("/" + expired); resp.Code != http.StatusNotFound {
		t.Errorf("expired: %d", resp.Code)
	}
	if resp := visitor.get("/" + notYet); resp.Code != http.StatusNotFound {
		t.Errorf("not yet: %d", resp.Code)
	}
}

func TestShortCodesAreScopedToTheirDomain(t *testing.T) {
	app := newTestApp(t)
	admin := app.adminClient(t)
	resp := admin.post("/rest/v1/short-urls",
		`{"longUrl":"https://example.com/other-domain","customSlug":"scoped","domain":"links.test"}`)
	doc := parseJson(t, resp.Body.String())
	if doc["domain"] != "links.test" {
		t.Fatalf("domain: %v", doc["domain"])
	}

	visitor := app.client(t)
	// On the registered domain the code resolves…
	onDomain := visitor.get("http://links.test/scoped")
	if onDomain.Code != http.StatusFound {
		t.Errorf("on domain: %d", onDomain.Code)
	}
	// …on the default domain it does not.
	onDefault := visitor.get("http://example.test/scoped")
	if onDefault.Code != http.StatusNotFound {
		t.Errorf("on default: %d", onDefault.Code)
	}
}

func TestUnknownShortCodesAreTrackedAsOrphanVisits(t *testing.T) {
	app := newTestApp(t)
	admin := app.adminClient(t)
	visitor := app.client(t)

	notFound := visitor.get("/definitely-missing")
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("status %d", notFound.Code)
	}
	visitor.get("/")

	orphans := parseJson(t, admin.get("/rest/v1/visits/orphan").Body.String())
	items := orphans["data"].([]any)
	if len(items) < 2 {
		t.Fatalf("orphan count: %d", len(items))
	}
	found := false
	for _, item := range items {
		if url, ok := item.(map[string]any)["visitedUrl"].(string); ok &&
			strings.Contains(url, "definitely-missing") {
			found = true
		}
	}
	if !found {
		t.Error("missing orphan for definitely-missing")
	}
}

func TestDomainLevelBaseUrlRedirectWinsOverTheLandingPage(t *testing.T) {
	app := newTestApp(t)
	admin := app.adminClient(t)
	admin.patch("/rest/v1/domains/redirects",
		`{"domain":"example.test","baseUrlRedirect":"https://company.example.com"}`)

	resp := app.client(t).get("http://example.test/")
	if resp.Code != http.StatusFound {
		t.Fatalf("status %d", resp.Code)
	}
	if loc := strings.TrimRight(resp.Header().Get("Location"), "/"); loc != "https://company.example.com" {
		t.Errorf("location %q", loc)
	}
}

func TestRobotsTxtListsCrawlableShortUrls(t *testing.T) {
	app := newTestApp(t)
	admin := app.adminClient(t)
	code := createShort(t, admin, `{"longUrl":"https://example.com/crawl","crawlable":true}`)

	robots := app.client(t).get("/robots.txt").Body.String()
	if !strings.Contains(robots, "Allow: /"+code) {
		t.Errorf("missing allow: %s", robots)
	}
	if !strings.Contains(robots, "Disallow: /") {
		t.Errorf("missing disallow: %s", robots)
	}
}

func TestQrCodesAreServedInPngAndSvg(t *testing.T) {
	app := newTestApp(t)
	admin := app.adminClient(t)
	code := createShort(t, admin, `{"longUrl":"https://example.com/qr"}`)

	visitor := app.client(t)
	png := visitor.get("/" + code + "/qr-code")
	if png.Code != http.StatusOK {
		t.Fatalf("png status %d", png.Code)
	}
	if ct := png.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("png content type %q", ct)
	}
	svg := visitor.get("/" + code + "/qr-code?format=svg&size=200")
	if ct := svg.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Errorf("svg content type %q", ct)
	}
	missing := visitor.get("/no-such-code/qr-code")
	if missing.Code != http.StatusNotFound {
		t.Errorf("missing status %d", missing.Code)
	}
}

func TestVisitsAreCountedPerRequest(t *testing.T) {
	app := newTestApp(t)
	admin := app.adminClient(t)
	code := createShort(t, admin, `{"longUrl":"https://example.com/counted"}`)

	app.client(t).get("/" + code)
	doc := parseJson(t, admin.get("/rest/v1/short-urls/"+code).Body.String())
	total := doc["visitsSummary"].(map[string]any)["total"].(float64)
	if total != 1 {
		t.Errorf("total visits: %v", total)
	}
}
