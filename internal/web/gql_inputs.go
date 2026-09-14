package web

import (
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

type GraphResolver struct{ App *App }

func value[T any](p *T) T {
	if p != nil {
		return *p
	}
	var zero T
	return zero
}
func ptr[T any](v T) *T { return &v }
func valueOr[T any](p *T, fallback T) T {
	if p != nil {
		return *p
	}
	return fallback
}
func linkInput(code string, domain *string) ShortURLInput {
	return ShortURLInput{Code: code, Domain: value(domain)}
}
func visitFilters(filter *VisitFilter) data.VisitFilters {
	if filter == nil {
		filter = &VisitFilter{}
	}
	return data.VisitFilters{
		Page: valueOr(filter.Page, 1), ItemsPerPage: valueOr(filter.ItemsPerPage, core.DefaultPageSize),
		StartDate: TryParseDate(value(filter.StartDate)), EndDate: TryParseDate(value(filter.EndDate)),
		ExcludeBots: value(filter.ExcludeBots),
	}
}
func statsInput(scope *StatsScope) statsOptions {
	if scope == nil {
		scope = &StatsScope{}
	}
	return statsOptions{
		StartDate: TryParseDate(value(scope.StartDate)), EndDate: TryParseDate(value(scope.EndDate)),
		ShortCode: value(scope.ShortCode), Domain: value(scope.Domain), Tag: value(scope.Tag), Orphan: value(scope.Orphan),
	}
}
func shortURLFilterInput(f *ShortURLFilter) *shortURLListOptions {
	if f == nil {
		f = &ShortURLFilter{}
	}
	filters := data.ShortURLFilters{
		Page: valueOr(f.Page, 1), ItemsPerPage: valueOr(f.ItemsPerPage, core.DefaultPageSize),
		StartDate: TryParseDate(value(f.StartDate)), EndDate: TryParseDate(value(f.EndDate)),
		SearchTerm: value(f.SearchTerm), Tags: f.Tags, TagsMatchAll: strings.EqualFold(value(f.TagsMode), "all"), Group: f.Group,
		ExcludeMaxVisitsReached: value(f.ExcludeMaxVisitsReached), ExcludePastValidUntil: value(f.ExcludePastValidUntil),
	}
	filters.OrderBy, filters.Descending = shortURLOrder(value(f.OrderBy))
	return &shortURLListOptions{filters, value(f.Domain)}
}
func patchField[T any](v graphql.Omittable[*T]) Field[T] {
	return Field[T]{Present: v.IsSet(), Null: v.Value() == nil, Value: value(v.Value())}
}
