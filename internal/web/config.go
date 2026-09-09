package web

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

// AppConfig is all runtime configuration. Populated from GORT_* environment
// variables (see ConfigFromEnv), or constructed directly in tests.
type AppConfig struct {
	// DefaultDomain is the authority (host[:port]) used to build short URLs
	// when no domain is picked.
	DefaultDomain core.DomainAuthority
	// UseHttps is the scheme used when rendering short URLs.
	UseHTTPS         bool
	DBDialect        data.Dialect
	ConnectionString string
	// DataDir is the directory for runtime state: SQLite db, GeoIP db,
	// session signing keys.
	DataDir               string
	ShortCodeLength       int
	DefaultRedirectStatus core.RedirectStatus
	AutoResolveTitles     bool
	// DisableTracking is the master switch: when true no visits are recorded
	// at all.
	DisableTracking bool
	// DisableIpTracking tracks visits but never records any form of the
	// visitor's IP.
	DisableIPTracking bool
	// AnonymizeIps anonymizes recorded IPs (zero host bits) before storing.
	AnonymizeIPs bool
	// TrackSkipParam: requests carrying this query param are redirected but
	// not tracked.
	TrackSkipParam string
	// TrackOrphanVisits tracks visits to unknown short codes / base URL /
	// other 404s.
	TrackOrphanVisits bool
	// Global fallbacks; per-domain values in the DB take precedence.
	BaseURLRedirect         string
	Regular404Redirect      string
	InvalidShortURLRedirect string
	GeoLiteLicenseKey       string
	InitialAdminUsername    string
	InitialAdminPassword    string
	// RateLimitPerMinute is requests per minute allowed on mutating REST
	// endpoints, per client IP.
	RateLimitPerMinute int
	Port               int

	// ---- OIDC single sign-on (Keycloak or any compliant IdP) ----

	// OidcIssuer enables SSO when set (e.g.
	// https://keycloak.example.com/realms/myrealm).
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	// OidcRedirectURL overrides the callback URL; when empty it is derived
	// from the incoming request as {scheme}://{host}/admin/oidc/callback.
	OIDCRedirectURL string
	// OidcScopes are the scopes requested besides the mandatory "openid".
	OIDCScopes []string
	// OidcGroupsClaim is the token claim carrying the user's groups.
	OIDCGroupsClaim string
	// OidcAdminGroup grants the dashboard admin role to members of this group.
	OIDCAdminGroup string
	// OidcProviderName is the label on the SSO login button.
	OIDCProviderName string
	// OidcOnly hides local password login (the initial admin remains as a
	// break-glass account for direct API/database recovery).
	OIDCOnly bool
}

// OidcEnabled reports whether SSO is configured.
func (cfg *AppConfig) OIDCEnabled() bool { return cfg.OIDCIssuer != "" }

func (cfg *AppConfig) GeoDBPath() string {
	return filepath.Join(cfg.DataDir, "GeoLite2-City.mmdb")
}

func (cfg *AppConfig) ShortURLBase(authority string) string {
	scheme := "http"
	if cfg.UseHTTPS {
		scheme = "https"
	}
	return scheme + "://" + authority
}

type ConfigLookup func(name string) (string, bool)

func boolVar(get ConfigLookup, name string, defaultVal bool) bool {
	v, ok := get(name)
	if !ok {
		return defaultVal
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultVal
	}
}

func intVar(get ConfigLookup, name string, defaultVal int) int {
	if v, ok := get(name); ok {
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return i
		}
	}
	return defaultVal
}

func strVar(get ConfigLookup, name string) string {
	if v, ok := get(name); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// ConfigFromLookup builds configuration from a variable lookup (normally
// environment variables prefixed with GORT_).
func ConfigFromLookup(get ConfigLookup) (*AppConfig, error) {
	dataDir := strVar(get, "DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}

	dialect := data.Sqlite
	switch strings.ToLower(strVar(get, "DB_DRIVER")) {
	case "postgres", "postgresql", "pgsql":
		dialect = data.Postgres
	}

	connString := strVar(get, "DB_CONNECTION")
	if connString == "" {
		if dialect == data.Sqlite {
			connString = filepath.Join(dataDir, "gort.db")
		} else {
			connString = "host=localhost dbname=gort user=gort password=gort"
		}
	}

	port := intVar(get, "PORT", 8080)

	// Configuration errors should stop startup with a clear message.
	rawDomain := strVar(get, "DEFAULT_DOMAIN")
	if rawDomain == "" {
		rawDomain = fmt.Sprintf("localhost:%d", port)
	}
	defaultDomain, err := core.NewDomainAuthority(rawDomain)
	if err != nil {
		return nil, fmt.Errorf("invalid GORT_DEFAULT_DOMAIN '%s': %s", rawDomain, err)
	}

	status, ok := core.RedirectStatusOfCode(intVar(get, "REDIRECT_STATUS", 302))
	if !ok {
		status = core.Found
	}

	return &AppConfig{
		DefaultDomain:           defaultDomain,
		UseHTTPS:                boolVar(get, "USE_HTTPS", false),
		DBDialect:               dialect,
		ConnectionString:        connString,
		DataDir:                 dataDir,
		ShortCodeLength:         intVar(get, "SHORT_CODE_LENGTH", core.DefaultCodeLength),
		DefaultRedirectStatus:   status,
		AutoResolveTitles:       boolVar(get, "AUTO_RESOLVE_TITLES", true),
		DisableTracking:         boolVar(get, "DISABLE_TRACKING", false),
		DisableIPTracking:       boolVar(get, "DISABLE_IP_TRACKING", false),
		AnonymizeIPs:            boolVar(get, "ANONYMIZE_IPS", true),
		TrackSkipParam:          strVar(get, "TRACK_SKIP_PARAM"),
		TrackOrphanVisits:       boolVar(get, "TRACK_ORPHAN_VISITS", true),
		BaseURLRedirect:         strVar(get, "BASE_URL_REDIRECT"),
		Regular404Redirect:      strVar(get, "REGULAR_404_REDIRECT"),
		InvalidShortURLRedirect: strVar(get, "INVALID_SHORT_URL_REDIRECT"),
		GeoLiteLicenseKey:       strVar(get, "GEOLITE_LICENSE_KEY"),
		InitialAdminUsername:    strVar(get, "INITIAL_ADMIN_USERNAME"),
		InitialAdminPassword:    strVar(get, "INITIAL_ADMIN_PASSWORD"),
		RateLimitPerMinute:      intVar(get, "RATE_LIMIT_PER_MINUTE", 120),
		Port:                    port,
		OIDCIssuer:              strVar(get, "OIDC_ISSUER"),
		OIDCClientID:            strVar(get, "OIDC_CLIENT_ID"),
		OIDCClientSecret:        strVar(get, "OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:         strVar(get, "OIDC_REDIRECT_URL"),
		OIDCScopes:              splitList(strVar(get, "OIDC_SCOPES"), "profile", "email"),
		OIDCGroupsClaim:         strVarDefault(get, "OIDC_GROUPS_CLAIM", "groups"),
		OIDCAdminGroup:          strVarDefault(get, "OIDC_ADMIN_GROUP", "gort-admins"),
		OIDCProviderName:        strVarDefault(get, "OIDC_PROVIDER_NAME", "SSO"),
		OIDCOnly:                boolVar(get, "OIDC_ONLY", false),
	}, nil
}

func strVarDefault(get ConfigLookup, name, defaultVal string) string {
	if v := strVar(get, name); v != "" {
		return v
	}
	return defaultVal
}

// splitList parses a space- or comma-separated list, falling back to the
// defaults when unset.
func splitList(raw string, defaults ...string) []string {
	if strings.TrimSpace(raw) == "" {
		return defaults
	}
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ' ' || r == ',' }) {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func ConfigFromEnv() (*AppConfig, error) {
	return ConfigFromLookup(func(name string) (string, bool) {
		v := os.Getenv("GORT_" + name)
		return v, v != ""
	})
}
