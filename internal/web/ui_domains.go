package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

type domainsView struct {
	Domains []data.DomainStatsRow
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
	return a.renderPage(w, http.StatusOK, "domains", user, "/admin/domains", "Domains", domainsView{Domains: domains})
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
