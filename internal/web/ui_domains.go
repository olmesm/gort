package web

import (
	"fmt"
	"net/http"
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
func (a *App) uiListDomains(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	domains, err := data.ListDomainsWithStats(r.Context(), a.Db)
	if err != nil {
		return err
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
	return a.renderPage(w, http.StatusOK, "domains", user, "/admin/domains", "Domains", model)
}

func (a *App) renderDomainsMessage(w http.ResponseWriter, status int, user *CurrentUser, message string) error {
	return a.renderPage(w, status, "message", user, "/admin/domains", "Domains", messageView{
		Error:     message,
		BackUrl:   "/admin/domains",
		BackLabel: "← Back to domains",
	})
}

// POST /admin/domains (admin)
func (a *App) uiCreateDomain(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	authority, err := core.NewDomainAuthority(r.PostFormValue("authority"))
	if err != nil {
		return a.renderDomainsMessage(w, http.StatusBadRequest, user, err.Error())
	}
	created, err := data.CreateDomain(r.Context(), a.Db, authority)
	if err != nil {
		return err
	}
	if created == nil {
		return a.renderDomainsMessage(w, http.StatusOK, user,
			fmt.Sprintf("Domain '%s' is already registered.", authority.Value()))
	}
	return redirect(w, r, "/admin/domains")
}

// POST /admin/domains/{id}/redirects (admin)
func (a *App) uiSetDomainRedirects(_ *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	id, err := pathID[core.DomainID](r, "id")
	if err != nil {
		return err
	}
	getOpt := func(name string) *string {
		if v := strings.TrimSpace(r.PostFormValue(name)); v != "" {
			return &v
		}
		return nil
	}
	if _, err := data.UpdateDomainRedirects(r.Context(), a.Db, id,
		getOpt("baseUrlRedirect"), getOpt("regular404Redirect"), getOpt("invalidShortUrlRedirect")); err != nil {
		return err
	}
	return redirect(w, r, "/admin/domains")
}

// POST /admin/domains/{id}/delete (admin)
func (a *App) uiDeleteDomain(_ *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	id, err := pathID[core.DomainID](r, "id")
	if err != nil {
		return err
	}
	if _, err := data.DeleteDomain(r.Context(), a.Db, id); err != nil {
		return err
	}
	return redirect(w, r, "/admin/domains")
}
