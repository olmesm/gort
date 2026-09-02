package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
	"github.com/olmesm/gort/internal/h"
)

// Dots in form field names would be read as nested keys elsewhere; keep the
// same convention as the original UI.
func webhookEventFieldName(e core.WebhookEvent) string {
	return "event_" + strings.ReplaceAll(e.Slug(), ".", "_")
}

func (a *App) webhooksPageContent(banner h.Node) ([]h.Node, error) {
	hooks, err := data.ListWebhooks(a.Db)
	if err != nil {
		return nil, err
	}

	var rows []h.Node
	for _, hook := range hooks {
		var eventBadges []h.Node
		for _, e := range strings.Split(hook.Events, ",") {
			eventBadges = append(eventBadges,
				h.E("span", []h.Attr{h.A("class", "badge gray")}, h.Text(strings.TrimSpace(e))))
		}
		statusBadge := h.E("span", []h.Attr{h.A("class", "badge red")}, h.Text("disabled"))
		if hook.Enabled {
			statusBadge = h.E("span", []h.Attr{h.A("class", "badge green")}, h.Text("enabled"))
		}
		toggleLabel := "Enable"
		if hook.Enabled {
			toggleLabel = "Disable"
		}

		rows = append(rows, h.E("tr", nil,
			h.E("td", nil, h.Text(hook.Name)),
			h.E("td", nil,
				h.E("span", []h.Attr{h.A("class", "truncate mono")}, h.Text(hook.Url))),
			h.E("td", nil, eventBadges...),
			h.E("td", nil, statusBadge),
			h.E("td", []h.Attr{h.A("class", "actions")},
				h.E("form", []h.Attr{
					h.A("class", "inline"), h.A("method", "post"),
					h.A("action", fmt.Sprintf("/admin/webhooks/%d/toggle", hook.Id)),
				},
					h.E("button", []h.Attr{h.A("class", "secondary small")}, h.Text(toggleLabel))),
				h.Text(" "),
				h.E("form", []h.Attr{
					h.A("class", "inline"), h.A("method", "post"),
					h.A("action", fmt.Sprintf("/admin/webhooks/%d/delete", hook.Id)),
					h.A("onsubmit", "return confirm('Delete this webhook?')"),
				},
					h.E("button", []h.Attr{h.A("class", "danger small")}, h.Text("Delete"))))))
	}

	var eventChecks []h.Node
	eventChecks = append(eventChecks, h.E("label", nil, h.Text("Events")))
	for _, e := range core.AllWebhookEvents {
		eventChecks = append(eventChecks,
			checkbox(webhookEventFieldName(e), e == core.EventUrlCreated, e.Slug()))
	}

	return []h.Node{
		h.E("h1", nil, h.Text("Webhooks")),
		h.E("p", []h.Attr{h.A("class", "muted")},
			h.Text("Webhooks receive signed JSON POSTs when events happen. Payloads carry an X-Gort-Signature header (HMAC-SHA256 of the body with the webhook secret).")),
		banner,
		h.E("div", []h.Attr{h.A("class", "table-wrap")},
			h.E("table", nil,
				h.E("thead", nil,
					h.E("tr", nil,
						h.E("th", nil, h.Text("Name")),
						h.E("th", nil, h.Text("URL")),
						h.E("th", nil, h.Text("Events")),
						h.E("th", nil, h.Text("Status")),
						h.E("th", nil))),
				h.E("tbody", nil, rows...))),
		h.E("h2", nil, h.Text("Create webhook")),
		h.E("div", []h.Attr{h.A("class", "card")},
			h.E("form", []h.Attr{h.A("class", "stack"), h.A("method", "post"), h.A("action", "/admin/webhooks")},
				h.E("div", []h.Attr{h.A("class", "row")},
					formField("Name", textInput("name", "", "notify-slack")),
					formField("URL",
						h.E("input", []h.Attr{
							h.A("type", "url"), h.A("name", "url"), h.Flag("required"),
							h.A("placeholder", "https://example.com/hooks/gort"),
						}))),
				h.E("div", nil, eventChecks...),
				h.E("div", nil, h.E("button", nil, h.Text("Create webhook"))))),
	}, nil
}

func (a *App) respondWebhooksPage(w http.ResponseWriter, user *CurrentUser, banner h.Node) {
	content, err := a.webhooksPageContent(banner)
	if err != nil {
		a.serverError(w, err)
		return
	}
	respondPage(w, user, "/admin/webhooks", "Webhooks", content)
}

// GET /admin/webhooks (admin)
func (a *App) uiListWebhooks(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	a.respondWebhooksPage(w, user, h.Empty())
}

// POST /admin/webhooks (admin) — shows the signing secret once.
func (a *App) uiCreateWebhook(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		BadRequest(w, "Invalid form submission.")
		return
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
		a.respondWebhooksPage(w, user,
			alertError("Name, a valid http(s) URL and at least one event are required."))
		return
	}
	secret := generateWebhookSecret()
	if _, err := data.InsertWebhook(a.Db, name, hookUrl, secret, events); err != nil {
		a.serverError(w, err)
		return
	}
	banner := alertSuccess(
		h.Text("Webhook created — its signing secret (copy it now, it will not be shown again): "),
		h.E("br", nil),
		h.E("strong", []h.Attr{h.A("class", "mono")}, h.Text(secret)))
	a.respondWebhooksPage(w, user, banner)
}

// POST /admin/webhooks/{id}/toggle (admin)
func (a *App) uiToggleWebhook(_ *CurrentUser, w http.ResponseWriter, r *http.Request) {
	if id, err := strconv.ParseInt(r.PathValue("id"), 10, 64); err == nil {
		hooks, err := data.ListWebhooks(a.Db)
		if err != nil {
			a.serverError(w, err)
			return
		}
		for _, hook := range hooks {
			if hook.Id == id {
				if _, err := data.SetWebhookEnabled(a.Db, core.WebhookID(id), !hook.Enabled); err != nil {
					a.serverError(w, err)
					return
				}
				break
			}
		}
	}
	http.Redirect(w, r, "/admin/webhooks", http.StatusFound)
}

// POST /admin/webhooks/{id}/delete (admin)
func (a *App) uiDeleteWebhook(_ *CurrentUser, w http.ResponseWriter, r *http.Request) {
	if id, err := strconv.ParseInt(r.PathValue("id"), 10, 64); err == nil {
		if _, err := data.DeleteWebhook(a.Db, core.WebhookID(id)); err != nil {
			a.serverError(w, err)
			return
		}
	}
	http.Redirect(w, r, "/admin/webhooks", http.StatusFound)
}
