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
	Url    string   `json:"url"`
	Events []string `json:"events"`
}

type PatchWebhookBody struct {
	Enabled bool `json:"enabled"`
}

type webhookDto struct {
	Id        int64     `json:"id"`
	Name      string    `json:"name"`
	Url       string    `json:"url"`
	Events    []string  `json:"events"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	Secret    string    `json:"secret,omitempty"`
}

func newWebhookDto(w *data.WebhookRow) webhookDto {
	return webhookDto{
		Id:        w.Id,
		Name:      w.Name,
		Url:       w.Url,
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

func isHttpUrl(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.IsAbs() && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func parseWebhookBody(body *CreateWebhookBody) (name, hookUrl string, events []core.WebhookEvent, err error) {
	if strings.TrimSpace(body.Name) == "" {
		return "", "", nil, fmt.Errorf("name is required.")
	}
	if !isHttpUrl(body.Url) {
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
	return strings.TrimSpace(body.Name), body.Url, events, nil
}

func generateWebhookSecret() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}

// GET /rest/v1/webhooks (admin)
func (a *App) apiListWebhooks(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
	hooks, err := data.ListWebhooks(a.Db)
	if err != nil {
		a.serverError(w, err)
		return
	}
	dtos := make([]webhookDto, len(hooks))
	for i := range hooks {
		dtos[i] = newWebhookDto(&hooks[i])
	}
	RespondJSON(w, http.StatusOK, map[string]any{"data": dtos})
}

// POST /rest/v1/webhooks (admin) — the signing secret is returned exactly
// once.
func (a *App) apiCreateWebhook(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
	body, err := ReadJSON[CreateWebhookBody](w, r)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	name, hookUrl, events, err := parseWebhookBody(body)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	secret := generateWebhookSecret()
	row, err := data.InsertWebhook(a.Db, name, hookUrl, secret, events)
	if err != nil {
		a.serverError(w, err)
		return
	}
	dto := newWebhookDto(row)
	dto.Secret = secret
	RespondJSON(w, http.StatusCreated, dto)
}

func webhookIdFromPath(r *http.Request) (core.WebhookID, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return core.WebhookID(id), err == nil
}

// PATCH /rest/v1/webhooks/{id} (admin)
func (a *App) apiPatchWebhook(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
	body, err := ReadJSON[PatchWebhookBody](w, r)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	id, ok := webhookIdFromPath(r)
	if !ok {
		NotFound(w, "Webhook was not found.")
		return
	}
	updated, err := data.SetWebhookEnabled(a.Db, id, body.Enabled)
	if err != nil {
		a.serverError(w, err)
		return
	}
	if !updated {
		NotFound(w, fmt.Sprintf("Webhook %d was not found.", id.Value()))
		return
	}
	RespondJSON(w, http.StatusOK, map[string]any{"id": id.Value(), "enabled": body.Enabled})
}

// DELETE /rest/v1/webhooks/{id} (admin)
func (a *App) apiDeleteWebhook(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) {
	id, ok := webhookIdFromPath(r)
	if !ok {
		NotFound(w, "Webhook was not found.")
		return
	}
	deleted, err := data.DeleteWebhook(a.Db, id)
	if err != nil {
		a.serverError(w, err)
		return
	}
	if !deleted {
		NotFound(w, fmt.Sprintf("Webhook %d was not found.", id.Value()))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
