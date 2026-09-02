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

// GET /admin/domains (admin)
func (a *App) uiListDomains(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	domains, err := data.ListDomainsWithStats(a.Db)
	if err != nil {
		a.serverError(w, err)
		return
	}

	valueOf := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}

	var rows []h.Node
	for _, d := range domains {
		rows = append(rows, h.E("tr", nil,
			h.E("td", nil,
				h.E("span", []h.Attr{h.A("class", "mono")}, h.Text(d.Authority)),
				h.IfNode(d.IsDefault, h.Text(" ")),
				h.IfNode(d.IsDefault,
					h.E("span", []h.Attr{h.A("class", "badge green")}, h.Text("default")))),
			h.E("td", nil, h.Text(strconv.FormatInt(d.ShortUrlCount, 10))),
			h.E("td", nil, h.Text(strconv.FormatInt(d.VisitCount, 10))),
			h.E("td", nil,
				h.E("form", []h.Attr{
					h.A("method", "post"),
					h.A("action", fmt.Sprintf("/admin/domains/%d/redirects", d.Id)),
					h.A("class", "stack"),
					h.A("style", "max-width:100%"),
				},
					h.E("div", []h.Attr{h.A("class", "row")},
						h.E("input", []h.Attr{
							h.A("type", "url"), h.A("name", "baseUrlRedirect"),
							h.A("placeholder", "Base URL redirect"),
							h.A("value", valueOf(d.BaseUrlRedirect)),
						}),
						h.E("input", []h.Attr{
							h.A("type", "url"), h.A("name", "regular404Redirect"),
							h.A("placeholder", "Regular 404 redirect"),
							h.A("value", valueOf(d.Regular404Redirect)),
						}),
						h.E("input", []h.Attr{
							h.A("type", "url"), h.A("name", "invalidShortUrlRedirect"),
							h.A("placeholder", "Invalid short URL redirect"),
							h.A("value", valueOf(d.InvalidShortUrlRedirect)),
						}),
						h.E("button", []h.Attr{h.A("class", "secondary small")}, h.Text("Save"))))),
			h.E("td", []h.Attr{h.A("class", "actions")},
				h.IfNode(!d.IsDefault,
					h.E("form", []h.Attr{
						h.A("class", "inline"), h.A("method", "post"),
						h.A("action", fmt.Sprintf("/admin/domains/%d/delete", d.Id)),
						h.A("onsubmit", "return confirm('Delete this domain and ALL its short URLs?')"),
					},
						h.E("button", []h.Attr{h.A("class", "danger small")}, h.Text("Delete")))))))
	}

	content := []h.Node{
		h.E("h1", nil, h.Text("Domains")),
		h.E("p", []h.Attr{h.A("class", "muted")},
			h.Text("Short URLs are unique per domain. Point extra domains at this server and register them here (or let them auto-register on first use).")),
		h.E("div", []h.Attr{h.A("class", "table-wrap")},
			h.E("table", nil,
				h.E("thead", nil,
					h.E("tr", nil,
						h.E("th", nil, h.Text("Domain")),
						h.E("th", nil, h.Text("Short URLs")),
						h.E("th", nil, h.Text("Visits")),
						h.E("th", nil, h.Text("Not-found redirects (base / 404 / invalid)")),
						h.E("th", nil))),
				h.E("tbody", nil, rows...))),
		h.E("h2", nil, h.Text("Add domain")),
		h.E("div", []h.Attr{h.A("class", "card")},
			h.E("form", []h.Attr{h.A("class", "row"), h.A("method", "post"), h.A("action", "/admin/domains")},
				formField("Authority (host or host:port)", textInput("authority", "", "links.example.com")),
				h.E("div", nil, h.E("button", nil, h.Text("Add domain"))))),
	}
	respondPage(w, user, "/admin/domains", "Domains", content)
}

// POST /admin/domains (admin)
func (a *App) uiCreateDomain(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		BadRequest(w, "Invalid form submission.")
		return
	}
	backLink := h.E("p", nil,
		h.E("a", []h.Attr{h.A("href", "/admin/domains")}, h.Text("← Back to domains")))

	authority, err := core.NewDomainAuthority(r.PostFormValue("authority"))
	if err != nil {
		respondHtml(w, http.StatusBadRequest, layoutPage(user, "/admin/domains", "Domains",
			[]h.Node{alertError(err.Error()), backLink}))
		return
	}
	created, err := data.CreateDomain(a.Db, authority)
	if err != nil {
		a.serverError(w, err)
		return
	}
	if created == nil {
		respondPage(w, user, "/admin/domains", "Domains",
			[]h.Node{alertError(fmt.Sprintf("Domain '%s' is already registered.", authority.Value())), backLink})
		return
	}
	http.Redirect(w, r, "/admin/domains", http.StatusFound)
}

// POST /admin/domains/{id}/redirects (admin)
func (a *App) uiSetDomainRedirects(_ *CurrentUser, w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		respondPlainNotFound(w)
		return
	}
	if err := r.ParseForm(); err != nil {
		BadRequest(w, "Invalid form submission.")
		return
	}
	getOpt := func(name string) *string {
		if v := strings.TrimSpace(r.PostFormValue(name)); v != "" {
			return &v
		}
		return nil
	}
	if _, err := data.UpdateDomainRedirects(a.Db, core.DomainID(id),
		getOpt("baseUrlRedirect"), getOpt("regular404Redirect"), getOpt("invalidShortUrlRedirect")); err != nil {
		a.serverError(w, err)
		return
	}
	http.Redirect(w, r, "/admin/domains", http.StatusFound)
}

// POST /admin/domains/{id}/delete (admin)
func (a *App) uiDeleteDomain(_ *CurrentUser, w http.ResponseWriter, r *http.Request) {
	if id, err := strconv.ParseInt(r.PathValue("id"), 10, 64); err == nil {
		if _, err := data.DeleteDomain(a.Db, core.DomainID(id)); err != nil {
			a.serverError(w, err)
			return
		}
	}
	http.Redirect(w, r, "/admin/domains", http.StatusFound)
}
