package data

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
)

const webhookSelectCols = "id, name, url, secret, events, enabled, created_at"

func scanWebhookRow(r rowScanner) (*WebhookRow, error) {
	var w WebhookRow
	var createdAt NullTime
	if err := r.Scan(&w.Id, &w.Name, &w.Url, &w.Secret, &w.Events, &w.Enabled, &createdAt); err != nil {
		return nil, err
	}
	w.CreatedAt = createdAt.Time
	return &w, nil
}

func InsertWebhook(db *Db, name, url, secret string, events []core.WebhookEvent) (*WebhookRow, error) {
	slugs := make([]string, len(events))
	for i, e := range events {
		slugs[i] = e.Slug()
	}
	var id int64
	err := db.QueryRow(
		`INSERT INTO webhooks (name, url, secret, events, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 RETURNING id`,
		name, url, secret, strings.Join(slugs, ","), true, db.BindTime(time.Now())).Scan(&id)
	if err != nil {
		return nil, err
	}
	return scanWebhookRow(db.QueryRow(
		fmt.Sprintf("SELECT %s FROM webhooks WHERE id = ?", webhookSelectCols), id))
}

func ListWebhooks(db *Db) ([]WebhookRow, error) {
	return queryAll(db, scanWebhookRow,
		fmt.Sprintf("SELECT %s FROM webhooks ORDER BY name", webhookSelectCols))
}

// ListWebhooksForEvent lists enabled webhooks subscribed to a given event.
func ListWebhooksForEvent(db *Db, event core.WebhookEvent) ([]WebhookRow, error) {
	enabled, err := queryAll(db, scanWebhookRow,
		fmt.Sprintf("SELECT %s FROM webhooks WHERE enabled = %s", webhookSelectCols, db.BoolLiteral(true)))
	if err != nil {
		return nil, err
	}
	var out []WebhookRow
	for _, w := range enabled {
		for _, e := range strings.Split(w.Events, ",") {
			if strings.TrimSpace(e) == event.Slug() {
				out = append(out, w)
				break
			}
		}
	}
	return out, nil
}

func SetWebhookEnabled(db *Db, id core.WebhookID, enabled bool) (bool, error) {
	return execAffected(db, "UPDATE webhooks SET enabled = ? WHERE id = ?", enabled, id.Value())
}

func DeleteWebhook(db *Db, id core.WebhookID) (bool, error) {
	return execAffected(db, "DELETE FROM webhooks WHERE id = ?", id.Value())
}

// ---- Delivery queue ----

func EnqueueDelivery(db *Db, webhookId core.WebhookID, event core.WebhookEvent, payload string) error {
	now := db.BindTime(time.Now())
	_, err := db.Exec(
		`INSERT INTO webhook_deliveries (webhook_id, event, payload, attempts, next_attempt_at, status, created_at)
		 VALUES (?, ?, ?, 0, ?, 'pending', ?)`,
		webhookId.Value(), event.Slug(), payload, now, now)
	return err
}

type DueDelivery struct {
	Delivery WebhookDeliveryRow
	Webhook  WebhookRow
}

// DueDeliveries returns deliveries due for an attempt, joined with their
// webhook config.
func DueDeliveries(db *Db, limit int) ([]DueDelivery, error) {
	return queryAll(db, func(r rowScanner) (*DueDelivery, error) {
		var d WebhookDeliveryRow
		var w WebhookRow
		var lastError sql.NullString
		var nextAttemptAt, dCreatedAt, wCreatedAt NullTime
		err := r.Scan(&d.Id, &d.WebhookId, &d.Event, &d.Payload, &d.Attempts, &nextAttemptAt,
			&d.Status, &lastError, &dCreatedAt,
			&w.Id, &w.Name, &w.Url, &w.Secret, &w.Events, &w.Enabled, &wCreatedAt)
		if err != nil {
			return nil, err
		}
		d.NextAttemptAt = nextAttemptAt.Time
		d.LastError = strPtr(lastError)
		d.CreatedAt = dCreatedAt.Time
		w.CreatedAt = wCreatedAt.Time
		return &DueDelivery{Delivery: d, Webhook: w}, nil
	}, `SELECT wd.id, wd.webhook_id, wd.event, wd.payload, wd.attempts, wd.next_attempt_at,
	           wd.status, wd.last_error, wd.created_at,
	           w.id, w.name, w.url, w.secret, w.events, w.enabled, w.created_at
	    FROM webhook_deliveries wd
	    JOIN webhooks w ON w.id = wd.webhook_id
	    WHERE wd.status = 'pending' AND wd.next_attempt_at <= ?
	    ORDER BY wd.next_attempt_at LIMIT ?`,
		db.BindTime(time.Now()), limit)
}

func MarkDelivered(db *Db, deliveryId int64) error {
	_, err := db.Exec("UPDATE webhook_deliveries SET status = 'delivered' WHERE id = ?", deliveryId)
	return err
}

// MarkFailedAttempt records a failed attempt; retries with exponential
// backoff, giving up after maxAttempts.
func MarkFailedAttempt(db *Db, deliveryId int64, attempts, maxAttempts int, errorMessage string) error {
	newAttempts := attempts + 1
	if newAttempts >= maxAttempts {
		_, err := db.Exec(
			`UPDATE webhook_deliveries SET status = 'failed', attempts = ?, last_error = ?
			 WHERE id = ?`,
			newAttempts, errorMessage, deliveryId)
		return err
	}
	delay := time.Duration(math.Pow(2, float64(newAttempts))*15) * time.Second
	_, err := db.Exec(
		`UPDATE webhook_deliveries SET attempts = ?, last_error = ?, next_attempt_at = ?
		 WHERE id = ?`,
		newAttempts, errorMessage, db.BindTime(time.Now().Add(delay)), deliveryId)
	return err
}
