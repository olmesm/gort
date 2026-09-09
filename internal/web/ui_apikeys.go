package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

type apiKeysView struct {
	Error    string
	PlainKey string
	Rows     []apiKeyRowView
	Domains  []string
}

type apiKeyRowView struct {
	Name         string
	Role         string
	Domain       string
	Expired      bool
	Enabled      bool
	Expires      string
	Created      string
	ToggleAction string
	DeleteAction string
}

func (a *App) apiKeysViewModel(ctx context.Context, errorMessage, plainKey string) (apiKeysView, error) {
	keys, err := data.ListAPIKeys(ctx, a.DB)
	if err != nil {
		return apiKeysView{}, err
	}
	domains, err := data.ListDomains(ctx, a.DB)
	if err != nil {
		return apiKeysView{}, err
	}
	authorityOf := func(id *core.DomainID) string {
		if id == nil {
			return "—"
		}
		for _, d := range domains {
			if d.ID == *id {
				return d.Authority
			}
		}
		return "—"
	}

	model := apiKeysView{Error: errorMessage, PlainKey: plainKey}
	for _, d := range domains {
		model.Domains = append(model.Domains, d.Authority)
	}
	for _, k := range keys {
		expires := "never"
		if k.ExpiresAt != nil {
			expires = formatDateTime(*k.ExpiresAt)
		}
		model.Rows = append(model.Rows, apiKeyRowView{
			Name:         orDash(k.Name),
			Role:         k.Role,
			Domain:       authorityOf(k.DomainID),
			Expired:      k.ExpiresAt != nil && !k.ExpiresAt.After(time.Now().UTC()),
			Enabled:      k.Enabled,
			Expires:      expires,
			Created:      formatDateTime(k.CreatedAt),
			ToggleAction: fmt.Sprintf("/admin/api-keys/%d/toggle", k.ID),
			DeleteAction: fmt.Sprintf("/admin/api-keys/%d/delete", k.ID),
		})
	}
	return model, nil
}

func (a *App) renderAPIKeysPage(ctx context.Context, w http.ResponseWriter, user *CurrentUser, errorMessage, plainKey string) error {
	model, err := a.apiKeysViewModel(ctx, errorMessage, plainKey)
	if err != nil {
		return err
	}
	return a.renderPage(w, http.StatusOK, "apikeys", user, "/admin/api-keys", "API keys", model)
}

// GET /admin/api-keys (admin)
func (a *App) uiListAPIKeys(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	return a.renderAPIKeysPage(r.Context(), w, user, "", "")
}

// POST /admin/api-keys (admin) — shows the plaintext key once.
func (a *App) uiCreateAPIKey(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	var name *string
	if n := strings.TrimSpace(r.PostFormValue("name")); n != "" {
		name = &n
	}
	var domain *data.DomainRow
	if authority := r.PostFormValue("domain"); authority != "" {
		var err error
		domain, err = data.DomainByAuthority(r.Context(), a.DB, strings.ToLower(authority))
		if err != nil {
			return err
		}
	}

	var role core.APIKeyRole
	switch r.PostFormValue("role") {
	case "author":
		role = core.AuthorRole()
	case "domain":
		if domain == nil {
			return a.renderAPIKeysPage(r.Context(), w, user, "Domain-role keys need a domain.", "")
		}
		role = core.DomainRole(domain.ID)
	default:
		role = core.AdminRole()
	}

	var expiresAt *time.Time
	if v := r.PostFormValue("expiresAt"); v != "" {
		expiresAt = TryParseDate(v)
	}

	plainKey := GenerateAPIKey()
	if _, err := data.InsertAPIKey(r.Context(), a.DB, HashAPIKey(plainKey), name, role, expiresAt); err != nil {
		return err
	}
	return a.renderAPIKeysPage(r.Context(), w, user, "", plainKey)
}

// POST /admin/api-keys/{id}/toggle (admin)
func (a *App) uiToggleAPIKey(_ *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	id, err := pathID[core.APIKeyID](r, "id")
	if err != nil {
		return err
	}
	key, err := data.APIKeyByID(r.Context(), a.DB, id)
	if err != nil {
		return err
	}
	if key != nil {
		if _, err := data.SetAPIKeyEnabled(r.Context(), a.DB, id, !key.Enabled); err != nil {
			return err
		}
	}
	return redirect(w, r, "/admin/api-keys")
}

// POST /admin/api-keys/{id}/delete (admin)
func (a *App) uiDeleteAPIKey(_ *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	id, err := pathID[core.APIKeyID](r, "id")
	if err != nil {
		return err
	}
	if _, err := data.DeleteAPIKey(r.Context(), a.DB, id); err != nil {
		return err
	}
	return redirect(w, r, "/admin/api-keys")
}
