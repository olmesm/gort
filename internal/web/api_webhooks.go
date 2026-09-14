package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
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
	Enabled bool `json:"enabled" required:"false"`
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
func (a *App) opListWebhooks(ctx context.Context, key *AuthenticatedKey, in *Empty) (*DataList[webhookDTO], error) {
	if !a.Cfg.WebhooksEnabled {
		return nil, NotFound("Webhooks are disabled.")
	}
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("This operation requires an admin API key.")
	}
	hooks, err := data.ListWebhooks(ctx, a.DB)
	if err != nil {
		return nil, err
	}
	dtos := make([]webhookDTO, len(hooks))
	for i := range hooks {
		dtos[i] = newWebhookDTO(&hooks[i])
	}
	return result(DataList[webhookDTO]{Data: dtos})
}

// POST /rest/v1/webhooks (admin) — the signing secret is returned exactly
// once.
func (a *App) opCreateWebhook(ctx context.Context, key *AuthenticatedKey, in *BodyInput[CreateWebhookBody]) (*webhookDTO, error) {
	if !a.Cfg.WebhooksEnabled {
		return nil, NotFound("Webhooks are disabled.")
	}
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("This operation requires an admin API key.")
	}
	body := &in.Body
	name, hookURL, events, err := parseWebhookBody(body)
	if err != nil {
		return nil, BadRequest(err.Error())
	}
	secret := generateWebhookSecret()
	row, err := data.InsertWebhook(ctx, a.DB, name, hookURL, secret, events)
	if err != nil {
		return nil, err
	}
	dto := newWebhookDTO(row)
	dto.Secret = secret
	return result(dto)
}

func webhookIDFromPath(raw string) (core.WebhookID, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	return core.WebhookID(id), err == nil
}

// PATCH /rest/v1/webhooks/{id} (admin)
func (a *App) opPatchWebhook(ctx context.Context, key *AuthenticatedKey, in *IDBodyInput[PatchWebhookBody]) (*IDEnabled, error) {
	if !a.Cfg.WebhooksEnabled {
		return nil, NotFound("Webhooks are disabled.")
	}
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("This operation requires an admin API key.")
	}
	body := &in.Body
	id, ok := webhookIDFromPath(in.ID)
	if !ok {
		return nil, NotFound("Webhook was not found.")
	}
	updated, err := data.SetWebhookEnabled(ctx, a.DB, id, body.Enabled)
	if err != nil {
		return nil, err
	}
	if !updated {
		return nil, NotFound(fmt.Sprintf("Webhook %d was not found.", id.Value()))
	}
	return result(IDEnabled{ID: id.Value(), Enabled: body.Enabled})
}

// DELETE /rest/v1/webhooks/{id} (admin)
func (a *App) opDeleteWebhook(ctx context.Context, key *AuthenticatedKey, in *IDInput) (*Empty, error) {
	if !a.Cfg.WebhooksEnabled {
		return nil, NotFound("Webhooks are disabled.")
	}
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("This operation requires an admin API key.")
	}
	id, ok := webhookIDFromPath(in.ID)
	if !ok {
		return nil, NotFound("Webhook was not found.")
	}
	deleted, err := data.DeleteWebhook(ctx, a.DB, id)
	if err != nil {
		return nil, err
	}
	if !deleted {
		return nil, NotFound(fmt.Sprintf("Webhook %d was not found.", id.Value()))
	}
	return nil, nil
}
