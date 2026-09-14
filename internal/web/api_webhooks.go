package web

import (
	"context"
	"fmt"
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
	return a.createWebhook(ctx, &in.Body)
}

// PATCH /rest/v1/webhooks/{id} (admin)
func (a *App) opPatchWebhook(ctx context.Context, key *AuthenticatedKey, in *idBodyOptions[PatchWebhookBody]) (*IDEnabled, error) {
	if !a.Cfg.WebhooksEnabled {
		return nil, NotFound("Webhooks are disabled.")
	}
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("This operation requires an admin API key.")
	}
	body := &in.Body
	if in.ID == nil {
		return nil, NotFound("Webhook was not found.")
	}
	id := core.WebhookID(*in.ID)
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
func (a *App) opDeleteWebhook(ctx context.Context, key *AuthenticatedKey, in *idOptions) (*Empty, error) {
	if !a.Cfg.WebhooksEnabled {
		return nil, NotFound("Webhooks are disabled.")
	}
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("This operation requires an admin API key.")
	}
	if in.ID == nil {
		return nil, NotFound("Webhook was not found.")
	}
	id := core.WebhookID(*in.ID)
	deleted, err := data.DeleteWebhook(ctx, a.DB, id)
	if err != nil {
		return nil, err
	}
	if !deleted {
		return nil, NotFound(fmt.Sprintf("Webhook %d was not found.", id.Value()))
	}
	return nil, nil
}
