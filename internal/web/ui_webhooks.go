package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

// Dots in form field names would be read as nested keys elsewhere; keep the
// same convention as the original UI.
func webhookEventFieldName(e core.WebhookEvent) string {
	return "event_" + strings.ReplaceAll(e.Slug(), ".", "_")
}

type webhooksView struct {
	Filters     listControlsView
	Pager       pagerView
	Error       string
	Secret      string
	Webhooks    []data.WebhookRow
	EventChecks []eventCheckView
}

type eventCheckView struct {
	Field   string
	Label   string
	Checked bool
}

func (a *App) webhooksViewModel(ctx context.Context, q url.Values, errorMessage, secret string) (webhooksView, error) {
	var event *core.WebhookEvent
	if e, ok := core.WebhookEventOfSlug(q.Get("event")); ok {
		event = &e
	}
	page, err := data.ListWebhooksPage(ctx, a.DB, listFilters(q), q.Get("status"), event)
	if err != nil {
		return webhooksView{}, err
	}
	model := webhooksView{Error: errorMessage, Secret: secret, Webhooks: page.Items}
	model.Pager = newPager(page, func(p int) string { return listPageURL("/admin/webhooks", q, p) })
	model.Filters = listControls("/admin/webhooks", q, "Search name or URL…",
		listSelect(q, "status", "Status", "enabled", "disabled"), listSelect(q, "event", "Event", strings.Split(allEventSlugs(), ", ")...))
	for _, e := range core.AllWebhookEvents {
		model.EventChecks = append(model.EventChecks, eventCheckView{
			Field:   webhookEventFieldName(e),
			Label:   e.Slug(),
			Checked: e == core.EventURLCreated,
		})
	}
	return model, nil
}

func (a *App) renderWebhooksPage(r *http.Request, w http.ResponseWriter, user *CurrentUser, errorMessage, secret string) error {
	model, err := a.webhooksViewModel(r.Context(), r.URL.Query(), errorMessage, secret)
	if err != nil {
		return err
	}
	return a.renderPage(w, http.StatusOK, "webhooks", user, "/admin/webhooks", "Webhooks", model)
}

// GET /admin/webhooks (admin)
func (a *App) uiListWebhooks(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	return a.renderWebhooksPage(r, w, user, "", "")
}

// POST /admin/webhooks (admin) — shows the signing secret once.
func (a *App) uiCreateWebhook(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	body := CreateWebhookBody{Name: strings.TrimSpace(r.PostFormValue("name")), URL: strings.TrimSpace(r.PostFormValue("url"))}
	for _, e := range core.AllWebhookEvents {
		if r.PostFormValue(webhookEventFieldName(e)) == "true" {
			body.Events = append(body.Events, e.Slug())
		}
	}
	hook, err := a.createWebhook(r.Context(), &body)
	if err != nil {
		var problem *Problem
		if errors.As(err, &problem) && problem.Status == http.StatusBadRequest {
			return a.renderWebhooksPage(r, w, user, problem.Detail, "")
		}
		return err
	}
	return a.renderWebhooksPage(r, w, user, "", hook.Secret)
}

// POST /admin/webhooks/{id}/toggle (admin)
func (a *App) uiToggleWebhook(_ *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	id, err := pathID[core.WebhookID](r, "id")
	if err != nil {
		return err
	}
	if _, err := data.ToggleWebhook(r.Context(), a.DB, id); err != nil {
		return err
	}
	return redirect(w, r, "/admin/webhooks")
}

// POST /admin/webhooks/{id}/delete (admin)
func (a *App) uiDeleteWebhook(_ *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	id, err := pathID[core.WebhookID](r, "id")
	if err != nil {
		return err
	}
	if _, err := data.DeleteWebhook(r.Context(), a.DB, id); err != nil {
		return err
	}
	return redirect(w, r, "/admin/webhooks")
}
