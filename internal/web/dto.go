package web

import (
	"time"

	"github.com/olmesm/gort/internal/data"
)

// JSON representations shared by the REST API, webhook payloads and the
// dashboard.

type VisitsSummaryDTO struct {
	Total   int64 `json:"total"`
	NonBots int64 `json:"nonBots"`
	Bots    int64 `json:"bots"`
}

type ShortURLMetaDTO struct {
	ValidSince *time.Time `json:"validSince,omitempty"`
	ValidUntil *time.Time `json:"validUntil,omitempty"`
	MaxVisits  *int64     `json:"maxVisits,omitempty"`
}

type ShortURLDTO struct {
	ShortCode      string           `json:"shortCode"`
	ShortURL       string           `json:"shortUrl"`
	Domain         string           `json:"domain"`
	LongURL        string           `json:"longUrl"`
	Title          *string          `json:"title,omitempty"`
	DateCreated    time.Time        `json:"dateCreated"`
	Tags           []string         `json:"tags"`
	Group          *string          `json:"group,omitempty"`
	Meta           ShortURLMetaDTO  `json:"meta"`
	VisitsSummary  VisitsSummaryDTO `json:"visitsSummary"`
	ForwardQuery   bool             `json:"forwardQuery"`
	Crawlable      bool             `json:"crawlable"`
	RedirectStatus int              `json:"redirectStatus"`
}

func ShortURLFor(cfg *AppConfig, authority, shortCode string) string {
	return cfg.ShortURLBase(authority) + "/" + shortCode
}

func NewShortURLDTO(cfg *AppConfig, tags []string, d *data.ShortURLDetail) ShortURLDTO {
	if tags == nil {
		tags = []string{}
	}
	return ShortURLDTO{
		ShortCode:   d.ShortCode,
		ShortURL:    ShortURLFor(cfg, d.Authority, d.ShortCode),
		Domain:      d.Authority,
		LongURL:     d.LongURL,
		Title:       d.Title,
		DateCreated: d.CreatedAt,
		Tags:        tags,
		Group:       d.GroupName,
		Meta: ShortURLMetaDTO{
			ValidSince: d.ValidSince,
			ValidUntil: d.ValidUntil,
			MaxVisits:  d.MaxVisits,
		},
		VisitsSummary: VisitsSummaryDTO{
			Total:   d.VisitCount,
			NonBots: d.VisitCount - d.BotVisitCount,
			Bots:    d.BotVisitCount,
		},
		ForwardQuery:   d.ForwardQuery,
		Crawlable:      d.Crawlable,
		RedirectStatus: d.RedirectStatus,
	}
}
