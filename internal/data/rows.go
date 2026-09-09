package data

import (
	"time"

	"github.com/olmesm/gort/internal/core"
)

type UserRow struct {
	ID           core.UserID
	Username     string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
	// AuthSource is "local" for password accounts, "oidc" for SSO-provisioned
	// ones.
	AuthSource  string
	OIDCSubject *string
}

type DomainRow struct {
	ID                      core.DomainID
	Authority               string
	BaseURLRedirect         *string
	Regular404Redirect      *string
	InvalidShortURLRedirect *string
	IsDefault               bool
	CreatedAt               time.Time
}

type ShortURLRow struct {
	ID                   core.ShortURLID
	ShortCode            string
	DomainID             core.DomainID
	LongURL              string
	Title                *string
	TitleWasAutoResolved bool
	RedirectStatus       int
	ForwardQuery         bool
	Crawlable            bool
	MaxVisits            *int64
	ValidSince           *time.Time
	ValidUntil           *time.Time
	AuthorUserID         *core.UserID
	AuthorAPIKeyID       *core.APIKeyID
	GroupName            *string
	CreatedAt            time.Time
}

// ShortUrlDetail is a short URL row enriched with joined data for lists and
// API payloads.
type ShortURLDetail struct {
	ID                   core.ShortURLID
	ShortCode            string
	DomainID             core.DomainID
	Authority            string
	LongURL              string
	Title                *string
	TitleWasAutoResolved bool
	RedirectStatus       int
	ForwardQuery         bool
	Crawlable            bool
	MaxVisits            *int64
	ValidSince           *time.Time
	ValidUntil           *time.Time
	AuthorUserID         *core.UserID
	AuthorAPIKeyID       *core.APIKeyID
	GroupName            *string
	CreatedAt            time.Time
	VisitCount           int64
	BotVisitCount        int64
}

type TagStatsRow struct {
	ID            int64
	Name          string
	ShortURLCount int64
	VisitCount    int64
}

type VisitRow struct {
	ID          core.VisitID
	ShortURLID  *core.ShortURLID
	VisitType   string
	VisitedAt   time.Time
	Referer     *string
	UserAgent   *string
	Browser     *string
	OS          *string
	Device      *string
	IsBot       bool
	RemoteIP    *string
	CountryCode *string
	CountryName *string
	City        *string
	Latitude    *float64
	Longitude   *float64
	VisitedURL  *string
	GeoResolved bool
}

type APIKeyRow struct {
	ID        core.APIKeyID
	KeyHash   string
	Name      *string
	Role      string
	DomainID  *core.DomainID
	Enabled   bool
	ExpiresAt *time.Time
	CreatedAt time.Time
}

type WebhookRow struct {
	ID        core.WebhookID
	Name      string
	URL       string
	Secret    string
	Events    string
	Enabled   bool
	CreatedAt time.Time
}

type WebhookDeliveryRow struct {
	ID            int64
	WebhookID     core.WebhookID
	Event         string
	Payload       string
	Attempts      int
	NextAttemptAt time.Time
	Status        string
	LastError     *string
	CreatedAt     time.Time
}

type DomainStatsRow struct {
	DomainRow
	ShortURLCount int64
	VisitCount    int64
}
