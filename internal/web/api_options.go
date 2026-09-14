package web

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

// Operations receive parsed values. REST retains its permissive v1 parsing;
// GraphQL supplies its native numbers and booleans directly.
type shortURLListOptions struct {
	data.ShortURLFilters
	Domain string
}
type shortURLVisitOptions struct {
	ShortURLInput
	data.VisitFilters
}
type domainVisitOptions struct {
	AuthorityInput
	data.VisitFilters
}
type tagVisitOptions struct {
	Tag string
	data.VisitFilters
}
type orphanVisitOptions struct {
	Type string
	data.VisitFilters
}
type tagListOptions struct {
	data.ListFilters
	WithStats bool
}
type statsOptions struct {
	ShortCode, Domain, Tag string
	Orphan                 bool
	StartDate, EndDate     *time.Time
}
type breakdownOptions struct {
	statsOptions
	By    string
	Limit int
}

// A nil ID represents a malformed REST path and retains the v1 404 response.
type idOptions struct{ ID *int64 }
type idBodyOptions[T any] struct {
	idOptions
	Body T
}

func restInput[I, P, O any](parse func(*I) *P, operation func(context.Context, *AuthenticatedKey, *P) (*O, error)) func(context.Context, *AuthenticatedKey, *I) (*O, error) {
	return func(ctx context.Context, key *AuthenticatedKey, in *I) (*O, error) {
		return operation(ctx, key, parse(in))
	}
}

func parseIntDefault(raw string, fallback int) int {
	if n, err := strconv.Atoi(raw); err == nil {
		return n
	}
	return fallback
}
func parseBool(raw string) bool {
	switch strings.ToLower(raw) {
	case "true", "1", "yes":
		return true
	}
	return false
}
func (in *VisitQuery) options() *data.VisitFilters {
	return &data.VisitFilters{
		StartDate: TryParseDate(in.StartDate), EndDate: TryParseDate(in.EndDate), ExcludeBots: parseBool(in.ExcludeBots),
		Page: parseIntDefault(in.Page, 1), ItemsPerPage: parseIntDefault(in.ItemsPerPage, core.DefaultPageSize),
	}
}
func (in *ShortURLVisitsInput) options() *shortURLVisitOptions {
	return &shortURLVisitOptions{in.ShortURLInput, *in.VisitQuery.options()}
}
func (in *DomainVisitsInput) options() *domainVisitOptions {
	return &domainVisitOptions{in.AuthorityInput, *in.VisitQuery.options()}
}
func (in *TagVisitsInput) options() *tagVisitOptions {
	return &tagVisitOptions{in.Tag, *in.VisitQuery.options()}
}
func (in *OrphanVisitsInput) options() *orphanVisitOptions {
	return &orphanVisitOptions{in.Type, *in.VisitQuery.options()}
}
func (in *ShortURLListInput) options() *shortURLListOptions {
	filters := data.ShortURLFilters{
		SearchTerm: in.SearchTerm, Tags: append(in.Tags, in.TagsArray...), TagsMatchAll: strings.EqualFold(in.TagsMode, "all"),
		StartDate: TryParseDate(in.StartDate), EndDate: TryParseDate(in.EndDate),
		ExcludeMaxVisitsReached: parseBool(in.ExcludeMaxVisitsReached), ExcludePastValidUntil: parseBool(in.ExcludePastValidUntil),
		Page: parseIntDefault(in.Page, 1), ItemsPerPage: parseIntDefault(in.ItemsPerPage, core.DefaultPageSize),
	}
	if in.Group.IsSet {
		filters.Group = &in.Group.Value
	}
	filters.OrderBy, filters.Descending = shortURLOrder(in.OrderBy)
	return &shortURLListOptions{filters, in.Domain}
}
func shortURLOrder(raw string) (data.ShortURLOrder, bool) {
	if raw == "" {
		return data.OrderDateCreated, true
	}
	field, dir := raw, "ASC"
	if parts := strings.Split(raw, "-"); len(parts) == 2 {
		field, dir = parts[0], strings.ToUpper(parts[1])
	}
	order := data.OrderDateCreated
	switch field {
	case "shortCode":
		order = data.OrderShortCode
	case "longUrl":
		order = data.OrderLongURL
	case "title":
		order = data.OrderTitle
	case "visits":
		order = data.OrderVisits
	}
	return order, dir == "DESC"
}
func (in *TagListInput) options() *tagListOptions {
	return &tagListOptions{
		ListFilters: data.ListFilters{Search: in.SearchTerm, Page: parseIntDefault(in.Page, 1), ItemsPerPage: parseIntDefault(in.ItemsPerPage, core.MaxPageSize)},
		WithStats:   parseBool(in.WithStats),
	}
}
func (in *DeleteTagsInput) options() *[]string {
	tags := append(in.Tags, in.TagsArray...)
	return &tags
}
func (in *StatsInput) options() *statsOptions {
	return &statsOptions{
		ShortCode: in.ShortCode, Domain: in.Domain, Tag: in.Tag, Orphan: parseBool(in.Orphan),
		StartDate: TryParseDate(in.StartDate), EndDate: TryParseDate(in.EndDate),
	}
}
func (in *BreakdownInput) options() *breakdownOptions {
	return &breakdownOptions{*in.StatsInput.options(), in.By, parseIntDefault(in.Limit, 25)}
}
func (in *IDInput) options() *idOptions {
	if id, err := strconv.ParseInt(in.ID, 10, 64); err == nil {
		return &idOptions{ID: &id}
	}
	return &idOptions{}
}
func (in *IDBodyInput[T]) options() *idBodyOptions[T] {
	return &idBodyOptions[T]{*in.IDInput.options(), in.Body}
}
