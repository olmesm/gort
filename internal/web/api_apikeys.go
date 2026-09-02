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
func (a *App) apiListApiKeys(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
	keys, err := data.ListApiKeys(a.Db)
	if err != nil {
		a.serverError(w, err)
		return
	}
	domains, err := data.ListDomains(a.Db)
	if err != nil {
		a.serverError(w, err)
		return
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
	RespondJSON(w, http.StatusOK, map[string]any{"data": dtos})
}

// POST /rest/v1/api-keys (admin) — the plaintext key is returned exactly
// once.
func (a *App) apiCreateApiKey(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
	body, err := ReadJSON[CreateApiKeyBody](r)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	var domain *data.DomainRow
	if body.Domain != nil {
		domain, err = data.TryGetDomainByAuthority(a.Db, strings.ToLower(strings.TrimSpace(*body.Domain)))
		if err != nil {
			a.serverError(w, err)
			return
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
			BadRequest(w, "domain-role keys need an existing 'domain'.")
			return
		}
		role = core.DomainRole(core.DomainID(domain.Id))
	default:
		BadRequest(w, fmt.Sprintf("Unknown role '%s'. Use admin, author or domain.", roleSlug))
		return
	}

	if body.ExpiresAt != nil && !body.ExpiresAt.After(time.Now().UTC()) {
		BadRequest(w, "expiresAt must be in the future.")
		return
	}

	plainKey := GenerateApiKey()
	row, err := data.InsertApiKey(a.Db, HashApiKey(plainKey), body.Name, role, body.ExpiresAt)
	if err != nil {
		a.serverError(w, err)
		return
	}
	var domainAuthority *string
	if domain != nil {
		domainAuthority = &domain.Authority
	}
	dto := newApiKeyDto(row, domainAuthority)
	dto.ApiKey = plainKey
	RespondJSON(w, http.StatusCreated, dto)
}

func apiKeyIdFromPath(r *http.Request) (core.ApiKeyID, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return core.ApiKeyID(id), err == nil
}

// PATCH /rest/v1/api-keys/{id} (admin)
func (a *App) apiPatchApiKey(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
	body, err := ReadJSON[PatchApiKeyBody](r)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	id, ok := apiKeyIdFromPath(r)
	if !ok {
		NotFound(w, "API key was not found.")
		return
	}
	updated, err := data.SetApiKeyEnabled(a.Db, id, body.Enabled)
	if err != nil {
		a.serverError(w, err)
		return
	}
	if !updated {
		NotFound(w, fmt.Sprintf("API key %d was not found.", id.Value()))
		return
	}
	RespondJSON(w, http.StatusOK, map[string]any{"id": id.Value(), "enabled": body.Enabled})
}

// DELETE /rest/v1/api-keys/{id} (admin)
func (a *App) apiDeleteApiKey(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
	id, ok := apiKeyIdFromPath(r)
	if !ok {
		NotFound(w, "API key was not found.")
		return
	}
	deleted, err := data.DeleteApiKey(a.Db, id)
	if err != nil {
		a.serverError(w, err)
		return
	}
	if !deleted {
		NotFound(w, fmt.Sprintf("API key %d was not found.", id.Value()))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
