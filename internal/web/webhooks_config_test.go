package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

func TestWebhooksConfig(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{{"", false}, {"false", false}, {"true", true}, {"invalid", false}} {
		t.Run(tc.value, func(t *testing.T) {
			cfg, err := ConfigFromLookup(func(name string) (string, bool) { return tc.value, name == "WEBHOOKS_ENABLED" && tc.value != "" })
			if err != nil {
				t.Fatal(err)
			}
			if cfg.WebhooksEnabled != tc.want {
				t.Fatalf("enabled = %v, want %v", cfg.WebhooksEnabled, tc.want)
			}
		})
	}
}

func TestWebhooksDisabledByDefault(t *testing.T) {
	app := newTestApp(t)
	client := app.adminClient(t)
	for _, path := range []string{"/rest/v1/webhooks", "/rest/v1/webhooks/1", "/admin/webhooks", "/admin/webhooks/1/toggle"} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete} {
			if response := client.do(method, path, ""); response.Code != http.StatusNotFound {
				t.Errorf("%s %s = %d", method, path, response.Code)
			}
		}
	}
	admin, err := data.UserByUsername(t.Context(), app.DB, "admin")
	if err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRecorder()
	app.SignIn(login, admin)
	client.headers["Cookie"] = login.Result().Cookies()[0].String()
	response := client.get("/admin/domains")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `href="/admin/webhooks"`) {
		t.Fatal("disabled webhooks must be hidden in navigation")
	}

	hook, err := data.InsertWebhook(t.Context(), app.DB, "stored", "http://127.0.0.1:1", "secret", []core.WebhookEvent{core.EventURLCreated})
	if err != nil {
		t.Fatal(err)
	}
	if err := data.EnqueueDelivery(t.Context(), app.DB, hook.ID, core.EventURLCreated, `{"saved":true}`); err != nil {
		t.Fatal(err)
	}
	app.Queues.PublishEvent(URLCreatedEvent(ShortURLDTO{}))
	if len(app.Queues.Events) != 0 {
		t.Fatal("disabled webhooks buffered an event")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	for _, worker := range []func(context.Context){app.eventWorker, app.webhookWorker} {
		done := make(chan struct{})
		go func() { worker(ctx); close(done) }()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("disabled webhook worker did not exit")
		}
	}
	due, err := data.DueDeliveries(t.Context(), app.DB, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].Delivery.Attempts != 0 || due[0].Delivery.Payload != `{"saved":true}` {
		t.Fatal("disabled worker modified stored delivery")
	}
}

func TestWebhooksOptInDeliversQueuedAndNewEvents(t *testing.T) {
	app := newTestAppWithConfig(t, map[string]string{"WEBHOOKS_ENABLED": "true"})
	received := make(chan string, 2)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-Gort-Signature") != signWebhookPayload("secret", string(body)) {
			t.Error("signature missing or incorrect")
		}
		received <- string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer endpoint.Close()
	hook, err := data.InsertWebhook(t.Context(), app.DB, "enabled", endpoint.URL, "secret", []core.WebhookEvent{core.EventURLCreated})
	if err != nil {
		t.Fatal(err)
	}
	if err := data.EnqueueDelivery(t.Context(), app.DB, hook.ID, core.EventURLCreated, `{"saved":true}`); err != nil {
		t.Fatal(err)
	}
	client := app.adminClient(t)
	if response := client.get("/rest/v1/webhooks"); response.Code != http.StatusOK {
		t.Fatalf("enabled endpoint = %d", response.Code)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { app.eventWorker(ctx); close(done) }()
	deliveryDone := make(chan struct{})
	go func() { app.webhookWorker(ctx); close(deliveryDone) }()
	defer func() { cancel(); <-done; <-deliveryDone }()
	app.Queues.PublishEvent(URLCreatedEvent(ShortURLDTO{}))
	bodies := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case body := <-received:
			bodies[body] = true
		case <-time.After(2 * time.Second):
			t.Fatal("enabled workers did not deliver queued and new events")
		}
	}
	if !bodies[`{"saved":true}`] {
		t.Fatal("saved delivery did not resume")
	}
}
