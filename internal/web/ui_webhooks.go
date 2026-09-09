package web

import (
	"context"
	"net/http"
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

func (a *App) webhooksViewModel(ctx context.Context, errorMessage, secret string) (webhooksView, error) {
	hooks, err := data.ListWebhooks(ctx, a.Db)
	if err != nil {
		return webhooksView{}, err
	}
	model := webhooksView{Error: errorMessage, Secret: secret, Webhooks: hooks}
	for _, e := range core.AllWebhookEvents {
		model.EventChecks = append(model.EventChecks, eventCheckView{
			Field:   webhookEventFieldName(e),
			Label:   e.Slug(),
			Checked: e == core.EventUrlCreated,
		})
	}
	return model, nil
}

func (a *App) renderWebhooksPage(ctx context.Context, w http.ResponseWriter, user *CurrentUser, errorMessage, secret string) error {
	model, err := a.webhooksViewModel(ctx, errorMessage, secret)
	if err != nil {
		return err
	}
	return a.renderPage(w, http.StatusOK, "webhooks", user, "/admin/webhooks", "Webhooks", model)
}

// GET /admin/webhooks (admin)
func (a *App) uiListWebhooks(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	return a.renderWebhooksPage(r.Context(), w, user, "", "")
}

// POST /admin/webhooks (admin) — shows the signing secret once.
func (a *App) uiCreateWebhook(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	name := strings.TrimSpace(r.PostFormValue("name"))
	hookUrl := strings.TrimSpace(r.PostFormValue("url"))
	var events []core.WebhookEvent
	for _, e := range core.AllWebhookEvents {
		if r.PostFormValue(webhookEventFieldName(e)) == "true" {
			events = append(events, e)
		}
	}
	if name == "" || !isHttpUrl(hookUrl) || len(events) == 0 {
		return a.renderWebhooksPage(r.Context(), w, user, "Name, a valid http(s) URL and at least one event are required.", "")
	}
	secret := generateWebhookSecret()
	if _, err := data.InsertWebhook(r.Context(), a.Db, name, hookUrl, secret, events); err != nil {
		return err
	}
	return a.renderWebhooksPage(r.Context(), w, user, "", secret)
}

// POST /admin/webhooks/{id}/toggle (admin)
func (a *App) uiToggleWebhook(_ *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	id, err := pathID[core.WebhookID](r, "id")
	if err != nil {
		return err
	}
	hooks, err := data.ListWebhooks(r.Context(), a.Db)
	if err != nil {
		return err
	}
	for _, hook := range hooks {
		if hook.Id == id {
			if _, err := data.SetWebhookEnabled(r.Context(), a.Db, id, !hook.Enabled); err != nil {
				return err
			}
			break
		}
	}
	return redirect(w, r, "/admin/webhooks")
}

// POST /admin/webhooks/{id}/delete (admin)
func (a *App) uiDeleteWebhook(_ *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	id, err := pathID[core.WebhookID](r, "id")
	if err != nil {
		return err
	}
	if _, err := data.DeleteWebhook(r.Context(), a.Db, id); err != nil {
		return err
	}
	return redirect(w, r, "/admin/webhooks")
}
