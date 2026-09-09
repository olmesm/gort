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
	BaseURLRedirect         *string `json:"baseUrlRedirect"`
	Regular404Redirect      *string `json:"regular404Redirect"`
	InvalidShortURLRedirect *string `json:"invalidShortUrlRedirect"`
}

type domainRedirectsDTO struct {
	BaseURLRedirect         *string `json:"baseUrlRedirect,omitempty"`
	Regular404Redirect      *string `json:"regular404Redirect,omitempty"`
	InvalidShortURLRedirect *string `json:"invalidShortUrlRedirect,omitempty"`
}

type domainDTO struct {
	Domain    string             `json:"domain"`
	IsDefault bool               `json:"isDefault"`
	Redirects domainRedirectsDTO `json:"redirects"`
}

func newDomainDTO(d *data.DomainRow) domainDTO {
	return domainDTO{
		Domain:    d.Authority,
		IsDefault: d.IsDefault,
		Redirects: domainRedirectsDTO{
			BaseURLRedirect:         d.BaseURLRedirect,
			Regular404Redirect:      d.Regular404Redirect,
			InvalidShortURLRedirect: d.InvalidShortURLRedirect,
		},
	}
}

// GET /rest/v1/domains
func (a *App) apiListDomains(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	domains, err := data.ListDomains(r.Context(), a.DB)
	if err != nil {
		return err
	}
	dtos := make([]domainDTO, len(domains))
	for i := range domains {
		dtos[i] = newDomainDTO(&domains[i])
	}
	return RespondJSON(w, http.StatusOK, map[string]any{"data": dtos})
}

// POST /rest/v1/domains (admin)
func (a *App) apiCreateDomain(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	body, err := ReadJSON[CreateDomainBody](w, r)
	if err != nil {
		return BadRequest(err.Error())
	}
	authority, err := core.NewDomainAuthority(body.Domain)
	if err != nil {
		return BadRequest(err.Error())
	}
	created, err := data.CreateDomain(r.Context(), a.DB, authority)
	if err != nil {
		return err
	}
	if created == nil {
		return Conflict("domain-exists", fmt.Sprintf("Domain '%s' is already registered.", authority.Value()))
	}
	return RespondJSON(w, http.StatusCreated, newDomainDTO(created))
}

// PATCH /rest/v1/domains/redirects (admin)
func (a *App) apiSetDomainRedirects(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	body, err := ReadJSON[DomainRedirectsBody](w, r)
	if err != nil {
		return BadRequest(err.Error())
	}
	domain, err := data.DomainByAuthority(r.Context(), a.DB, strings.ToLower(strings.TrimSpace(body.Domain)))
	if err != nil {
		return err
	}
	if domain == nil {
		return NotFound(fmt.Sprintf("Domain '%s' is not registered.", body.Domain))
	}
	if _, err := data.UpdateDomainRedirects(r.Context(), a.DB, domain.ID,
		body.BaseURLRedirect, body.Regular404Redirect, body.InvalidShortURLRedirect); err != nil {
		return err
	}
	updated, err := data.DomainByID(r.Context(), a.DB, domain.ID)
	if err != nil || updated == nil {
		return err
	}
	return RespondJSON(w, http.StatusOK, newDomainDTO(updated))
}

// DELETE /rest/v1/domains/{authority} (admin)
func (a *App) apiDeleteDomain(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	authority := r.PathValue("authority")
	domain, err := data.DomainByAuthority(r.Context(), a.DB, strings.ToLower(authority))
	if err != nil {
		return err
	}
	if domain == nil {
		return NotFound(fmt.Sprintf("Domain '%s' is not registered.", authority))
	}
	if domain.IsDefault {
		return Forbidden("The default domain cannot be deleted.")
	}
	if _, err := data.DeleteDomain(r.Context(), a.DB, domain.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// GET /rest/v1/domains/{authority}/visits
func (a *App) apiDomainVisits(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	authority := r.PathValue("authority")
	domain, err := data.DomainByAuthority(r.Context(), a.DB, strings.ToLower(authority))
	if err != nil {
		return err
	}
	if domain == nil {
		return NotFound(fmt.Sprintf("Domain '%s' is not registered.", authority))
	}
	allowed := false
	switch key.Role.Kind {
	case core.RoleAdmin:
		allowed = true
	case core.RoleDomain:
		allowed = key.Role.DomainID == domain.ID
	}
	if !allowed {
		return Forbidden("This API key cannot view visits for this domain.")
	}
	page, err := data.ListVisitsForDomain(r.Context(), a.DB, domain.ID, visitFiltersFromQuery(r.URL.Query()))
	if err != nil {
		return err
	}
	return RespondJSON(w, http.StatusOK, NewPageDTO(page, NewVisitDTO))
}
