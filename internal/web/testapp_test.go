package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

// newTestApp uses a temporary SQLite database, or an isolated PostgreSQL schema
// when GORT_TEST_POSTGRES_DSN is set to a PostgreSQL URL.
func newTestApp(t *testing.T) *App {
	t.Helper()
	return newTestAppWithConfig(t, nil)
}

func newTestAppWithConfig(t *testing.T, overrides map[string]string) *App {
	t.Helper()
	dataDir := t.TempDir()

	vars := map[string]string{
		"DEFAULT_DOMAIN":         "example.test",
		"DB_CONNECTION":          dataDir + "/test.db",
		"DATA_DIR":               dataDir,
		"AUTO_RESOLVE_TITLES":    "false",
		"INITIAL_ADMIN_USERNAME": "admin",
		"INITIAL_ADMIN_PASSWORD": "test-password-123",
		"RATE_LIMIT_PER_MINUTE":  "10000",
	}
	if dsn := os.Getenv("GORT_TEST_POSTGRES_DSN"); dsn != "" {
		pg, err := data.Open(data.Postgres, dsn)
		if err != nil {
			t.Fatal(err)
		}
		token := make([]byte, 12)
		if _, err := rand.Read(token); err != nil {
			t.Fatal(err)
		}
		schema := "gort_test_" + hex.EncodeToString(token)
		if _, err := pg.Exec(t.Context(), "CREATE SCHEMA "+schema); err != nil {
			_ = pg.Close()
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := pg.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
				t.Error(err)
			}
			_ = pg.Close()
		})
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		q := parsed.Query()
		q.Set("search_path", schema)
		parsed.RawQuery = q.Encode()
		vars["DB_DRIVER"], vars["DB_CONNECTION"] = "postgres", parsed.String()
	}
	for key, value := range overrides {
		vars[key] = value
	}
	cfg, err := ConfigFromLookup(func(name string) (string, bool) {
		v, ok := vars[name]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := NewApp(cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.DB.Close() })
	return app
}

// createApiKey seeds an API key with the given role and returns the
// plaintext key.
func createAPIKey(t *testing.T, app *App, role core.APIKeyRole) string {
	t.Helper()
	plain := GenerateAPIKey()
	name := "test"
	if _, err := data.InsertAPIKey(t.Context(), app.DB, HashAPIKey(plain), &name, role, nil); err != nil {
		t.Fatal(err)
	}
	return plain
}

type testClient struct {
	t       *testing.T
	app     *App
	headers map[string]string
}

func (app *App) client(t *testing.T) *testClient {
	return &testClient{t: t, app: app, headers: map[string]string{}}
}

func (app *App) adminClient(t *testing.T) *testClient {
	c := app.client(t)
	c.headers["X-Api-Key"] = createAPIKey(t, app, core.AdminRole())
	return c
}

func (c *testClient) do(method, target string, body string) *httptest.ResponseRecorder {
	c.t.Helper()
	if !strings.Contains(target, "://") {
		target = "http://example.test" + target
	}
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	c.app.Handler().ServeHTTP(rec, req)
	return rec
}

func (c *testClient) get(target string) *httptest.ResponseRecorder {
	return c.do(http.MethodGet, target, "")
}

func (c *testClient) post(target, body string) *httptest.ResponseRecorder {
	return c.do(http.MethodPost, target, body)
}

// postForm submits an application/x-www-form-urlencoded body, like a browser
// form.
func (c *testClient) postForm(target, form string) *httptest.ResponseRecorder {
	c.t.Helper()
	if !strings.Contains(target, "://") {
		target = "http://example.test" + target
	}
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	c.app.Handler().ServeHTTP(rec, req)
	return rec
}

func (c *testClient) patch(target, body string) *httptest.ResponseRecorder {
	return c.do(http.MethodPatch, target, body)
}

func (c *testClient) put(target, body string) *httptest.ResponseRecorder {
	return c.do(http.MethodPut, target, body)
}

func (c *testClient) delete(target string) *httptest.ResponseRecorder {
	return c.do(http.MethodDelete, target, "")
}
