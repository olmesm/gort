package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
)

type apiKeyContext struct{}

func keyFromContext(ctx context.Context) *AuthenticatedKey {
	return ctx.Value(apiKeyContext{}).(*AuthenticatedKey)
}

type restResponse[T any] struct{ Body T }

// Keep the established v1 problem document for binding and validation errors.
func init() {
	huma.NewErrorWithContext = func(_ huma.Context, status int, message string, errs ...error) huma.StatusError {
		if status == 422 || status == 413 {
			status = 400
		}
		if status >= 500 {
			return internalProblem
		}
		if len(errs) > 0 {
			message = fmt.Sprintf("%s: %v", message, errs[0])
		}
		return &Problem{Status: status, Type: "invalid-data", Title: "Invalid data", Detail: message}
	}
}

func registerOperation[I, O any](a *App, api huma.API, method, path, name, summary string, status int, fn func(context.Context, *AuthenticatedKey, *I) (*O, error)) {
	responses := map[string]*huma.Response{}
	for _, code := range []int{400, 401, 403, 404, 409, 429, 500} {
		responses[strconv.Itoa(code)] = &huma.Response{Description: http.StatusText(code), Content: map[string]*huma.MediaType{
			"application/problem+json": {Schema: api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[problemDetails](), true, "")},
		}}
	}
	tag := strings.Split(strings.TrimPrefix(path, "/rest/v1/"), "/")[0]
	tag = map[string]string{"short-urls": "Short URLs", "api-keys": "API keys", "domains": "Domains", "stats": "Statistics", "tags": "Tags", "visits": "Visits", "webhooks": "Webhooks"}[tag]
	op := huma.Operation{Tags: []string{tag}, OperationID: name, Method: method, Path: path, Summary: summary, DefaultStatus: status, MaxBodyBytes: 1 << 20, Responses: responses}
	if status == 204 {
		huma.Register(api, op, func(ctx context.Context, in *I) (*Empty, error) {
			_, err := fn(ctx, keyFromContext(ctx), in)
			return nil, a.apiError(err)
		})
		return
	}
	huma.Register(api, op, func(ctx context.Context, in *I) (*restResponse[O], error) {
		out, err := fn(ctx, keyFromContext(ctx), in)
		if err != nil {
			return nil, a.apiError(err)
		}
		if out == nil {
			return nil, a.apiError(errors.New("operation returned no result"))
		}
		return &restResponse[O]{Body: *out}, nil
	})
}

func (a *App) apiError(err error) error {
	if err == nil {
		return nil
	}
	var problem *Problem
	if errors.As(err, &problem) {
		return problem
	}
	a.Logger.Error("API operation failed", "error", err)
	return internalProblem
}

func (a *App) registerREST(router chi.Router) {
	config := huma.DefaultConfig("Gort API", "0.2.0")
	config.OpenAPIPath = "/rest/openapi"
	config.DocsPath = ""
	config.SchemasPath = ""
	config.CreateHooks = nil // Preserve response envelopes without adding $schema.
	config.AllowAdditionalPropertiesByDefault = true
	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"apiKey":     {Type: "apiKey", In: "header", Name: "X-Api-Key"},
		"bearerAuth": {Type: "http", Scheme: "bearer"},
	}
	config.Servers = []*huma.Server{{URL: "/"}}
	config.Security = []map[string][]string{{"apiKey": {}}, {"bearerAuth": {}}}
	config.Info.Description = "Use an API key in X-Api-Key or Authorization: Bearer. Admin keys have full access. Author keys manage links created with that key. Domain keys manage links on their assigned domain. Only admin keys can change shared tags or view global statistics. PATCH distinguishes omitted values from explicit null. Webhook operations appear only when enabled."
	router.Get("/rest/docs", func(w http.ResponseWriter, r *http.Request) { _ = a.render(w, 200, a.baseTemplates, "rest-docs", nil) })
	api := humachi.New(router, config)
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		if ctx.Operation().OperationID == "health" {
			next(ctx)
			return
		}
		r, w := humachi.Unwrap(ctx)
		a.requireAPIKey(func(key *AuthenticatedKey, _ http.ResponseWriter, _ *http.Request) error {
			next(huma.WithValue(ctx, apiKeyContext{}, key))
			return nil
		})(w, r)
	})
	huma.Register(api, huma.Operation{OperationID: "health", Method: "GET", Path: "/rest/health", Summary: "Check database connectivity", Tags: []string{"Health"}, Security: []map[string][]string{}, Responses: map[string]*huma.Response{"503": {Description: "Database unavailable", Content: map[string]*huma.MediaType{"application/json": {Schema: api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[healthBody](), true, "")}}}}}, func(ctx context.Context, _ *Empty) (*healthResponse, error) {
		out := &healthResponse{Status: 200, Body: healthBody{Status: "pass", Version: "1.0.0"}}
		var one int64
		if err := a.DB.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
			out.Status = 503
			out.Body.Status = "fail"
		}
		return out, nil
	})

	registerOperation(a, api, "GET", "/rest/v1/short-urls", "listShortURLs", "List short URLs", 200, a.opListShortURLs)
	registerOperation(a, api, "POST", "/rest/v1/short-urls", "createShortURL", "Create short URL", 201, a.opCreateShortURL)
	registerOperation(a, api, "GET", "/rest/v1/short-urls/{code}", "getShortURL", "Get short URL", 200, a.opGetShortURL)
	registerOperation(a, api, "PATCH", "/rest/v1/short-urls/{code}", "editShortURL", "Edit short URL", 200, a.opEditShortURL)
	registerOperation(a, api, "DELETE", "/rest/v1/short-urls/{code}", "deleteShortURL", "Delete short URL", 204, a.opDeleteShortURL)
	registerOperation(a, api, "GET", "/rest/v1/short-urls/{code}/redirect-rules", "getRules", "Get redirect rules", 200, a.opGetRules)
	registerOperation(a, api, "POST", "/rest/v1/short-urls/{code}/redirect-rules", "setRules", "Set redirect rules", 200, a.opSetRules)
	registerOperation(a, api, "GET", "/rest/v1/short-urls/{code}/visits", "listShortURLVisits", "List visits for a short URL", 200, a.opListShortURLVisits)
	registerOperation(a, api, "DELETE", "/rest/v1/short-urls/{code}/visits", "deleteShortURLVisits", "Delete visits for a short URL", 200, a.opDeleteShortURLVisits)
	registerOperation(a, api, "GET", "/rest/v1/tags", "listTags", "List tags", 200, a.opListTags)
	registerOperation(a, api, "PUT", "/rest/v1/tags", "renameTag", "Rename tag (admin)", 200, a.opRenameTag)
	registerOperation(a, api, "DELETE", "/rest/v1/tags", "deleteTags", "Delete tags (admin)", 200, a.opDeleteTags)
	registerOperation(a, api, "GET", "/rest/v1/tags/{tag}/visits", "tagVisits", "List tag visits (admin)", 200, a.opTagVisits)
	registerOperation(a, api, "GET", "/rest/v1/domains", "listDomains", "List domains", 200, a.opListDomains)
	registerOperation(a, api, "POST", "/rest/v1/domains", "createDomain", "Create domain (admin)", 201, a.opCreateDomain)
	registerOperation(a, api, "PATCH", "/rest/v1/domains/redirects", "setDomainRedirects", "Set domain redirects (admin)", 200, a.opSetDomainRedirects)
	registerOperation(a, api, "DELETE", "/rest/v1/domains/{authority}", "deleteDomain", "Delete domain (admin)", 204, a.opDeleteDomain)
	registerOperation(a, api, "GET", "/rest/v1/domains/{authority}/visits", "domainVisits", "List domain visits", 200, a.opDomainVisits)
	registerOperation(a, api, "GET", "/rest/v1/visits", "visitsOverview", "Visit totals (admin)", 200, a.opVisitsOverview)
	registerOperation(a, api, "GET", "/rest/v1/visits/non-orphan", "listNonOrphanVisits", "List non-orphan visits", 200, a.opListNonOrphanVisits)
	registerOperation(a, api, "GET", "/rest/v1/visits/orphan", "listOrphanVisits", "List orphan visits (admin)", 200, a.opListOrphanVisits)
	registerOperation(a, api, "DELETE", "/rest/v1/visits/orphan", "deleteOrphanVisits", "Delete orphan visits (admin)", 200, a.opDeleteOrphanVisits)
	registerOperation(a, api, "GET", "/rest/v1/stats/visits-per-day", "visitsPerDay", "Visits per day", 200, a.opVisitsPerDay)
	registerOperation(a, api, "GET", "/rest/v1/stats/breakdown", "breakdown", "Visit breakdown", 200, a.opBreakdown)
	registerOperation(a, api, "GET", "/rest/v1/api-keys", "listAPIKeys", "List API keys (admin)", 200, a.opListAPIKeys)
	registerOperation(a, api, "POST", "/rest/v1/api-keys", "createAPIKey", "Create API key (admin)", 201, a.opCreateAPIKey)
	registerOperation(a, api, "PATCH", "/rest/v1/api-keys/{id}", "patchAPIKey", "Patch API key (admin)", 200, a.opPatchAPIKey)
	registerOperation(a, api, "DELETE", "/rest/v1/api-keys/{id}", "deleteAPIKey", "Delete API key (admin)", 204, a.opDeleteAPIKey)
	if a.Cfg.WebhooksEnabled {
		registerOperation(a, api, "GET", "/rest/v1/webhooks", "listWebhooks", "List webhooks (admin)", 200, a.opListWebhooks)
		registerOperation(a, api, "POST", "/rest/v1/webhooks", "createWebhook", "Create webhook (admin)", 201, a.opCreateWebhook)
		registerOperation(a, api, "PATCH", "/rest/v1/webhooks/{id}", "patchWebhook", "Update webhook (admin)", 200, a.opPatchWebhook)
		registerOperation(a, api, "DELETE", "/rest/v1/webhooks/{id}", "deleteWebhook", "Delete webhook (admin)", 204, a.opDeleteWebhook)
	}
	// Tags have two documented response shapes, selected by withStats.
	tags := api.OpenAPI().Paths["/rest/v1/tags"].Get.Responses["200"].Content["application/json"]
	tags.Schema = &huma.Schema{OneOf: []*huma.Schema{
		api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[PageDTO[string]](), true, ""),
		api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[PageDTO[tagStatsDTO]](), true, ""),
	}}
}

type healthBody struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}
type healthResponse struct {
	Status int
	Body   healthBody
}
