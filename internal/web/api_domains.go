package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

type CreateDomainBody struct {
	Domain string `json:"domain"`
}

type DomainRedirectsBody struct {
	Domain                  string  `json:"domain"`
	BaseUrlRedirect         *string `json:"baseUrlRedirect"`
	Regular404Redirect      *string `json:"regular404Redirect"`
	InvalidShortUrlRedirect *string `json:"invalidShortUrlRedirect"`
}

type domainRedirectsDto struct {
	BaseUrlRedirect         *string `json:"baseUrlRedirect,omitempty"`
	Regular404Redirect      *string `json:"regular404Redirect,omitempty"`
	InvalidShortUrlRedirect *string `json:"invalidShortUrlRedirect,omitempty"`
}

type domainDto struct {
	Domain    string             `json:"domain"`
	IsDefault bool               `json:"isDefault"`
	Redirects domainRedirectsDto `json:"redirects"`
}

func newDomainDto(d *data.DomainRow) domainDto {
	return domainDto{
		Domain:    d.Authority,
		IsDefault: d.IsDefault,
		Redirects: domainRedirectsDto{
			BaseUrlRedirect:         d.BaseUrlRedirect,
			Regular404Redirect:      d.Regular404Redirect,
			InvalidShortUrlRedirect: d.InvalidShortUrlRedirect,
		},
	}
}

// GET /rest/v1/domains
func (a *App) apiListDomains(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
	domains, err := data.ListDomains(a.Db)
	if err != nil {
		a.serverError(w, err)
		return
	}
	dtos := make([]domainDto, len(domains))
	for i := range domains {
		dtos[i] = newDomainDto(&domains[i])
	}
	RespondJSON(w, http.StatusOK, map[string]any{"data": dtos})
}

// POST /rest/v1/domains (admin)
func (a *App) apiCreateDomain(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
	body, err := ReadJSON[CreateDomainBody](w, r)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	authority, err := core.NewDomainAuthority(body.Domain)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	created, err := data.CreateDomain(a.Db, authority)
	if err != nil {
		a.serverError(w, err)
		return
	}
	if created == nil {
		Conflict(w, "domain-exists", fmt.Sprintf("Domain '%s' is already registered.", authority.Value()))
		return
	}
	RespondJSON(w, http.StatusCreated, newDomainDto(created))
}

// PATCH /rest/v1/domains/redirects (admin)
func (a *App) apiSetDomainRedirects(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
	body, err := ReadJSON[DomainRedirectsBody](w, r)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	domain, err := data.DomainByAuthority(a.Db, strings.ToLower(strings.TrimSpace(body.Domain)))
	if err != nil {
		a.serverError(w, err)
		return
	}
	if domain == nil {
		NotFound(w, fmt.Sprintf("Domain '%s' is not registered.", body.Domain))
		return
	}
	if _, err := data.UpdateDomainRedirects(a.Db, core.DomainID(domain.Id),
		body.BaseUrlRedirect, body.Regular404Redirect, body.InvalidShortUrlRedirect); err != nil {
		a.serverError(w, err)
		return
	}
	updated, err := data.DomainByID(a.Db, core.DomainID(domain.Id))
	if err != nil || updated == nil {
		a.serverError(w, err)
		return
	}
	RespondJSON(w, http.StatusOK, newDomainDto(updated))
}

// DELETE /rest/v1/domains/{authority} (admin)
func (a *App) apiDeleteDomain(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
	authority := r.PathValue("authority")
	domain, err := data.DomainByAuthority(a.Db, strings.ToLower(authority))
	if err != nil {
		a.serverError(w, err)
		return
	}
	if domain == nil {
		NotFound(w, fmt.Sprintf("Domain '%s' is not registered.", authority))
		return
	}
	if domain.IsDefault {
		Forbidden(w, "The default domain cannot be deleted.")
		return
	}
	if _, err := data.DeleteDomain(a.Db, core.DomainID(domain.Id)); err != nil {
		a.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /rest/v1/domains/{authority}/visits
func (a *App) apiDomainVisits(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
	authority := r.PathValue("authority")
	domain, err := data.DomainByAuthority(a.Db, strings.ToLower(authority))
	if err != nil {
		a.serverError(w, err)
		return
	}
	if domain == nil {
		NotFound(w, fmt.Sprintf("Domain '%s' is not registered.", authority))
		return
	}
	allowed := false
	switch key.Role.Kind {
	case core.RoleAdmin:
		allowed = true
	case core.RoleDomain:
		allowed = key.Role.DomainID.Value() == domain.Id
	}
	if !allowed {
		Forbidden(w, "This API key cannot view visits for this domain.")
		return
	}
	page, err := data.ListVisitsForDomain(a.Db, core.DomainID(domain.Id), visitFiltersFromQuery(r.URL.Query()))
	if err != nil {
		a.serverError(w, err)
		return
	}
	RespondJSON(w, http.StatusOK, NewPageDto(page, NewVisitDto))
}
