package web

import (
	"time"

	"github.com/olmesm/gort/internal/data"
)

// JSON representations shared by the REST API, webhook payloads and the
// dashboard.

type VisitsSummaryDto struct {
	Total   int64 `json:"total"`
	NonBots int64 `json:"nonBots"`
	Bots    int64 `json:"bots"`
}

type ShortUrlMetaDto struct {
	ValidSince *time.Time `json:"validSince,omitempty"`
	ValidUntil *time.Time `json:"validUntil,omitempty"`
	MaxVisits  *int64     `json:"maxVisits,omitempty"`
}

type ShortUrlDto struct {
	ShortCode      string           `json:"shortCode"`
	ShortUrl       string           `json:"shortUrl"`
	Domain         string           `json:"domain"`
	LongUrl        string           `json:"longUrl"`
	Title          *string          `json:"title,omitempty"`
	DateCreated    time.Time        `json:"dateCreated"`
	Tags           []string         `json:"tags"`
	Group          *string          `json:"group,omitempty"`
	Meta           ShortUrlMetaDto  `json:"meta"`
	VisitsSummary  VisitsSummaryDto `json:"visitsSummary"`
	ForwardQuery   bool             `json:"forwardQuery"`
	Crawlable      bool             `json:"crawlable"`
	RedirectStatus int              `json:"redirectStatus"`
}

func ShortUrlFor(cfg *AppConfig, authority, shortCode string) string {
	return cfg.ShortUrlBase(authority) + "/" + shortCode
}

func NewShortUrlDto(cfg *AppConfig, tags []string, d *data.ShortUrlDetail) ShortUrlDto {
	if tags == nil {
		tags = []string{}
	}
	return ShortUrlDto{
		ShortCode:   d.ShortCode,
		ShortUrl:    ShortUrlFor(cfg, d.Authority, d.ShortCode),
		Domain:      d.Authority,
		LongUrl:     d.LongUrl,
		Title:       d.Title,
		DateCreated: d.CreatedAt,
		Tags:        tags,
		Group:       d.GroupName,
		Meta: ShortUrlMetaDto{
			ValidSince: d.ValidSince,
			ValidUntil: d.ValidUntil,
			MaxVisits:  d.MaxVisits,
		},
		VisitsSummary: VisitsSummaryDto{
			Total:   d.VisitCount,
			NonBots: d.VisitCount - d.BotVisitCount,
			Bots:    d.BotVisitCount,
		},
		ForwardQuery:   d.ForwardQuery,
		Crawlable:      d.Crawlable,
		RedirectStatus: d.RedirectStatus,
	}
}
