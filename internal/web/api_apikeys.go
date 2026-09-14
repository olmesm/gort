package web

import (
	"context"
	"fmt"
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
	return a.issueAPIKey(ctx, &in.Body)
}

// PATCH /rest/v1/api-keys/{id} (admin)
func (a *App) opPatchAPIKey(ctx context.Context, key *AuthenticatedKey, in *idBodyOptions[PatchAPIKeyBody]) (*IDEnabled, error) {
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("This operation requires an admin API key.")
	}
	body := &in.Body
	if in.ID == nil {
		return nil, NotFound("API key was not found.")
	}
	id := core.APIKeyID(*in.ID)
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
func (a *App) opDeleteAPIKey(ctx context.Context, key *AuthenticatedKey, in *idOptions) (*Empty, error) {
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("This operation requires an admin API key.")
	}
	if in.ID == nil {
		return nil, NotFound("API key was not found.")
	}
	id := core.APIKeyID(*in.ID)
	deleted, err := data.DeleteAPIKey(ctx, a.DB, id)
	if err != nil {
		return nil, err
	}
	if !deleted {
		return nil, NotFound(fmt.Sprintf("API key %d was not found.", id.Value()))
	}
	return nil, nil
}
