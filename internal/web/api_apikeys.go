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

type CreateApiKeyBody struct {
	Name      *string    `json:"name"`
	Role      *string    `json:"role"`
	Domain    *string    `json:"domain"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

type PatchApiKeyBody struct {
	Enabled bool `json:"enabled"`
}

type apiKeyDto struct {
	Id        int64      `json:"id"`
	Name      *string    `json:"name,omitempty"`
	Role      string     `json:"role"`
	Domain    *string    `json:"domain,omitempty"`
	Enabled   bool       `json:"enabled"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	ApiKey    string     `json:"apiKey,omitempty"`
}

func newApiKeyDto(k *data.ApiKeyRow, domainAuthority *string) apiKeyDto {
	return apiKeyDto{
		Id:        k.Id,
		Name:      k.Name,
		Role:      k.Role,
		Domain:    domainAuthority,
		Enabled:   k.Enabled,
		ExpiresAt: k.ExpiresAt,
		CreatedAt: k.CreatedAt,
	}
}

// GET /rest/v1/api-keys (admin)
func (a *App) apiListApiKeys(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	keys, err := data.ListApiKeys(a.Db)
	if err != nil {
		return err
	}
	domains, err := data.ListDomains(a.Db)
	if err != nil {
		return err
	}
	authorityOf := func(id *int64) *string {
		if id == nil {
			return nil
		}
		for _, d := range domains {
			if d.Id == *id {
				authority := d.Authority
				return &authority
			}
		}
		return nil
	}
	dtos := make([]apiKeyDto, len(keys))
	for i := range keys {
		dtos[i] = newApiKeyDto(&keys[i], authorityOf(keys[i].DomainId))
	}
	return RespondJSON(w, http.StatusOK, map[string]any{"data": dtos})
}

// POST /rest/v1/api-keys (admin) — the plaintext key is returned exactly
// once.
func (a *App) apiCreateApiKey(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	body, err := ReadJSON[CreateApiKeyBody](w, r)
	if err != nil {
		return BadRequest(err.Error())
	}

	var domain *data.DomainRow
	if body.Domain != nil {
		domain, err = data.DomainByAuthority(a.Db, strings.ToLower(strings.TrimSpace(*body.Domain)))
		if err != nil {
			return err
		}
	}

	var role core.ApiKeyRole
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
		role = core.DomainRole(core.DomainID(domain.Id))
	default:
		return BadRequest(fmt.Sprintf("Unknown role '%s'. Use admin, author or domain.", roleSlug))
	}

	if body.ExpiresAt != nil && !body.ExpiresAt.After(time.Now().UTC()) {
		return BadRequest("expiresAt must be in the future.")
	}

	plainKey := GenerateApiKey()
	row, err := data.InsertApiKey(a.Db, HashApiKey(plainKey), body.Name, role, body.ExpiresAt)
	if err != nil {
		return err
	}
	var domainAuthority *string
	if domain != nil {
		domainAuthority = &domain.Authority
	}
	dto := newApiKeyDto(row, domainAuthority)
	dto.ApiKey = plainKey
	return RespondJSON(w, http.StatusCreated, dto)
}

func apiKeyIdFromPath(r *http.Request) (core.ApiKeyID, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return core.ApiKeyID(id), err == nil
}

// PATCH /rest/v1/api-keys/{id} (admin)
func (a *App) apiPatchApiKey(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	body, err := ReadJSON[PatchApiKeyBody](w, r)
	if err != nil {
		return BadRequest(err.Error())
	}
	id, ok := apiKeyIdFromPath(r)
	if !ok {
		return NotFound("API key was not found.")
	}
	updated, err := data.SetApiKeyEnabled(a.Db, id, body.Enabled)
	if err != nil {
		return err
	}
	if !updated {
		return NotFound(fmt.Sprintf("API key %d was not found.", id.Value()))
	}
	return RespondJSON(w, http.StatusOK, map[string]any{"id": id.Value(), "enabled": body.Enabled})
}

// DELETE /rest/v1/api-keys/{id} (admin)
func (a *App) apiDeleteApiKey(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	id, ok := apiKeyIdFromPath(r)
	if !ok {
		return NotFound("API key was not found.")
	}
	deleted, err := data.DeleteApiKey(a.Db, id)
	if err != nil {
		return err
	}
	if !deleted {
		return NotFound(fmt.Sprintf("API key %d was not found.", id.Value()))
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
