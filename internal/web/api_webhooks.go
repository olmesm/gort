package web

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

type CreateWebhookBody struct {
	Name   string   `json:"name"`
	URL    string   `json:"url"`
	Events []string `json:"events"`
}

type PatchWebhookBody struct {
	Enabled bool `json:"enabled"`
}

type webhookDTO struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	Secret    string    `json:"secret,omitempty"`
}

func newWebhookDTO(w *data.WebhookRow) webhookDTO {
	return webhookDTO{
		ID:        w.ID.Value(),
		Name:      w.Name,
		URL:       w.URL,
		Events:    strings.Split(w.Events, ","),
		Enabled:   w.Enabled,
		CreatedAt: w.CreatedAt,
	}
}

func allEventSlugs() string {
	slugs := make([]string, len(core.AllWebhookEvents))
	for i, e := range core.AllWebhookEvents {
		slugs[i] = e.Slug()
	}
	return strings.Join(slugs, ", ")
}

func isHTTPURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.IsAbs() && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func parseWebhookBody(body *CreateWebhookBody) (name, hookURL string, events []core.WebhookEvent, err error) {
	if strings.TrimSpace(body.Name) == "" {
		return "", "", nil, fmt.Errorf("name is required.")
	}
	if !isHTTPURL(body.URL) {
		return "", "", nil, fmt.Errorf("url must be an absolute http(s) URL.")
	}
	if len(body.Events) == 0 {
		return "", "", nil, fmt.Errorf("Subscribe to at least one event: %s.", allEventSlugs())
	}
	for _, slug := range body.Events {
		event, ok := core.WebhookEventOfSlug(slug)
		if !ok {
			return "", "", nil, fmt.Errorf("Unknown event '%s'. Valid events: %s.", slug, allEventSlugs())
		}
		events = append(events, event)
	}
	return strings.TrimSpace(body.Name), body.URL, events, nil
}

func generateWebhookSecret() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}

// GET /rest/v1/webhooks (admin)
func (a *App) apiListWebhooks(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	hooks, err := data.ListWebhooks(r.Context(), a.DB)
	if err != nil {
		return err
	}
	dtos := make([]webhookDTO, len(hooks))
	for i := range hooks {
		dtos[i] = newWebhookDTO(&hooks[i])
	}
	return RespondJSON(w, http.StatusOK, map[string]any{"data": dtos})
}

// POST /rest/v1/webhooks (admin) — the signing secret is returned exactly
// once.
func (a *App) apiCreateWebhook(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	body, err := ReadJSON[CreateWebhookBody](w, r)
	if err != nil {
		return BadRequest(err.Error())
	}
	name, hookURL, events, err := parseWebhookBody(body)
	if err != nil {
		return BadRequest(err.Error())
	}
	secret := generateWebhookSecret()
	row, err := data.InsertWebhook(r.Context(), a.DB, name, hookURL, secret, events)
	if err != nil {
		return err
	}
	dto := newWebhookDTO(row)
	dto.Secret = secret
	return RespondJSON(w, http.StatusCreated, dto)
}

func webhookIDFromPath(r *http.Request) (core.WebhookID, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return core.WebhookID(id), err == nil
}

// PATCH /rest/v1/webhooks/{id} (admin)
func (a *App) apiPatchWebhook(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	body, err := ReadJSON[PatchWebhookBody](w, r)
	if err != nil {
		return BadRequest(err.Error())
	}
	id, ok := webhookIDFromPath(r)
	if !ok {
		return NotFound("Webhook was not found.")
	}
	updated, err := data.SetWebhookEnabled(r.Context(), a.DB, id, body.Enabled)
	if err != nil {
		return err
	}
	if !updated {
		return NotFound(fmt.Sprintf("Webhook %d was not found.", id.Value()))
	}
	return RespondJSON(w, http.StatusOK, map[string]any{"id": id.Value(), "enabled": body.Enabled})
}

// DELETE /rest/v1/webhooks/{id} (admin)
func (a *App) apiDeleteWebhook(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	id, ok := webhookIDFromPath(r)
	if !ok {
		return NotFound("Webhook was not found.")
	}
	deleted, err := data.DeleteWebhook(r.Context(), a.DB, id)
	if err != nil {
		return err
	}
	if !deleted {
		return NotFound(fmt.Sprintf("Webhook %d was not found.", id.Value()))
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
