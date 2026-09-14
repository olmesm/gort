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

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

//go:embed static/*
var staticFiles embed.FS

// App wires configuration, persistence, queues and background services
// together and exposes the HTTP handler.
type App struct {
	Cfg    *AppConfig
	DB     *data.DB
	Queues *WorkQueues
	Geo    *GeoIPService
	Logger *slog.Logger

	sessionKey    []byte
	oidc          *oidcClient
	baseTemplates *template.Template
	pages         map[string]*template.Template
	titleClient   *http.Client
	webhookClient *http.Client
	geoClient     *http.Client
	limiter       *rateLimiter
	mux           *chi.Mux
}

// NewApp builds the application: opens the database, runs migrations,
// registers the default domain, bootstraps the first admin user and mounts
// all routes. Call StartWorkers to launch background processing.
func NewApp(cfg *AppConfig, logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, err
	}

	db, err := data.Open(cfg.DBDialect, cfg.ConnectionString)
	if err != nil {
		return nil, err
	}

	sessionKey, err := loadOrCreateSessionKey(cfg.DataDir)
	if err != nil {
		return nil, err
	}

	a := &App{
		Cfg:           cfg,
		DB:            db,
		Queues:        NewWorkQueues(cfg.WebhooksEnabled),
		Logger:        logger,
		sessionKey:    sessionKey,
		titleClient:   newOutboundClient(10*time.Second, cfg.AllowPrivateOutbound),
		webhookClient: newOutboundClient(15*time.Second, cfg.AllowPrivateOutbound),
		geoClient:     &http.Client{Timeout: 5 * time.Minute},
		limiter:       newRateLimiter(cfg.RateLimitPerMinute),
	}
	a.Geo = NewGeoIPService(cfg, logger)
	a.baseTemplates, a.pages = parseTemplates()
	if cfg.OIDCEnabled() {
		if cfg.OIDCClientID == "" {
			return nil, fmt.Errorf("GORT_OIDC_ISSUER is set but GORT_OIDC_CLIENT_ID is empty")
		}
		a.oidc = newOIDCClient(cfg)
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
	if err := data.Migrate(ctx, a.DB); err != nil {
		return err
	}
	if _, err := data.EnsureDefaultDomain(ctx, a.DB, a.Cfg.DefaultDomain); err != nil {
		return err
	}

	userCount, err := data.CountUsers(ctx, a.DB)
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
		hash, err := HashPassword(password)
		if err != nil {
			return fmt.Errorf("invalid initial admin password: %w", err)
		}
		created, err := data.InsertUser(ctx, a.DB, username, hash, core.UserAdmin)
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

// rateLimiter is a fixed-window limiter for API writes and login attempts, partitioned
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
		isLimited := (strings.HasPrefix(r.URL.Path, "/rest") || r.URL.Path == "/graphql" || r.URL.Path == "/admin/login") &&
			r.Method != http.MethodGet && r.Method != http.MethodHead
		if isLimited && a.Cfg.RateLimitPerMinute > 0 {
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

func (a *App) buildRouter() *chi.Mux {
	mux := chi.NewRouter()
	mux.Use(middleware.GetHead)

	// Static assets served from the embedded filesystem.
	serveAsset := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if name == "inter-var.woff2" {
				// Reuse the font across full-page navigations. Its URL is not
				// fingerprinted, so keep the cache lifetime bounded.
				w.Header().Set("Cache-Control", "public, max-age=86400")
			}
			http.ServeFileFS(w, r, staticFiles, "static/"+name)
		}
	}
	for _, asset := range []string{"app.css", "htmx.min.js", "inter-var.woff2", "scalar-1.68.0.js", "api-docs.js"} {
		mux.Get("/"+asset, serveAsset(asset))
	}

	a.registerREST(mux)
	a.registerGraphQL(mux)

	// Dashboard
	mux.Method("GET", "/admin", a.requireUser(a.uiOverview))
	mux.Method("GET", "/admin/login", a.handle(a.uiLoginForm))
	mux.Method("POST", "/admin/login", a.handle(a.uiLogin))
	mux.Method("POST", "/admin/logout", a.handle(a.uiLogout))
	mux.Method("GET", "/admin/oidc/login", a.handle(a.uiOIDCLogin))
	mux.Method("GET", "/admin/oidc/callback", a.handle(a.uiOIDCCallback))

	mux.Method("GET", "/admin/short-urls", a.requireUser(a.uiListShortURLs))
	mux.Method("GET", "/admin/short-urls/new", a.requireUser(a.uiCreateShortURLForm))
	mux.Method("POST", "/admin/short-urls/new", a.requireUser(a.uiCreateShortURL))
	mux.Method("GET", "/admin/short-urls/{id}/edit", a.requireUser(a.uiEditShortURLForm))
	mux.Method("POST", "/admin/short-urls/{id}/edit", a.requireUser(a.uiEditShortURL))
	mux.Method("POST", "/admin/short-urls/{id}/rules/add", a.requireUser(a.uiAddRule))
	mux.Method("POST", "/admin/short-urls/{id}/rules/delete", a.requireUser(a.uiDeleteRule))
	mux.Method("POST", "/admin/short-urls/{id}/delete", a.requireUser(a.uiDeleteShortURL))
	mux.Method("POST", "/admin/short-urls/{id}/visits/delete", a.requireUser(a.uiDeleteShortURLVisits))
	mux.Method("GET", "/admin/short-urls/{id}/visits", a.requireUser(a.uiShortURLVisits))

	mux.Method("GET", "/admin/visits/orphan", a.requireAdmin(a.uiOrphanVisits))
	mux.Method("POST", "/admin/visits/orphan/delete", a.requireAdmin(a.uiDeleteOrphanVisits))

	mux.Method("GET", "/admin/tags", a.requireAdmin(a.uiListTags))
	mux.Method("POST", "/admin/tags/rename", a.requireAdmin(a.uiRenameTag))
	mux.Method("POST", "/admin/tags/delete", a.requireAdmin(a.uiDeleteTag))

	mux.Method("GET", "/admin/domains", a.requireAdmin(a.uiListDomains))
	mux.Method("POST", "/admin/domains", a.requireAdmin(a.uiCreateDomain))
	mux.Method("POST", "/admin/domains/{id}/redirects", a.requireAdmin(a.uiSetDomainRedirects))
	mux.Method("POST", "/admin/domains/{id}/delete", a.requireAdmin(a.uiDeleteDomain))

	mux.Method("GET", "/admin/api-keys", a.requireAdmin(a.uiListAPIKeys))
	mux.Method("POST", "/admin/api-keys", a.requireAdmin(a.uiCreateAPIKey))
	mux.Method("POST", "/admin/api-keys/{id}/toggle", a.requireAdmin(a.uiToggleAPIKey))
	mux.Method("POST", "/admin/api-keys/{id}/delete", a.requireAdmin(a.uiDeleteAPIKey))

	mux.Method("GET", "/admin/users", a.requireAdmin(a.uiListUsers))
	mux.Method("POST", "/admin/users", a.requireAdmin(a.uiCreateUser))
	mux.Method("POST", "/admin/users/{id}/role", a.requireAdmin(a.uiSetUserRole))
	mux.Method("POST", "/admin/users/{id}/password", a.requireAdmin(a.uiSetUserPassword))
	mux.Method("POST", "/admin/users/{id}/delete", a.requireAdmin(a.uiDeleteUser))

	if a.Cfg.WebhooksEnabled {
		mux.Method("GET", "/admin/webhooks", a.requireAdmin(a.uiListWebhooks))
		mux.Method("POST", "/admin/webhooks", a.requireAdmin(a.uiCreateWebhook))
		mux.Method("POST", "/admin/webhooks/{id}/toggle", a.requireAdmin(a.uiToggleWebhook))
		mux.Method("POST", "/admin/webhooks/{id}/delete", a.requireAdmin(a.uiDeleteWebhook))
	}

	// Public
	mux.Method("GET", "/robots.txt", a.handle(a.handleRobots))
	mux.Method("GET", "/{code}/qr-code", a.handle(a.handleQRCode))
	mux.Method("GET", "/", a.handle(a.handleBaseURL))
	// A "GET" pattern also serves HEAD requests.
	mux.Get("/*", a.handle(a.handleShortURL))

	mux.Handle("/rest/*", http.NotFoundHandler())
	if !a.Cfg.WebhooksEnabled {
		mux.Handle("/admin/webhooks", http.NotFoundHandler())
		mux.Handle("/admin/webhooks/*", http.NotFoundHandler())
	}
	return mux
}

// Handler is the full middleware + routing pipeline.
func (a *App) Handler() http.Handler {
	return a.browserSecurity(a.rateLimitMiddleware(a.mux))
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
