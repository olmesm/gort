package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

// PaginationDto is the paged REST response envelope.
type PaginationDto struct {
	CurrentPage        int   `json:"currentPage"`
	PagesCount         int   `json:"pagesCount"`
	ItemsPerPage       int   `json:"itemsPerPage"`
	ItemsInCurrentPage int   `json:"itemsInCurrentPage"`
	TotalItems         int64 `json:"totalItems"`
}

type PageDto[T any] struct {
	Data       []T           `json:"data"`
	Pagination PaginationDto `json:"pagination"`
}

func NewPageDto[A, B any](page core.Page[A], mapItem func(A) B) PageDto[B] {
	items := make([]B, len(page.Items))
	for i, item := range page.Items {
		items[i] = mapItem(item)
	}
	return PageDto[B]{
		Data: items,
		Pagination: PaginationDto{
			CurrentPage:        page.CurrentPage,
			PagesCount:         page.TotalPages(),
			ItemsPerPage:       page.ItemsPerPage,
			ItemsInCurrentPage: len(items),
			TotalItems:         page.TotalItems,
		},
	}
}

type VisitDto struct {
	Date         time.Time `json:"date"`
	Referer      *string   `json:"referer,omitempty"`
	UserAgent    *string   `json:"userAgent,omitempty"`
	Browser      *string   `json:"browser,omitempty"`
	Os           *string   `json:"os,omitempty"`
	Device       *string   `json:"device,omitempty"`
	PotentialBot bool      `json:"potentialBot"`
	VisitedUrl   *string   `json:"visitedUrl,omitempty"`
	CountryCode  *string   `json:"countryCode,omitempty"`
	Country      *string   `json:"country,omitempty"`
	City         *string   `json:"city,omitempty"`
	Latitude     *float64  `json:"latitude,omitempty"`
	Longitude    *float64  `json:"longitude,omitempty"`
}

func NewVisitDto(v data.VisitRow) VisitDto {
	return VisitDto{
		Date:         v.VisitedAt,
		Referer:      v.Referer,
		UserAgent:    v.UserAgent,
		Browser:      v.Browser,
		Os:           v.Os,
		Device:       v.Device,
		PotentialBot: v.IsBot,
		VisitedUrl:   v.VisitedUrl,
		CountryCode:  v.CountryCode,
		Country:      v.CountryName,
		City:         v.City,
		Latitude:     v.Latitude,
		Longitude:    v.Longitude,
	}
}

// TryParseDate parses an ISO-8601 date(-time), always yielding UTC.
func TryParseDate(s string) *time.Time {
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			utc := t.UTC()
			return &utc
		}
	}
	return nil
}

func queryInt(q url.Values, name string) *int {
	if v := q.Get(name); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return &i
		}
	}
	return nil
}

func queryIntDefault(q url.Values, name string, defaultVal int) int {
	if v := queryInt(q, name); v != nil {
		return *v
	}
	return defaultVal
}

func queryDate(q url.Values, name string) *time.Time {
	if v := q.Get(name); v != "" {
		return TryParseDate(v)
	}
	return nil
}

func queryBool(q url.Values, name string) bool {
	switch strings.ToLower(q.Get(name)) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

// queryStringList mirrors ASP.NET binding: repeated params, `name[]` params
// and nothing else.
func queryStringList(q url.Values, name string) []string {
	values := append([]string{}, q[name]...)
	values = append(values, q[name+"[]"]...)
	return values
}

func visitFiltersFromQuery(q url.Values) data.VisitFilters {
	return data.VisitFilters{
		StartDate:    queryDate(q, "startDate"),
		EndDate:      queryDate(q, "endDate"),
		ExcludeBots:  queryBool(q, "excludeBots"),
		Page:         queryIntDefault(q, "page", 1),
		ItemsPerPage: queryIntDefault(q, "itemsPerPage", core.DefaultPageSize),
	}
}

// ---- API key scoping ----

// applyKeyScope restricts list filters to what an API key may see.
func applyKeyScope(key *AuthenticatedKey, filters data.ShortUrlFilters) data.ShortUrlFilters {
	switch key.Role.Kind {
	case core.RoleAuthor:
		id := key.Id()
		filters.AuthorApiKeyId = &id
	case core.RoleDomain:
		id := key.Role.DomainID
		filters.DomainId = &id
	}
	return filters
}

// canAccessShortUrl says whether this key may see/manipulate the given short
// URL.
func canAccessShortUrl(key *AuthenticatedKey, detail *data.ShortUrlDetail) bool {
	switch key.Role.Kind {
	case core.RoleAdmin:
		return true
	case core.RoleAuthor:
		return detail.AuthorApiKeyId != nil && *detail.AuthorApiKeyId == key.Row.Id
	case core.RoleDomain:
		return detail.DomainId == key.Role.DomainID.Value()
	default:
		return false
	}
}

// findAccessibleShortUrl resolves a short URL by code (+ optional ?domain=)
// and checks key access. On failure it writes the error response and returns
// nil.
func (a *App) findAccessibleShortUrl(w http.ResponseWriter, key *AuthenticatedKey, code, domainAuthority string) *data.ShortUrlDetail {
	domain, err := a.ResolveNamedDomain(domainAuthority)
	if err != nil {
		a.serverError(w, err)
		return nil
	}
	if domain == nil {
		NotFound(w, fmt.Sprintf("Domain '%s' is not registered.", domainAuthority))
		return nil
	}
	detail, err := data.TryGetDetail(a.Db, core.DomainID(domain.Id), code)
	if err != nil {
		a.serverError(w, err)
		return nil
	}
	if detail == nil || !canAccessShortUrl(key, detail) {
		// Do not leak existence to keys that cannot see the URL.
		NotFound(w, fmt.Sprintf("No short URL found for code '%s'.", code))
		return nil
	}
	return detail
}
