package data

import "time"

type UserRow struct {
	Id           int64
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
	Id                      int64
	Authority               string
	BaseUrlRedirect         *string
	Regular404Redirect      *string
	InvalidShortUrlRedirect *string
	IsDefault               bool
	CreatedAt               time.Time
}

type ShortUrlRow struct {
	Id                   int64
	ShortCode            string
	DomainId             int64
	LongUrl              string
	Title                *string
	TitleWasAutoResolved bool
	RedirectStatus       int
	ForwardQuery         bool
	Crawlable            bool
	MaxVisits            *int64
	ValidSince           *time.Time
	ValidUntil           *time.Time
	AuthorUserId         *int64
	AuthorApiKeyId       *int64
	GroupName            *string
	CreatedAt            time.Time
}

// ShortUrlDetail is a short URL row enriched with joined data for lists and
// API payloads.
type ShortUrlDetail struct {
	Id                   int64
	ShortCode            string
	DomainId             int64
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
	AuthorUserId         *int64
	AuthorApiKeyId       *int64
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
	Id          int64
	ShortUrlId  *int64
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
	Id        int64
	KeyHash   string
	Name      *string
	Role      string
	DomainId  *int64
	Enabled   bool
	ExpiresAt *time.Time
	CreatedAt time.Time
}

type WebhookRow struct {
	Id        int64
	Name      string
	Url       string
	Secret    string
	Events    string
	Enabled   bool
	CreatedAt time.Time
}

type WebhookDeliveryRow struct {
	Id            int64
	WebhookId     int64
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
