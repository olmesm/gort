package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

type domainsView struct {
	Rows []domainRowView
}

type domainRowView struct {
	Authority               string
	IsDefault               bool
	ShortUrlCount           int64
	VisitCount              int64
	BaseUrlRedirect         string
	Regular404Redirect      string
	InvalidShortUrlRedirect string
	RedirectsAction         string
	DeleteAction            string
}

type messageView struct {
	Error     string
	BackUrl   string
	BackLabel string
}

// GET /admin/domains (admin)
func (a *App) uiListDomains(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	domains, err := data.ListDomainsWithStats(a.Db)
	if err != nil {
		a.serverError(w, err)
		return
	}
	model := domainsView{}
	for _, d := range domains {
		model.Rows = append(model.Rows, domainRowView{
			Authority:               d.Authority,
			IsDefault:               d.IsDefault,
			ShortUrlCount:           d.ShortUrlCount,
			VisitCount:              d.VisitCount,
			BaseUrlRedirect:         valueOrEmpty(d.BaseUrlRedirect),
			Regular404Redirect:      valueOrEmpty(d.Regular404Redirect),
			InvalidShortUrlRedirect: valueOrEmpty(d.InvalidShortUrlRedirect),
			RedirectsAction:         fmt.Sprintf("/admin/domains/%d/redirects", d.Id),
			DeleteAction:            fmt.Sprintf("/admin/domains/%d/delete", d.Id),
		})
	}
	a.renderPage(w, http.StatusOK, "domains", user, "/admin/domains", "Domains", model)
}

func (a *App) renderDomainsMessage(w http.ResponseWriter, status int, user *CurrentUser, message string) {
	a.renderPage(w, status, "message", user, "/admin/domains", "Domains", messageView{
		Error:     message,
		BackUrl:   "/admin/domains",
		BackLabel: "← Back to domains",
	})
}

// POST /admin/domains (admin)
func (a *App) uiCreateDomain(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		BadRequest(w, "Invalid form submission.")
		return
	}
	authority, err := core.NewDomainAuthority(r.PostFormValue("authority"))
	if err != nil {
		a.renderDomainsMessage(w, http.StatusBadRequest, user, err.Error())
		return
	}
	created, err := data.CreateDomain(a.Db, authority)
	if err != nil {
		a.serverError(w, err)
		return
	}
	if created == nil {
		a.renderDomainsMessage(w, http.StatusOK, user,
			fmt.Sprintf("Domain '%s' is already registered.", authority.Value()))
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
