package web

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

type CreateAPIKeyBody struct {
	Name      *string    `json:"name" required:"false"`
	Role      *string    `json:"role" required:"false"`
	Domain    *string    `json:"domain" required:"false"`
	ExpiresAt *time.Time `json:"expiresAt" required:"false"`
}

type PatchAPIKeyBody struct {
	Enabled bool `json:"enabled" required:"false"`
}

type apiKeyDTO struct {
	ID        int64      `json:"id"`
	Name      *string    `json:"name,omitempty"`
	Role      string     `json:"role"`
	Domain    *string    `json:"domain,omitempty"`
	Enabled   bool       `json:"enabled"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	APIKey    string     `json:"apiKey,omitempty"`
}

func newAPIKeyDTO(k *data.APIKeyRow, domainAuthority *string) apiKeyDTO {
	return apiKeyDTO{
		ID:        k.ID.Value(),
		Name:      k.Name,
		Role:      k.Role,
		Domain:    domainAuthority,
		Enabled:   k.Enabled,
		ExpiresAt: k.ExpiresAt,
		CreatedAt: k.CreatedAt,
	}
}

// GET /rest/v1/api-keys (admin)
func (a *App) opListAPIKeys(ctx context.Context, key *AuthenticatedKey, in *Empty) (*DataList[apiKeyDTO], error) {
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("This operation requires an admin API key.")
	}
	keys, err := data.ListAPIKeys(ctx, a.DB)
	if err != nil {
		return nil, err
	}
	domains, err := data.ListDomains(ctx, a.DB)
	if err != nil {
		return nil, err
	}
	authorityOf := func(id *core.DomainID) *string {
		if id == nil {
			return nil
		}
		for _, d := range domains {
			if d.ID == *id {
				authority := d.Authority
				return &authority
			}
		}
		return nil
	}
	dtos := make([]apiKeyDTO, len(keys))
	for i := range keys {
		dtos[i] = newAPIKeyDTO(&keys[i], authorityOf(keys[i].DomainID))
	}
	return result(DataList[apiKeyDTO]{Data: dtos})
}

// POST /rest/v1/api-keys (admin) — the plaintext key is returned exactly
// once.
func (a *App) opCreateAPIKey(ctx context.Context, key *AuthenticatedKey, in *BodyInput[CreateAPIKeyBody]) (*apiKeyDTO, error) {
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("This operation requires an admin API key.")
	}
	body := &in.Body

	var domain *data.DomainRow
	var err error
	if body.Domain != nil {
		domain, err = data.DomainByAuthority(ctx, a.DB, strings.ToLower(strings.TrimSpace(*body.Domain)))
		if err != nil {
			return nil, err
		}
	}

	var role core.APIKeyRole
	roleSlug := "admin"
	if body.Role != nil {
		roleSlug = strings.ToLower(*body.Role)
	}
	switch roleSlug {
	case "admin":
		role = core.AdminRole()
	case "author":
		role = core.AuthorRole()
	case "domain":
		if domain == nil {
			return nil, BadRequest("domain-role keys need an existing 'domain'.")
		}
		role = core.DomainRole(domain.ID)
	default:
		return nil, BadRequest(fmt.Sprintf("Unknown role '%s'. Use admin, author or domain.", roleSlug))
	}

	if body.ExpiresAt != nil && !body.ExpiresAt.After(time.Now().UTC()) {
		return nil, BadRequest("expiresAt must be in the future.")
	}

	plainKey := GenerateAPIKey()
	row, err := data.InsertAPIKey(ctx, a.DB, HashAPIKey(plainKey), body.Name, role, body.ExpiresAt)
	if err != nil {
		return nil, err
	}
	var domainAuthority *string
	if domain != nil {
		domainAuthority = &domain.Authority
	}
	dto := newAPIKeyDTO(row, domainAuthority)
	dto.APIKey = plainKey
	return result(dto)
}

func apiKeyIDFromPath(raw string) (core.APIKeyID, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	return core.APIKeyID(id), err == nil
}

// PATCH /rest/v1/api-keys/{id} (admin)
func (a *App) opPatchAPIKey(ctx context.Context, key *AuthenticatedKey, in *IDBodyInput[PatchAPIKeyBody]) (*IDEnabled, error) {
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("This operation requires an admin API key.")
	}
	body := &in.Body
	id, ok := apiKeyIDFromPath(in.ID)
	if !ok {
		return nil, NotFound("API key was not found.")
	}
	updated, err := data.SetAPIKeyEnabled(ctx, a.DB, id, body.Enabled)
	if err != nil {
		return nil, err
	}
	if !updated {
		return nil, NotFound(fmt.Sprintf("API key %d was not found.", id.Value()))
	}
	return result(IDEnabled{ID: id.Value(), Enabled: body.Enabled})
}

// DELETE /rest/v1/api-keys/{id} (admin)
func (a *App) opDeleteAPIKey(ctx context.Context, key *AuthenticatedKey, in *IDInput) (*Empty, error) {
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("This operation requires an admin API key.")
	}
	id, ok := apiKeyIDFromPath(in.ID)
	if !ok {
		return nil, NotFound("API key was not found.")
	}
	deleted, err := data.DeleteAPIKey(ctx, a.DB, id)
	if err != nil {
		return nil, err
	}
	if !deleted {
		return nil, NotFound(fmt.Sprintf("API key %d was not found.", id.Value()))
	}
	return nil, nil
}
