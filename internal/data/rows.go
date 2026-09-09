package data

import (
	"time"

	"github.com/olmesm/gort/internal/core"
)

type UserRow struct {
	Id           core.UserID
	Username     string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
	// AuthSource is "local" for password accounts, "oidc" for SSO-provisioned
	// ones.
	AuthSource  string
	OidcSubject *string
}

type DomainRow struct {
	Id                      core.DomainID
	Authority               string
	BaseUrlRedirect         *string
	Regular404Redirect      *string
	InvalidShortUrlRedirect *string
	IsDefault               bool
	CreatedAt               time.Time
}

type ShortUrlRow struct {
	Id                   core.ShortUrlID
	ShortCode            string
	DomainId             core.DomainID
	LongUrl              string
	Title                *string
	TitleWasAutoResolved bool
	RedirectStatus       int
	ForwardQuery         bool
	Crawlable            bool
	MaxVisits            *int64
	ValidSince           *time.Time
	ValidUntil           *time.Time
	AuthorUserId         *core.UserID
	AuthorApiKeyId       *core.ApiKeyID
	GroupName            *string
	CreatedAt            time.Time
}

// ShortUrlDetail is a short URL row enriched with joined data for lists and
// API payloads.
type ShortUrlDetail struct {
	Id                   core.ShortUrlID
	ShortCode            string
	DomainId             core.DomainID
	Authority            string
	LongUrl              string
	Title                *string
	TitleWasAutoResolved bool
	RedirectStatus       int
	ForwardQuery         bool
	Crawlable            bool
	MaxVisits            *int64
	ValidSince           *time.Time
	ValidUntil           *time.Time
	AuthorUserId         *core.UserID
	AuthorApiKeyId       *core.ApiKeyID
	GroupName            *string
	CreatedAt            time.Time
	VisitCount           int64
	BotVisitCount        int64
}

type TagStatsRow struct {
	Id            int64
	Name          string
	ShortUrlCount int64
	VisitCount    int64
}

type VisitRow struct {
	Id          core.VisitID
	ShortUrlId  *core.ShortUrlID
	VisitType   string
	VisitedAt   time.Time
	Referer     *string
	UserAgent   *string
	Browser     *string
	Os          *string
	Device      *string
	IsBot       bool
	RemoteIp    *string
	CountryCode *string
	CountryName *string
	City        *string
	Latitude    *float64
	Longitude   *float64
	VisitedUrl  *string
	GeoResolved bool
}

type ApiKeyRow struct {
	Id        core.ApiKeyID
	KeyHash   string
	Name      *string
	Role      string
	DomainId  *core.DomainID
	Enabled   bool
	ExpiresAt *time.Time
	CreatedAt time.Time
}

type WebhookRow struct {
	Id        core.WebhookID
	Name      string
	Url       string
	Secret    string
	Events    string
	Enabled   bool
	CreatedAt time.Time
}

type WebhookDeliveryRow struct {
	Id            int64
	WebhookId     core.WebhookID
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
	ShortUrlCount int64
	VisitCount    int64
}
