package web

import (
	"context"
	"fmt"
	"strings"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

type CreateDomainBody struct {
	Domain string `json:"domain"`
}

type DomainRedirectsBody struct {
	Domain                  string  `json:"domain"`
	BaseURLRedirect         *string `json:"baseUrlRedirect" required:"false"`
	Regular404Redirect      *string `json:"regular404Redirect" required:"false"`
	InvalidShortURLRedirect *string `json:"invalidShortUrlRedirect" required:"false"`
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
func (a *App) opListDomains(ctx context.Context, _ *AuthenticatedKey, in *Empty) (*DataList[domainDTO], error) {
	domains, err := data.ListDomains(ctx, a.DB)
	if err != nil {
		return nil, err
	}
	dtos := make([]domainDTO, len(domains))
	for i := range domains {
		dtos[i] = newDomainDTO(&domains[i])
	}
	return result(DataList[domainDTO]{Data: dtos})
}

// POST /rest/v1/domains (admin)
func (a *App) opCreateDomain(ctx context.Context, key *AuthenticatedKey, in *BodyInput[CreateDomainBody]) (*domainDTO, error) {
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("This operation requires an admin API key.")
	}
	body := &in.Body
	authority, err := core.NewDomainAuthority(body.Domain)
	if err != nil {
		return nil, BadRequest(err.Error())
	}
	created, err := data.CreateDomain(ctx, a.DB, authority)
	if err != nil {
		return nil, err
	}
	if created == nil {
		return nil, Conflict("domain-exists", fmt.Sprintf("Domain '%s' is already registered.", authority.Value()))
	}
	return result(newDomainDTO(created))
}

// PATCH /rest/v1/domains/redirects (admin)
func (a *App) opSetDomainRedirects(ctx context.Context, key *AuthenticatedKey, in *BodyInput[DomainRedirectsBody]) (*domainDTO, error) {
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("This operation requires an admin API key.")
	}
	body := &in.Body
	domain, err := data.DomainByAuthority(ctx, a.DB, strings.ToLower(strings.TrimSpace(body.Domain)))
	if err != nil {
		return nil, err
	}
	if domain == nil {
		return nil, NotFound(fmt.Sprintf("Domain '%s' is not registered.", body.Domain))
	}
	if _, err := data.UpdateDomainRedirects(ctx, a.DB, domain.ID,
		body.BaseURLRedirect, body.Regular404Redirect, body.InvalidShortURLRedirect); err != nil {
		return nil, err
	}
	updated, err := data.DomainByID(ctx, a.DB, domain.ID)
	if err != nil || updated == nil {
		return nil, err
	}
	return result(newDomainDTO(updated))
}

// DELETE /rest/v1/domains/{authority} (admin)
func (a *App) opDeleteDomain(ctx context.Context, key *AuthenticatedKey, in *AuthorityInput) (*Empty, error) {
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("This operation requires an admin API key.")
	}
	authority := in.Authority
	domain, err := data.DomainByAuthority(ctx, a.DB, strings.ToLower(authority))
	if err != nil {
		return nil, err
	}
	if domain == nil {
		return nil, NotFound(fmt.Sprintf("Domain '%s' is not registered.", authority))
	}
	if domain.IsDefault {
		return nil, Forbidden("The default domain cannot be deleted.")
	}
	if _, err := data.DeleteDomain(ctx, a.DB, domain.ID); err != nil {
		return nil, err
	}
	return nil, nil
}

// GET /rest/v1/domains/{authority}/visits
func (a *App) opDomainVisits(ctx context.Context, key *AuthenticatedKey, in *domainVisitOptions) (*PageDTO[VisitDTO], error) {
	authority := in.Authority
	domain, err := data.DomainByAuthority(ctx, a.DB, strings.ToLower(authority))
	if err != nil {
		return nil, err
	}
	if domain == nil {
		return nil, NotFound(fmt.Sprintf("Domain '%s' is not registered.", authority))
	}
	allowed := false
	switch key.Role.Kind {
	case core.RoleAdmin:
		allowed = true
	case core.RoleDomain:
		allowed = key.Role.DomainID == domain.ID
	}
	if !allowed {
		return nil, Forbidden("This API key cannot view visits for this domain.")
	}
	page, err := data.ListVisitsForDomain(ctx, a.DB, domain.ID, in.VisitFilters)
	if err != nil {
		return nil, err
	}
	return result(NewPageDTO(page, NewVisitDTO))
}
