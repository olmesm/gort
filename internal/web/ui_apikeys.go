package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
	"github.com/olmesm/gort/internal/h"
)

func (a *App) apiKeysPageContent(banner h.Node) ([]h.Node, error) {
	keys, err := data.ListApiKeys(a.Db)
	if err != nil {
		return nil, err
	}
	domains, err := data.ListDomains(a.Db)
	if err != nil {
		return nil, err
	}
	authorityOf := func(id *int64) string {
		if id == nil {
			return "—"
		}
		for _, d := range domains {
			if d.Id == *id {
				return d.Authority
			}
		}
		return "—"
	}

	var rows []h.Node
	for _, k := range keys {
		expired := k.ExpiresAt != nil && !k.ExpiresAt.After(time.Now().UTC())
		name := "—"
		if k.Name != nil {
			name = *k.Name
		}
		expires := "never"
		if k.ExpiresAt != nil {
			expires = formatDateTime(*k.ExpiresAt)
		}
		var statusBadge h.Node
		switch {
		case expired:
			statusBadge = h.E("span", []h.Attr{h.A("class", "badge red")}, h.Text("expired"))
		case k.Enabled:
			statusBadge = h.E("span", []h.Attr{h.A("class", "badge green")}, h.Text("enabled"))
		default:
			statusBadge = h.E("span", []h.Attr{h.A("class", "badge red")}, h.Text("disabled"))
		}
		toggleLabel := "Enable"
		if k.Enabled {
			toggleLabel = "Disable"
		}

		rows = append(rows, h.E("tr", nil,
			h.E("td", nil, h.Text(name)),
			h.E("td", nil, h.E("span", []h.Attr{h.A("class", "badge gray")}, h.Text(k.Role))),
			h.E("td", nil, h.Text(authorityOf(k.DomainId))),
			h.E("td", nil, statusBadge),
			h.E("td", []h.Attr{h.A("class", "muted")}, h.Text(expires)),
			h.E("td", []h.Attr{h.A("class", "muted")}, h.Text(formatDateTime(k.CreatedAt))),
			h.E("td", []h.Attr{h.A("class", "actions")},
				h.E("form", []h.Attr{
					h.A("class", "inline"), h.A("method", "post"),
					h.A("action", fmt.Sprintf("/admin/api-keys/%d/toggle", k.Id)),
				},
					h.E("button", []h.Attr{h.A("class", "secondary small")}, h.Text(toggleLabel))),
				h.Text(" "),
				h.E("form", []h.Attr{
					h.A("class", "inline"), h.A("method", "post"),
					h.A("action", fmt.Sprintf("/admin/api-keys/%d/delete", k.Id)),
					h.A("onsubmit", "return confirm('Delete this API key?')"),
				},
					h.E("button", []h.Attr{h.A("class", "danger small")}, h.Text("Delete"))))))
	}

	domainOptions := []h.Node{h.E("option", []h.Attr{h.A("value", "")}, h.Text("—"))}
	for _, d := range domains {
		domainOptions = append(domainOptions,
			h.E("option", []h.Attr{h.A("value", d.Authority)}, h.Text(d.Authority)))
	}

	return []h.Node{
		h.E("h1", nil, h.Text("API keys")),
		h.E("p", []h.Attr{h.A("class", "muted")},
			h.Text("Keys authenticate REST API calls via the X-Api-Key header. Admin keys can do everything; author keys only see short URLs they created; domain keys are limited to one domain.")),
		banner,
		h.E("div", []h.Attr{h.A("class", "table-wrap")},
			h.E("table", nil,
				h.E("thead", nil,
					h.E("tr", nil,
						h.E("th", nil, h.Text("Name")),
						h.E("th", nil, h.Text("Role")),
						h.E("th", nil, h.Text("Domain")),
						h.E("th", nil, h.Text("Status")),
						h.E("th", nil, h.Text("Expires (UTC)")),
						h.E("th", nil, h.Text("Created (UTC)")),
						h.E("th", nil))),
				h.E("tbody", nil, rows...))),
		h.E("h2", nil, h.Text("Create API key")),
		h.E("div", []h.Attr{h.A("class", "card")},
			h.E("form", []h.Attr{h.A("class", "row"), h.A("method", "post"), h.A("action", "/admin/api-keys")},
				formField("Name", textInput("name", "", "ci-deploy")),
				formField("Role",
					h.E("select", []h.Attr{h.A("name", "role")},
						h.E("option", []h.Attr{h.A("value", "admin")}, h.Text("admin")),
						h.E("option", []h.Attr{h.A("value", "author")}, h.Text("author")),
						h.E("option", []h.Attr{h.A("value", "domain")}, h.Text("domain")))),
				formField("Domain (for domain role)",
					h.E("select", []h.Attr{h.A("name", "domain")}, domainOptions...)),
				formField("Expires (UTC, optional)",
					h.E("input", []h.Attr{h.A("type", "datetime-local"), h.A("name", "expiresAt")})),
				h.E("div", nil, h.E("button", nil, h.Text("Create"))))),
	}, nil
}

// GET /admin/api-keys (admin)
func (a *App) uiListApiKeys(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	content, err := a.apiKeysPageContent(h.Empty())
	if err != nil {
		a.serverError(w, err)
		return
	}
	respondPage(w, user, "/admin/api-keys", "API keys", content)
}

// POST /admin/api-keys (admin) — shows the plaintext key once.
func (a *App) uiCreateApiKey(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		BadRequest(w, "Invalid form submission.")
		return
	}
	var name *string
	if n := strings.TrimSpace(r.PostFormValue("name")); n != "" {
		name = &n
	}
	var domain *data.DomainRow
	if authority := r.PostFormValue("domain"); authority != "" {
		var err error
		domain, err = data.TryGetDomainByAuthority(a.Db, strings.ToLower(authority))
		if err != nil {
			a.serverError(w, err)
			return
		}
	}

	var role core.ApiKeyRole
	roleError := ""
	switch r.PostFormValue("role") {
	case "author":
		role = core.AuthorRole()
	case "domain":
		if domain == nil {
			roleError = "Domain-role keys need a domain."
		} else {
			role = core.DomainRole(core.DomainID(domain.Id))
		}
	default:
		role = core.AdminRole()
	}

	if roleError != "" {
		content, err := a.apiKeysPageContent(alertError(roleError))
		if err != nil {
			a.serverError(w, err)
			return
		}
		respondPage(w, user, "/admin/api-keys", "API keys", content)
		return
	}

	var expiresAt *time.Time
	if v := r.PostFormValue("expiresAt"); v != "" {
		expiresAt = TryParseDate(v)
	}

	plainKey := GenerateApiKey()
	if _, err := data.InsertApiKey(a.Db, HashApiKey(plainKey), name, role, expiresAt); err != nil {
		a.serverError(w, err)
		return
	}
	banner := alertSuccess(
		h.Text("API key created — copy it now, it will not be shown again: "),
		h.E("br", nil),
		h.E("strong", []h.Attr{h.A("class", "mono")}, h.Text(plainKey)))
	content, err := a.apiKeysPageContent(banner)
	if err != nil {
		a.serverError(w, err)
		return
	}
	respondPage(w, user, "/admin/api-keys", "API keys", content)
}

// POST /admin/api-keys/{id}/toggle (admin)
func (a *App) uiToggleApiKey(_ *CurrentUser, w http.ResponseWriter, r *http.Request) {
	if id, err := strconv.ParseInt(r.PathValue("id"), 10, 64); err == nil {
		key, err := data.TryGetApiKeyById(a.Db, core.ApiKeyID(id))
		if err != nil {
			a.serverError(w, err)
			return
		}
		if key != nil {
			if _, err := data.SetApiKeyEnabled(a.Db, core.ApiKeyID(id), !key.Enabled); err != nil {
				a.serverError(w, err)
				return
			}
		}
	}
	http.Redirect(w, r, "/admin/api-keys", http.StatusFound)
}

// POST /admin/api-keys/{id}/delete (admin)
func (a *App) uiDeleteApiKey(_ *CurrentUser, w http.ResponseWriter, r *http.Request) {
	if id, err := strconv.ParseInt(r.PathValue("id"), 10, 64); err == nil {
		if _, err := data.DeleteApiKey(a.Db, core.ApiKeyID(id)); err != nil {
			a.serverError(w, err)
			return
		}
	}
	http.Redirect(w, r, "/admin/api-keys", http.StatusFound)
}
