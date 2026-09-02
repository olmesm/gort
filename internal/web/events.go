package web

import (
	"encoding/json"
	"time"

	"github.com/olmesm/gort/internal/core"
)

// VisitedShortUrl is the short URL a visit event refers to.
type VisitedShortUrl struct {
	ShortCode string `json:"shortCode"`
	Domain    string `json:"domain"`
	LongUrl   string `json:"longUrl"`
}

// VisitEventPayload is what a visit event carries to webhook subscribers.
type VisitEventPayload struct {
	VisitType    string           `json:"visitType"`
	ShortUrl     *VisitedShortUrl `json:"shortUrl,omitempty"`
	VisitedUrl   *string          `json:"visitedUrl,omitempty"`
	Referer      *string          `json:"referer,omitempty"`
	UserAgent    *string          `json:"userAgent,omitempty"`
	PotentialBot bool             `json:"potentialBot"`
}

// DomainEvent is an integration event published by the application. Typed
// end-to-end: the event kind and its payload shape can no longer drift apart
// per call site.
type DomainEvent struct {
	kind core.WebhookEvent
	// Exactly one of these is set, matching the kind.
	urlCreated *ShortUrlDto
	visit      *VisitEventPayload
}

func UrlCreatedEvent(dto ShortUrlDto) DomainEvent {
	return DomainEvent{kind: core.EventUrlCreated, urlCreated: &dto}
}

func VisitRecordedEvent(payload VisitEventPayload) DomainEvent {
	return DomainEvent{kind: core.EventVisitRecorded, visit: &payload}
}

func OrphanVisitRecordedEvent(payload VisitEventPayload) DomainEvent {
	return DomainEvent{kind: core.EventOrphanVisitRecorded, visit: &payload}
}

// Kind is the webhook subscription this event maps to.
func (e DomainEvent) Kind() core.WebhookEvent { return e.kind }

type eventEnvelope struct {
	Event      string    `json:"event"`
	OccurredAt time.Time `json:"occurredAt"`
	Data       any       `json:"data"`
}

// ToDeliveryPayload is the signed JSON body delivered to webhook endpoints.
func (e DomainEvent) ToDeliveryPayload(occurredAt time.Time) string {
	var payloadData any
	if e.urlCreated != nil {
		payloadData = e.urlCreated
	} else {
		payloadData = e.visit
	}
	body, _ := json.Marshal(eventEnvelope{
		Event:      e.kind.Slug(),
		OccurredAt: occurredAt,
		Data:       payloadData,
	})
	return string(body)
}
