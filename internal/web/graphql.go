package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/99designs/gqlgen/graphql"
	gqlhandler "github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/go-chi/chi/v5"
	"github.com/olmesm/gort/internal/core"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

//go:generate sh -c "cd ../.. && go tool gqlgen generate"

func (a *App) registerGraphQL(r chi.Router) {
	srv := gqlhandler.New(NewExecutableSchema(Config{Resolvers: &GraphResolver{App: a}, Complexity: graphComplexity()}))
	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})
	srv.SetParserTokenLimit(10000)
	srv.Use(extension.Introspection{})
	srv.Use(extension.FixedComplexityLimit(10000))
	srv.SetErrorPresenter(func(ctx context.Context, err error) *gqlerror.Error {
		var problem *Problem
		if !errors.As(err, &problem) {
			// Syntax and validation errors originate in gqlgen.
			var queryError *gqlerror.Error
			if errors.As(err, &queryError) && (queryError.Err == nil || graphql.GetFieldContext(ctx) == nil) {
				return graphql.DefaultErrorPresenter(ctx, err)
			}
			problem = a.apiError(err).(*Problem)
		}
		return &gqlerror.Error{Message: problem.Detail, Path: graphql.GetPath(ctx), Extensions: map[string]any{"code": problem.Type, "status": problem.Status}}
	})
	srv.SetRecoverFunc(func(ctx context.Context, recovered any) error {
		a.Logger.Error("GraphQL resolver panicked", "error", recovered)
		return internalProblem
	})
	r.Handle("/graphql", a.requireAPIKey(func(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Cache-Control", "no-store")
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		srv.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), apiKeyContext{}, key)))
		return nil
	}))
	r.Get("/graphql/schema.graphql", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(sources[0].Input))
	})
	r.Get("/graphql/docs", a.handle(func(w http.ResponseWriter, r *http.Request) error {
		return a.renderShared(w, http.StatusOK, "graphql-docs", nil)
	}))
	r.Handle("/graphql/*", http.NotFoundHandler())
}

// Charge for each requested row, including nested visit pages. Clamp page
// sizes exactly as the shared operations do so invalid values cannot bypass it.
func graphPageCost(child int, size *int) int {
	n := value(size)
	if n < 1 {
		n = core.DefaultPageSize
	}
	if n > core.MaxPageSize {
		n = core.MaxPageSize
	}
	return 1 + n*child
}
func graphComplexity() ComplexityRoot {
	var c ComplexityRoot
	visits := func(child int, f *VisitFilter) int {
		if f == nil {
			return graphPageCost(child, nil)
		}
		return graphPageCost(child, f.ItemsPerPage)
	}
	c.Query.ShortURLs = func(child int, f *ShortURLFilter) int {
		if f == nil {
			return graphPageCost(child, nil)
		}
		return graphPageCost(child, f.ItemsPerPage)
	}
	c.Query.Tags = func(child int, _ *string, _ *int, size *int) int {
		if size == nil {
			size = ptr(core.MaxPageSize)
		}
		return graphPageCost(child, size)
	}
	c.Query.Visits = visits
	c.ShortURL.Visits = visits
	c.Query.OrphanVisits = func(child int, _ *string, f *VisitFilter) int { return visits(child, f) }
	c.Query.TagVisits = func(child int, _ string, f *VisitFilter) int { return visits(child, f) }
	c.Query.DomainVisits = c.Query.TagVisits
	// These unpaginated v1 collections can be large; charge conservatively.
	collection := func(child int) int { return 1 + core.MaxPageSize*child }
	c.Query.APIKeys = collection
	c.Query.Webhooks = collection
	c.Query.Domains = collection
	c.Query.VisitsPerDay = func(child int, _ *StatsScope) int { return collection(child) }
	c.Query.Breakdown = func(child int, _ string, _ *StatsScope, limit *int) int {
		n := 25
		if limit != nil {
			n = max(1, min(100, *limit))
		}
		return 1 + n*child
	}
	return c
}
