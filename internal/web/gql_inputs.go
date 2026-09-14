package web

import (
	"github.com/99designs/gqlgen/graphql"
	"strconv"
)

func value[T any](p *T) T {
	if p != nil {
		return *p
	}
	var zero T
	return zero
}
func ptr[T any](v T) *T { return &v }
func intQuery(p *int) string {
	if p == nil {
		return ""
	}
	return strconv.Itoa(*p)
}
func boolQuery(p *bool) string {
	if p == nil {
		return ""
	}
	return strconv.FormatBool(*p)
}
func pageQuery(page, size *int) PageQuery {
	return PageQuery{Page: intQuery(page), ItemsPerPage: intQuery(size)}
}
func linkInput(code string, domain *string) ShortURLInput {
	return ShortURLInput{Code: code, Domain: value(domain)}
}
func visitQuery(filter *VisitFilter) VisitQuery {
	if filter == nil {
		return VisitQuery{}
	}
	return VisitQuery{PageQuery: pageQuery(filter.Page, filter.ItemsPerPage), DateQuery: DateQuery{StartDate: value(filter.StartDate), EndDate: value(filter.EndDate)}, ExcludeBots: boolQuery(filter.ExcludeBots)}
}
func statsInput(scope *StatsScope) StatsInput {
	if scope == nil {
		return StatsInput{}
	}
	return StatsInput{DateQuery: DateQuery{StartDate: value(scope.StartDate), EndDate: value(scope.EndDate)}, ShortCode: value(scope.ShortCode), Domain: value(scope.Domain), Tag: value(scope.Tag), Orphan: boolQuery(scope.Orphan)}
}
func shortURLFilterInput(f *ShortURLFilter) *ShortURLListInput {
	if f == nil {
		return &ShortURLListInput{}
	}
	return &ShortURLListInput{PageQuery: pageQuery(f.Page, f.ItemsPerPage), DateQuery: DateQuery{StartDate: value(f.StartDate), EndDate: value(f.EndDate)}, SearchTerm: value(f.SearchTerm), Tags: f.Tags, TagsMode: value(f.TagsMode), Group: OptionalQuery{Value: value(f.Group), IsSet: f.Group != nil}, Domain: value(f.Domain), OrderBy: value(f.OrderBy), ExcludeMaxVisitsReached: boolQuery(f.ExcludeMaxVisitsReached), ExcludePastValidUntil: boolQuery(f.ExcludePastValidUntil)}
}
func patchField[T any](v graphql.Omittable[*T]) Field[T] {
	return Field[T]{Present: v.IsSet(), Null: v.Value() == nil, Value: value(v.Value())}
}
