package web

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

//go:embed static/*
var staticFiles embed.FS

// App wires configuration, persistence, queues and background services
// together and exposes the HTTP handler.
type App struct {
	Cfg    *AppConfig
	Db     *data.Db
	Queues *WorkQueues
	Geo    *GeoIpService
	Logger *slog.Logger

	sessionKey    []byte
	oidc          *oidcClient
	baseTemplates *template.Template
	pages         map[string]*template.Template
	titleClient   *http.Client
	webhookClient *http.Client
	geoClient     *http.Client
	limiter       *rateLimiter
	mux           *http.ServeMux
}

// NewApp builds the application: opens the database, runs migrations,
// registers the default domain, bootstraps the first admin user and mounts
// all routes. Call StartWorkers to launch background processing.
func NewApp(cfg *AppConfig, logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}

	db, err := data.Open(cfg.DbDialect, cfg.ConnectionString)
	if err != nil {
		return nil, err
	}

	sessionKey, err := loadOrCreateSessionKey(cfg.DataDir)
	if err != nil {
		return nil, err
	}

	a := &App{
		Cfg:           cfg,
		Db:            db,
		Queues:        NewWorkQueues(),
		Logger:        logger,
		sessionKey:    sessionKey,
		titleClient:   &http.Client{Timeout: 10 * time.Second},
		webhookClient: &http.Client{Timeout: 15 * time.Second},
		geoClient:     &http.Client{Timeout: 5 * time.Minute},
		limiter:       newRateLimiter(cfg.RateLimitPerMinute),
	}
	a.Geo = NewGeoIpService(cfg, logger)
	a.baseTemplates, a.pages = parseTemplates()
	if cfg.OidcEnabled() {
		if cfg.OidcClientID == "" {
			return nil, fmt.Errorf("GORT_OIDC_ISSUER is set but GORT_OIDC_CLIENT_ID is empty")
		}
		a.oidc = newOidcClient(cfg)
	}

	if err := a.initialize(context.Background()); err != nil {
		return nil, err
	}
	a.mux = a.buildRouter()
	return a, nil
}

// initialize runs migrations, registers the default domain and bootstraps
// the first admin user.
func (a *App) initialize(ctx context.Context) error {
	if err := data.Migrate(ctx, a.Db); err != nil {
		return err
	}
	if _, err := data.EnsureDefaultDomain(ctx, a.Db, a.Cfg.DefaultDomain); err != nil {
		return err
	}

	userCount, err := data.CountUsers(ctx, a.Db)
	if err != nil {
		return err
	}
	if userCount == 0 {
		username := a.Cfg.InitialAdminUsername
		if username == "" {
			username = "admin"
		}
		password := a.Cfg.InitialAdminPassword
		generated := false
		if password == "" {
			bytes := make([]byte, 12)
			if _, err := rand.Read(bytes); err != nil {
				return err
			}
			password = base64.RawURLEncoding.EncodeToString(bytes)
			generated = true
		}
		created, err := data.InsertUser(ctx, a.Db, username, HashPassword(password), core.UserAdmin)
		if err != nil {
			return err
		}
		if created != nil {
			if generated {
				a.Logger.Warn(fmt.Sprintf(
					"Created initial admin user '%s' with generated password: %s — log in at /admin/login and change it.",
					username, password))
			} else {
				a.Logger.Info(fmt.Sprintf("Created initial admin user '%s'.", username))
			}
		}
	}
	return nil
}

// ---- Rate limiting ----

// rateLimiter is a fixed-window limiter for mutating REST calls, partitioned
// by client IP.
type rateLimiter struct {
	limit  int
	mu     sync.Mutex
	window time.Time
	counts map[string]int
}

func newRateLimiter(limit int) *rateLimiter {
	return &rateLimiter{limit: limit, counts: map[string]int{}}
}

func (l *rateLimiter) allow(key string) bool {
	if l.limit <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now().Truncate(time.Minute)
	if !now.Equal(l.window) {
		l.window = now
		l.counts = map[string]int{}
	}
	l.counts[key]++
	return l.counts[key] <= l.limit
}

func (a *App) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isMutatingRest := strings.HasPrefix(r.URL.Path, "/rest") &&
			r.Method != http.MethodGet && r.Method != http.MethodHead
		if isMutatingRest && a.Cfg.RateLimitPerMinute > 0 {
			key := RemoteIP(r)
			if key == "" {
				key = "unknown"
			}
			if !a.limiter.allow(key) {
				a.handleError(w, NewProblem(429, "rate-limit", "Too many requests", "Rate limit exceeded; retry in a minute."))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---- Routing ----

func (a *App) buildRouter() *http.ServeMux {
	mux := http.NewServeMux()

	// Static assets served from the embedded filesystem.
	serveAsset := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			http.ServeFileFS(w, r, staticFiles, "static/"+name)
		}
	}
	for _, asset := range []string{"app.css", "htmx.min.js", "inter-var.woff2"} {
		mux.HandleFunc("GET /"+asset, serveAsset(asset))
	}

	// REST API
	mux.Handle("GET /rest/health", a.handle(a.handleHealth))

	mux.Handle("GET /rest/v1/short-urls", a.requireApiKey(a.apiListShortUrls))
	mux.Handle("POST /rest/v1/short-urls", a.requireApiKey(a.apiCreateShortUrl))
	mux.Handle("GET /rest/v1/short-urls/{code}", a.requireApiKey(a.apiGetShortUrl))
	mux.Handle("PATCH /rest/v1/short-urls/{code}", a.requireApiKey(a.apiEditShortUrl))
	mux.Handle("DELETE /rest/v1/short-urls/{code}", a.requireApiKey(a.apiDeleteShortUrl))
	mux.Handle("GET /rest/v1/short-urls/{code}/redirect-rules", a.requireApiKey(a.apiGetRules))
	mux.Handle("POST /rest/v1/short-urls/{code}/redirect-rules", a.requireApiKey(a.apiSetRules))
	mux.Handle("GET /rest/v1/short-urls/{code}/visits", a.requireApiKey(a.apiListShortUrlVisits))
	mux.Handle("DELETE /rest/v1/short-urls/{code}/visits", a.requireApiKey(a.apiDeleteShortUrlVisits))

	mux.Handle("GET /rest/v1/tags", a.requireApiKey(a.apiListTags))
	mux.Handle("PUT /rest/v1/tags", a.requireApiKey(a.apiRenameTag))
	mux.Handle("DELETE /rest/v1/tags", a.requireApiKey(a.apiDeleteTags))
	mux.Handle("GET /rest/v1/tags/{tag}/visits", a.requireApiKey(a.apiTagVisits))

	mux.Handle("GET /rest/v1/domains", a.requireApiKey(a.apiListDomains))
	mux.Handle("POST /rest/v1/domains", a.requireAdminKey(a.apiCreateDomain))
	mux.Handle("PATCH /rest/v1/domains/redirects", a.requireAdminKey(a.apiSetDomainRedirects))
	mux.Handle("DELETE /rest/v1/domains/{authority}", a.requireAdminKey(a.apiDeleteDomain))
	mux.Handle("GET /rest/v1/domains/{authority}/visits", a.requireApiKey(a.apiDomainVisits))

	mux.Handle("GET /rest/v1/visits", a.requireApiKey(a.apiVisitsOverview))
	mux.Handle("GET /rest/v1/visits/non-orphan", a.requireApiKey(a.apiListNonOrphanVisits))
	mux.Handle("GET /rest/v1/visits/orphan", a.requireApiKey(a.apiListOrphanVisits))
	mux.Handle("DELETE /rest/v1/visits/orphan", a.requireApiKey(a.apiDeleteOrphanVisits))
	mux.Handle("GET /rest/v1/stats/visits-per-day", a.requireApiKey(a.apiVisitsPerDay))
	mux.Handle("GET /rest/v1/stats/breakdown", a.requireApiKey(a.apiBreakdown))

	mux.Handle("GET /rest/v1/api-keys", a.requireAdminKey(a.apiListApiKeys))
	mux.Handle("POST /rest/v1/api-keys", a.requireAdminKey(a.apiCreateApiKey))
	mux.Handle("PATCH /rest/v1/api-keys/{id}", a.requireAdminKey(a.apiPatchApiKey))
	mux.Handle("DELETE /rest/v1/api-keys/{id}", a.requireAdminKey(a.apiDeleteApiKey))

	mux.Handle("GET /rest/v1/webhooks", a.requireAdminKey(a.apiListWebhooks))
	mux.Handle("POST /rest/v1/webhooks", a.requireAdminKey(a.apiCreateWebhook))
	mux.Handle("PATCH /rest/v1/webhooks/{id}", a.requireAdminKey(a.apiPatchWebhook))
	mux.Handle("DELETE /rest/v1/webhooks/{id}", a.requireAdminKey(a.apiDeleteWebhook))

	// Dashboard
	mux.Handle("GET /admin", a.requireUser(a.uiOverview))
	mux.Handle("GET /admin/login", a.handle(a.uiLoginForm))
	mux.Handle("POST /admin/login", a.handle(a.uiLogin))
	mux.Handle("POST /admin/logout", a.handle(a.uiLogout))
	mux.Handle("GET /admin/oidc/login", a.handle(a.uiOidcLogin))
	mux.Handle("GET /admin/oidc/callback", a.handle(a.uiOidcCallback))

	mux.Handle("GET /admin/short-urls", a.requireUser(a.uiListShortUrls))
	mux.Handle("GET /admin/short-urls/new", a.requireUser(a.uiCreateShortUrlForm))
	mux.Handle("POST /admin/short-urls/new", a.requireUser(a.uiCreateShortUrl))
	mux.Handle("GET /admin/short-urls/{id}/edit", a.requireUser(a.uiEditShortUrlForm))
	mux.Handle("POST /admin/short-urls/{id}/edit", a.requireUser(a.uiEditShortUrl))
	mux.Handle("POST /admin/short-urls/{id}/rules/add", a.requireUser(a.uiAddRule))
	mux.Handle("POST /admin/short-urls/{id}/rules/delete", a.requireUser(a.uiDeleteRule))
	mux.Handle("POST /admin/short-urls/{id}/delete", a.requireUser(a.uiDeleteShortUrl))
	mux.Handle("POST /admin/short-urls/{id}/visits/delete", a.requireUser(a.uiDeleteShortUrlVisits))
	mux.Handle("GET /admin/short-urls/{id}/visits", a.requireUser(a.uiShortUrlVisits))

	mux.Handle("GET /admin/visits/orphan", a.requireUser(a.uiOrphanVisits))
	mux.Handle("POST /admin/visits/orphan/delete", a.requireAdmin(a.uiDeleteOrphanVisits))

	mux.Handle("GET /admin/tags", a.requireUser(a.uiListTags))
	mux.Handle("POST /admin/tags/rename", a.requireUser(a.uiRenameTag))
	mux.Handle("POST /admin/tags/delete", a.requireUser(a.uiDeleteTag))

	mux.Handle("GET /admin/domains", a.requireAdmin(a.uiListDomains))
	mux.Handle("POST /admin/domains", a.requireAdmin(a.uiCreateDomain))
	mux.Handle("POST /admin/domains/{id}/redirects", a.requireAdmin(a.uiSetDomainRedirects))
	mux.Handle("POST /admin/domains/{id}/delete", a.requireAdmin(a.uiDeleteDomain))

	mux.Handle("GET /admin/api-keys", a.requireAdmin(a.uiListApiKeys))
	mux.Handle("POST /admin/api-keys", a.requireAdmin(a.uiCreateApiKey))
	mux.Handle("POST /admin/api-keys/{id}/toggle", a.requireAdmin(a.uiToggleApiKey))
	mux.Handle("POST /admin/api-keys/{id}/delete", a.requireAdmin(a.uiDeleteApiKey))

	mux.Handle("GET /admin/users", a.requireAdmin(a.uiListUsers))
	mux.Handle("POST /admin/users", a.requireAdmin(a.uiCreateUser))
	mux.Handle("POST /admin/users/{id}/role", a.requireAdmin(a.uiSetUserRole))
	mux.Handle("POST /admin/users/{id}/password", a.requireAdmin(a.uiSetUserPassword))
	mux.Handle("POST /admin/users/{id}/delete", a.requireAdmin(a.uiDeleteUser))

	mux.Handle("GET /admin/webhooks", a.requireAdmin(a.uiListWebhooks))
	mux.Handle("POST /admin/webhooks", a.requireAdmin(a.uiCreateWebhook))
	mux.Handle("POST /admin/webhooks/{id}/toggle", a.requireAdmin(a.uiToggleWebhook))
	mux.Handle("POST /admin/webhooks/{id}/delete", a.requireAdmin(a.uiDeleteWebhook))

	// Public
	mux.Handle("GET /robots.txt", a.handle(a.handleRobots))
	mux.Handle("GET /{code}/qr-code", a.handle(a.handleQrCode))
	mux.Handle("GET /{$}", a.handle(a.handleBaseUrl))
	// A "GET" pattern also serves HEAD requests.
	mux.Handle("GET /", a.handle(a.handleShortUrl))

	return mux
}

// Handler is the full middleware + routing pipeline.
func (a *App) Handler() http.Handler {
	return a.rateLimitMiddleware(a.mux)
}

// Run serves HTTP until the context is cancelled.
func (a *App) Run(ctx context.Context) error {
	a.StartWorkers(ctx)
	server := &http.Server{
		Addr:              fmt.Sprintf("0.0.0.0:%d", a.Cfg.Port),
		Handler:           a.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	a.Logger.Info(fmt.Sprintf("Gort listening on http://0.0.0.0:%d", a.Cfg.Port))
	err := server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
