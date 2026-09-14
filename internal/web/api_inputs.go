package web

import (
	"reflect"

	"github.com/danielgtaylor/huma/v2"
)

type Empty struct{}
type BodyInput[T any] struct{ Body T }
type ShortURLInput struct {
	Code   string `path:"code" doc:"Short code"`
	Domain string `query:"domain" doc:"Domain hosting this code. Defaults to the configured domain."`
}
type ShortURLBodyInput[T any] struct {
	ShortURLInput
	Body T
}
type IDInput struct {
	ID string `path:"id" doc:"Numeric identifier"`
}
type IDBodyInput[T any] struct {
	IDInput
	Body T
}
type AuthorityInput struct {
	Authority string `path:"authority" doc:"Domain name, including port when needed"`
}

// String parameters preserve v1's permissive date, boolean and pagination
// parsing at the REST boundary.
type PageQuery struct {
	Page         string `query:"page" doc:"Page number, starting at 1. Invalid values use the default."`
	ItemsPerPage string `query:"itemsPerPage" doc:"Page size. Values are clamped to the supported range."`
}
type DateQuery struct {
	StartDate string `query:"startDate" doc:"Start date in UTC, ISO 8601"`
	EndDate   string `query:"endDate" doc:"End date in UTC, ISO 8601"`
}
type VisitQuery struct {
	PageQuery
	DateQuery
	ExcludeBots string `query:"excludeBots" doc:"Exclude bots when true, 1 or yes"`
}
type ShortURLVisitsInput struct {
	ShortURLInput
	VisitQuery
}
type DomainVisitsInput struct {
	AuthorityInput
	VisitQuery
}
type TagVisitsInput struct {
	Tag string `path:"tag"`
	VisitQuery
}
type OrphanVisitsInput struct {
	VisitQuery
	Type string `query:"type" doc:"base_url, invalid_short_url or regular_404"`
}
type ShortURLListInput struct {
	PageQuery
	DateQuery
	SearchTerm              string        `query:"searchTerm"`
	Tags                    []string      `query:"tags,explode" doc:"Repeat this parameter to match multiple tags"`
	TagsArray               []string      `query:"tags[],explode" doc:"Alternative spelling for repeated tags"`
	TagsMode                string        `query:"tagsMode" doc:"all requires every tag; other values match any tag"`
	Group                   OptionalQuery `query:"group" doc:"Omit for all groups; an empty value selects ungrouped links"`
	Domain                  string        `query:"domain"`
	OrderBy                 string        `query:"orderBy" doc:"dateCreated, shortCode, longUrl, title or visits, optionally followed by -ASC or -DESC"`
	ExcludeMaxVisitsReached string        `query:"excludeMaxVisitsReached"`
	ExcludePastValidUntil   string        `query:"excludePastValidUntil"`
}
type TagListInput struct {
	PageQuery
	SearchTerm string `query:"searchTerm"`
	WithStats  string `query:"withStats" doc:"When true, returns objects with tag counts instead of names"`
}
type DeleteTagsInput struct {
	Tags      []string `query:"tags,explode"`
	TagsArray []string `query:"tags[],explode"`
}
type StatsInput struct {
	DateQuery
	ShortCode string `query:"shortCode"`
	Domain    string `query:"domain"`
	Tag       string `query:"tag"`
	Orphan    string `query:"orphan" doc:"Admin only. Select orphan visits."`
}
type BreakdownInput struct {
	StatsInput
	By    string `query:"by" doc:"country, countryCode, city, browser, os, referer or device"`
	Limit string `query:"limit" doc:"Maximum results, from 1 to 100. Defaults to 25."`
}

func result[T any](value T) (*T, error) { return &value, nil }

type DataList[T any] struct {
	Data []T `json:"data"`
}
type DeletedVisits struct {
	DeletedVisits int `json:"deletedVisits"`
}
type DeletedTags struct {
	DeletedTags int `json:"deletedTags"`
}
type IDEnabled struct {
	ID      int64 `json:"id"`
	Enabled bool  `json:"enabled"`
}
type VisitOverviewDTO struct {
	VisitsCount       int64 `json:"visitsCount"`
	OrphanVisitsCount int64 `json:"orphanVisitsCount"`
	ShortURLsCount    int64 `json:"shortUrlsCount"`
	TagsCount         int64 `json:"tagsCount"`
	BotVisitsCount    int64 `json:"botVisitsCount"`
}

// A PATCH Field is represented on the wire by its value, not its Go wrapper.
func (f Field[T]) Schema(r huma.Registry) *huma.Schema {
	s := *r.Schema(reflect.TypeFor[T](), false, "")
	s.Nullable = true
	return &s
}

type OptionalQuery struct {
	Value string
	IsSet bool
}

func (o OptionalQuery) Schema(huma.Registry) *huma.Schema { return &huma.Schema{Type: "string"} }
func (o *OptionalQuery) Receiver() reflect.Value {
	return reflect.ValueOf(o).Elem().FieldByName("Value")
}
func (o *OptionalQuery) OnParamSet(isSet bool, _ any) { o.IsSet = isSet }

// Huma treats an empty query value as unset during binding. v1 uses group=
// to select ungrouped links, so preserve presence from the request URL.
func (in *ShortURLListInput) Resolve(ctx huma.Context) []error {
	u := ctx.URL()
	q := u.Query()
	in.Group = OptionalQuery{Value: q.Get("group"), IsSet: q.Has("group")}
	return nil
}
