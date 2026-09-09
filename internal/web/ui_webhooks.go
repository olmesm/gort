package web

import (
	"fmt"
	"net/http"
	"strconv"
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
	Rows        []webhookRowView
	EventChecks []eventCheckView
}

type webhookRowView struct {
	Name         string
	Url          string
	Events       []string
	Enabled      bool
	ToggleAction string
	DeleteAction string
}

type eventCheckView struct {
	Field   string
	Label   string
	Checked bool
}

func (a *App) webhooksViewModel(errorMessage, secret string) (webhooksView, error) {
	hooks, err := data.ListWebhooks(a.Db)
	if err != nil {
		return webhooksView{}, err
	}
	model := webhooksView{Error: errorMessage, Secret: secret}
	for _, hook := range hooks {
		var events []string
		for _, e := range strings.Split(hook.Events, ",") {
			events = append(events, strings.TrimSpace(e))
		}
		model.Rows = append(model.Rows, webhookRowView{
			Name:         hook.Name,
			Url:          hook.Url,
			Events:       events,
			Enabled:      hook.Enabled,
			ToggleAction: fmt.Sprintf("/admin/webhooks/%d/toggle", hook.Id),
			DeleteAction: fmt.Sprintf("/admin/webhooks/%d/delete", hook.Id),
		})
	}
	for _, e := range core.AllWebhookEvents {
		model.EventChecks = append(model.EventChecks, eventCheckView{
			Field:   webhookEventFieldName(e),
			Label:   e.Slug(),
			Checked: e == core.EventUrlCreated,
		})
	}
	return model, nil
}

func (a *App) renderWebhooksPage(w http.ResponseWriter, user *CurrentUser, errorMessage, secret string) error {
	model, err := a.webhooksViewModel(errorMessage, secret)
	if err != nil {
		return err
	}
	return a.renderPage(w, http.StatusOK, "webhooks", user, "/admin/webhooks", "Webhooks", model)
}

// GET /admin/webhooks (admin)
func (a *App) uiListWebhooks(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	return a.renderWebhooksPage(w, user, "", "")
}

// POST /admin/webhooks (admin) — shows the signing secret once.
func (a *App) uiCreateWebhook(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return BadRequest("Invalid form submission.")
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	hookUrl := strings.TrimSpace(r.PostFormValue("url"))
	var events []core.WebhookEvent
	for _, e := range core.AllWebhookEvents {
		if r.PostFormValue(webhookEventFieldName(e)) == "true" {
			events = append(events, e)
		}
	}
	if name == "" || !isHttpUrl(hookUrl) || len(events) == 0 {
		return a.renderWebhooksPage(w, user, "Name, a valid http(s) URL and at least one event are required.", "")
	}
	secret := generateWebhookSecret()
	if _, err := data.InsertWebhook(a.Db, name, hookUrl, secret, events); err != nil {
		return err
	}
	return a.renderWebhooksPage(w, user, "", secret)
}

// POST /admin/webhooks/{id}/toggle (admin)
func (a *App) uiToggleWebhook(_ *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	if id, err := strconv.ParseInt(r.PathValue("id"), 10, 64); err == nil {
		hooks, err := data.ListWebhooks(a.Db)
		if err != nil {
			return err
		}
		for _, hook := range hooks {
			if hook.Id == id {
				if _, err := data.SetWebhookEnabled(a.Db, core.WebhookID(id), !hook.Enabled); err != nil {
					return err
				}
				break
			}
		}
	}
	return redirect(w, r, "/admin/webhooks")
}

// POST /admin/webhooks/{id}/delete (admin)
func (a *App) uiDeleteWebhook(_ *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	if id, err := strconv.ParseInt(r.PathValue("id"), 10, 64); err == nil {
		if _, err := data.DeleteWebhook(a.Db, core.WebhookID(id)); err != nil {
			return err
		}
	}
	return redirect(w, r, "/admin/webhooks")
}
