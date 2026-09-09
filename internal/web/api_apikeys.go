package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

type CreateAPIKeyBody struct {
	Name      *string    `json:"name"`
	Role      *string    `json:"role"`
	Domain    *string    `json:"domain"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

type PatchAPIKeyBody struct {
	Enabled bool `json:"enabled"`
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
func (a *App) apiListAPIKeys(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	keys, err := data.ListAPIKeys(r.Context(), a.DB)
	if err != nil {
		return err
	}
	domains, err := data.ListDomains(r.Context(), a.DB)
	if err != nil {
		return err
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
	return RespondJSON(w, http.StatusOK, map[string]any{"data": dtos})
}

// POST /rest/v1/api-keys (admin) — the plaintext key is returned exactly
// once.
func (a *App) apiCreateAPIKey(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	body, err := ReadJSON[CreateAPIKeyBody](w, r)
	if err != nil {
		return BadRequest(err.Error())
	}

	var domain *data.DomainRow
	if body.Domain != nil {
		domain, err = data.DomainByAuthority(r.Context(), a.DB, strings.ToLower(strings.TrimSpace(*body.Domain)))
		if err != nil {
			return err
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
			return BadRequest("domain-role keys need an existing 'domain'.")
		}
		role = core.DomainRole(domain.ID)
	default:
		return BadRequest(fmt.Sprintf("Unknown role '%s'. Use admin, author or domain.", roleSlug))
	}

	if body.ExpiresAt != nil && !body.ExpiresAt.After(time.Now().UTC()) {
		return BadRequest("expiresAt must be in the future.")
	}

	plainKey := GenerateAPIKey()
	row, err := data.InsertAPIKey(r.Context(), a.DB, HashAPIKey(plainKey), body.Name, role, body.ExpiresAt)
	if err != nil {
		return err
	}
	var domainAuthority *string
	if domain != nil {
		domainAuthority = &domain.Authority
	}
	dto := newAPIKeyDTO(row, domainAuthority)
	dto.APIKey = plainKey
	return RespondJSON(w, http.StatusCreated, dto)
}

func apiKeyIDFromPath(r *http.Request) (core.APIKeyID, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return core.APIKeyID(id), err == nil
}

// PATCH /rest/v1/api-keys/{id} (admin)
func (a *App) apiPatchAPIKey(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	body, err := ReadJSON[PatchAPIKeyBody](w, r)
	if err != nil {
		return BadRequest(err.Error())
	}
	id, ok := apiKeyIDFromPath(r)
	if !ok {
		return NotFound("API key was not found.")
	}
	updated, err := data.SetAPIKeyEnabled(r.Context(), a.DB, id, body.Enabled)
	if err != nil {
		return err
	}
	if !updated {
		return NotFound(fmt.Sprintf("API key %d was not found.", id.Value()))
	}
	return RespondJSON(w, http.StatusOK, map[string]any{"id": id.Value(), "enabled": body.Enabled})
}

// DELETE /rest/v1/api-keys/{id} (admin)
func (a *App) apiDeleteAPIKey(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	id, ok := apiKeyIDFromPath(r)
	if !ok {
		return NotFound("API key was not found.")
	}
	deleted, err := data.DeleteAPIKey(r.Context(), a.DB, id)
	if err != nil {
		return err
	}
	if !deleted {
		return NotFound(fmt.Sprintf("API key %d was not found.", id.Value()))
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
