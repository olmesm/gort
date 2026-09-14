package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

// issueAPIKey validates and stores a key, returning the plaintext only here.
// Callers enforce dashboard or API-key authorization before issuing it.
func (a *App) issueAPIKey(ctx context.Context, body *CreateAPIKeyBody) (*apiKeyDTO, error) {
	var domain *data.DomainRow
	var err error
	if body.Domain != nil {
		domain, err = data.DomainByAuthority(ctx, a.DB, strings.ToLower(strings.TrimSpace(*body.Domain)))
		if err != nil {
			return nil, err
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
			return nil, BadRequest("domain-role keys need an existing 'domain'.")
		}
		role = core.DomainRole(domain.ID)
	default:
		return nil, BadRequest(fmt.Sprintf("Unknown role '%s'. Use admin, author or domain.", roleSlug))
	}

	if body.ExpiresAt != nil && !body.ExpiresAt.After(time.Now().UTC()) {
		return nil, BadRequest("expiresAt must be in the future.")
	}

	plainKey := GenerateAPIKey()
	row, err := data.InsertAPIKey(ctx, a.DB, HashAPIKey(plainKey), body.Name, role, body.ExpiresAt)
	if err != nil {
		return nil, err
	}
	var domainAuthority *string
	if domain != nil {
		domainAuthority = &domain.Authority
	}
	dto := newAPIKeyDTO(row, domainAuthority)
	dto.APIKey = plainKey
	return result(dto)
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

func generateWebhookSecret() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}

// createWebhook validates the subscription and returns its new signing secret once.
func (a *App) createWebhook(ctx context.Context, body *CreateWebhookBody) (*webhookDTO, error) {
	if strings.TrimSpace(body.Name) == "" {
		return nil, BadRequest("name is required.")
	}
	if !isHTTPURL(body.URL) {
		return nil, BadRequest("url must be an absolute http(s) URL.")
	}
	if len(body.Events) == 0 {
		return nil, BadRequest(fmt.Sprintf("Subscribe to at least one event: %s.", allEventSlugs()))
	}
	var events []core.WebhookEvent
	for _, slug := range body.Events {
		event, ok := core.WebhookEventOfSlug(slug)
		if !ok {
			return nil, BadRequest(fmt.Sprintf("Unknown event '%s'. Valid events: %s.", slug, allEventSlugs()))
		}
		events = append(events, event)
	}
	secret := generateWebhookSecret()
	row, err := data.InsertWebhook(ctx, a.DB, strings.TrimSpace(body.Name), body.URL, secret, events)
	if err != nil {
		return nil, err
	}
	dto := newWebhookDTO(row)
	dto.Secret = secret
	return result(dto)
}
