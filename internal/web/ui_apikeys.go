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

func (a *App) apiKeysViewModel(errorMessage, plainKey string) (apiKeysView, error) {
	keys, err := data.ListApiKeys(a.Db)
	if err != nil {
		return apiKeysView{}, err
	}
	domains, err := data.ListDomains(a.Db)
	if err != nil {
		return apiKeysView{}, err
	}
	authorityOf := func(id *int64) string {
		if id == nil {
			return "—"
		}
		for _, d := range domains {
			if d.Id == *id {
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
			Domain:       authorityOf(k.DomainId),
			Expired:      k.ExpiresAt != nil && !k.ExpiresAt.After(time.Now().UTC()),
			Enabled:      k.Enabled,
			Expires:      expires,
			Created:      formatDateTime(k.CreatedAt),
			ToggleAction: fmt.Sprintf("/admin/api-keys/%d/toggle", k.Id),
			DeleteAction: fmt.Sprintf("/admin/api-keys/%d/delete", k.Id),
		})
	}
	return model, nil
}

func (a *App) renderApiKeysPage(w http.ResponseWriter, user *CurrentUser, errorMessage, plainKey string) error {
	model, err := a.apiKeysViewModel(errorMessage, plainKey)
	if err != nil {
		return err
	}
	return a.renderPage(w, http.StatusOK, "apikeys", user, "/admin/api-keys", "API keys", model)
}

// GET /admin/api-keys (admin)
func (a *App) uiListApiKeys(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	return a.renderApiKeysPage(w, user, "", "")
}

// POST /admin/api-keys (admin) — shows the plaintext key once.
func (a *App) uiCreateApiKey(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return BadRequest("Invalid form submission.")
	}
	var name *string
	if n := strings.TrimSpace(r.PostFormValue("name")); n != "" {
		name = &n
	}
	var domain *data.DomainRow
	if authority := r.PostFormValue("domain"); authority != "" {
		var err error
		domain, err = data.DomainByAuthority(a.Db, strings.ToLower(authority))
		if err != nil {
			return err
		}
	}

	var role core.ApiKeyRole
	switch r.PostFormValue("role") {
	case "author":
		role = core.AuthorRole()
	case "domain":
		if domain == nil {
			return a.renderApiKeysPage(w, user, "Domain-role keys need a domain.", "")
		}
		role = core.DomainRole(core.DomainID(domain.Id))
	default:
		role = core.AdminRole()
	}

	var expiresAt *time.Time
	if v := r.PostFormValue("expiresAt"); v != "" {
		expiresAt = TryParseDate(v)
	}

	plainKey := GenerateApiKey()
	if _, err := data.InsertApiKey(a.Db, HashApiKey(plainKey), name, role, expiresAt); err != nil {
		return err
	}
	return a.renderApiKeysPage(w, user, "", plainKey)
}

// POST /admin/api-keys/{id}/toggle (admin)
func (a *App) uiToggleApiKey(_ *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	if id, err := strconv.ParseInt(r.PathValue("id"), 10, 64); err == nil {
		key, err := data.ApiKeyByID(a.Db, core.ApiKeyID(id))
		if err != nil {
			return err
		}
		if key != nil {
			if _, err := data.SetApiKeyEnabled(a.Db, core.ApiKeyID(id), !key.Enabled); err != nil {
				return err
			}
		}
	}
	return redirect(w, r, "/admin/api-keys")
}

// POST /admin/api-keys/{id}/delete (admin)
func (a *App) uiDeleteApiKey(_ *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	if id, err := strconv.ParseInt(r.PathValue("id"), 10, 64); err == nil {
		if _, err := data.DeleteApiKey(a.Db, core.ApiKeyID(id)); err != nil {
			return err
		}
	}
	return redirect(w, r, "/admin/api-keys")
}
