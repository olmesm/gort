package web

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/geoip2-golang"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

// GeoIpService is a thread-safe holder around the MaxMind reader; reloadable
// when the database file is refreshed.
type GeoIpService struct {
	cfg    *AppConfig
	logger *slog.Logger
	mu     sync.RWMutex
	reader *geoip2.Reader
}

func NewGeoIpService(cfg *AppConfig, logger *slog.Logger) *GeoIpService {
	return &GeoIpService{cfg: cfg, logger: logger}
}

func (g *GeoIpService) Reload() {
	path := g.cfg.GeoDbPath()
	if _, err := os.Stat(path); err != nil {
		return
	}
	newReader, err := geoip2.Open(path)
	if err != nil {
		g.logger.Warn("Failed to load GeoIP database", "path", path, "error", err)
		return
	}
	g.mu.Lock()
	if g.reader != nil {
		_ = g.reader.Close()
	}
	g.reader = newReader
	g.mu.Unlock()
	g.logger.Info("GeoIP database loaded", "path", path)
}

func (g *GeoIpService) IsAvailable() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.reader != nil
}

func (g *GeoIpService) TryLookup(ip string) *data.GeoInfo {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.reader == nil {
		return nil
	}
	addr := net.ParseIP(ip)
	if addr == nil {
		return nil
	}
	city, err := g.reader.City(addr)
	if err != nil {
		return nil
	}
	info := &data.GeoInfo{}
	if city.Country.IsoCode != "" {
		v := city.Country.IsoCode
		info.CountryCode = &v
	}
	if name := city.Country.Names["en"]; name != "" {
		v := name
		info.CountryName = &v
	}
	if name := city.City.Names["en"]; name != "" {
		v := name
		info.City = &v
	}
	if city.Location.Latitude != 0 || city.Location.Longitude != 0 {
		lat, lon := city.Location.Latitude, city.Location.Longitude
		info.Latitude, info.Longitude = &lat, &lon
	}
	return info
}

// WorkQueues connect request handlers to background workers. Everything a
// handler pushes here is fire-and-forget: the hot path never waits on
// geolocation, title fetching or webhook bookkeeping.
type WorkQueues struct {
	Geo           chan geoJob
	Title         chan titleJob
	Events        chan DomainEvent
	WebhookSignal chan struct{}
}

type geoJob struct {
	VisitId core.VisitID
	Ip      string
}

type titleJob struct {
	ShortUrlId core.ShortUrlID
	LongUrl    core.LongUrl
}

func NewWorkQueues() *WorkQueues {
	return &WorkQueues{
		Geo:           make(chan geoJob, 4096),
		Title:         make(chan titleJob, 4096),
		Events:        make(chan DomainEvent, 4096),
		WebhookSignal: make(chan struct{}, 1),
	}
}

// PublishEvent publishes an integration event. Never blocks: fan-out to
// webhook subscribers happens on the event worker.
func (q *WorkQueues) PublishEvent(event DomainEvent) {
	select {
	case q.Events <- event:
	default:
	}
}

func (q *WorkQueues) enqueueGeo(visitId core.VisitID, ip string) {
	select {
	case q.Geo <- geoJob{VisitId: visitId, Ip: ip}:
	default:
	}
}

func (q *WorkQueues) enqueueTitle(shortUrlId core.ShortUrlID, longUrl core.LongUrl) {
	select {
	case q.Title <- titleJob{ShortUrlId: shortUrlId, LongUrl: longUrl}:
	default:
	}
}

func (q *WorkQueues) signalWebhooks() {
	select {
	case q.WebhookSignal <- struct{}{}:
	default:
	}
}

// StartWorkers launches all background workers; they stop when ctx is
// cancelled.
func (a *App) StartWorkers(ctx context.Context) {
	go a.eventWorker(ctx)
	go a.geoWorker(ctx)
	go a.titleWorker(ctx)
	go a.webhookWorker(ctx)
	go a.geoDbUpdater(ctx)
}

// eventWorker turns published events into queued webhook deliveries.
func (a *App) eventWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-a.Queues.Events:
			hooks, err := data.ListWebhooksForEvent(ctx, a.Db, event.Kind())
			if err != nil {
				a.Logger.Warn("Failed to fan out event to webhooks", "event", event.Kind().Slug(), "error", err)
				continue
			}
			if len(hooks) == 0 {
				continue
			}
			payload := event.ToDeliveryPayload(time.Now().UTC())
			failed := false
			for _, hook := range hooks {
				if err := data.EnqueueDelivery(ctx, a.Db, hook.Id, event.Kind(), payload); err != nil {
					a.Logger.Warn("Failed to enqueue webhook delivery", "webhook", hook.Id, "error", err)
					failed = true
				}
			}
			if !failed {
				a.Queues.signalWebhooks()
			}
		}
	}
}

// geoWorker resolves geolocation for recorded visits.
func (a *App) geoWorker(ctx context.Context) {
	resolve := func(visitId core.VisitID, ip string) {
		var err error
		if info := a.Geo.TryLookup(ip); info != nil {
			err = data.SetVisitGeo(ctx, a.Db, visitId, *info)
		} else {
			err = data.MarkGeoResolved(ctx, a.Db, visitId)
		}
		if err != nil {
			a.Logger.Warn("Failed to geolocate visit", "visit", visitId.Value(), "error", err)
		}
	}

	// Catch up on visits left unresolved by previous runs.
	if a.Geo.IsAvailable() {
		pending, err := data.ListPendingGeo(ctx, a.Db, 1000)
		if err != nil {
			a.Logger.Warn("Geo catch-up scan failed", "error", err)
		} else {
			for _, p := range pending {
				resolve(p.Id, p.Ip)
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case job := <-a.Queues.Geo:
			resolve(job.VisitId, job.Ip)
		}
	}
}

var titleRegex = regexp.MustCompile(`(?i)<title[^>]*>\s*([^<]{1,512})`)

// TryFetchTitle fetches a page and extracts its <title>, when the response is
// a successful HTML document.
func (a *App) TryFetchTitle(ctx context.Context, longUrl core.LongUrl) string {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, longUrl.Value(), nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "gort-title-resolver/1.0")
	resp, err := a.titleClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 ||
		!strings.Contains(strings.ToLower(contentType), "html") {
		return ""
	}

	buffer, _ := io.ReadAll(io.LimitReader(resp.Body, 65536))
	match := titleRegex.FindSubmatch(buffer)
	if match == nil {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(strings.TrimSpace(string(match[1]))))
}

// titleWorker fetches page titles for newly created short URLs.
func (a *App) titleWorker(ctx context.Context) {
	if !a.Cfg.AutoResolveTitles {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-a.Queues.Title:
			if title := a.TryFetchTitle(ctx, job.LongUrl); title != "" {
				if err := data.SetResolvedTitle(ctx, a.Db, job.ShortUrlId, title); err != nil {
					a.Logger.Warn("Failed to store title", "shortUrl", job.ShortUrlId.Value(), "error", err)
				}
			}
		}
	}
}

const webhookMaxAttempts = 6

func signWebhookPayload(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// webhookWorker delivers queued webhook payloads with retries and HMAC
// signatures.
func (a *App) webhookWorker(ctx context.Context) {
	deliver := func(due data.DueDelivery) {
		delivery, hook := due.Delivery, due.Webhook
		reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, hook.Url,
			strings.NewReader(delivery.Payload))
		if err != nil {
			_ = data.MarkFailedAttempt(ctx, a.Db, delivery.Id, delivery.Attempts, webhookMaxAttempts, err.Error())
			return
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("X-Gort-Event", delivery.Event)
		req.Header.Set("X-Gort-Signature", signWebhookPayload(hook.Secret, delivery.Payload))

		resp, err := a.webhookClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			a.Logger.Warn("Webhook delivery failed", "delivery", delivery.Id, "error", err)
			_ = data.MarkFailedAttempt(ctx, a.Db, delivery.Id, delivery.Attempts, webhookMaxAttempts, err.Error())
			return
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			_ = data.MarkDelivered(ctx, a.Db, delivery.Id)
		} else {
			_ = data.MarkFailedAttempt(ctx, a.Db, delivery.Id, delivery.Attempts, webhookMaxAttempts,
				fmt.Sprintf("HTTP %d", resp.StatusCode))
		}
	}

	for {
		// Wake up when new deliveries are enqueued, or poll for retries.
		select {
		case <-ctx.Done():
			return
		case <-a.Queues.WebhookSignal:
		case <-time.After(15 * time.Second):
		}
		due, err := data.DueDeliveries(ctx, a.Db, 50)
		if err != nil {
			a.Logger.Warn("Failed to load due webhook deliveries", "error", err)
			continue
		}
		for _, d := range due {
			deliver(d)
		}
	}
}

// geoDbUpdater downloads and refreshes the GeoLite2 city database when a
// license key is configured.
func (a *App) geoDbUpdater(ctx context.Context) {
	a.Geo.Reload()

	if a.Cfg.GeoLiteLicenseKey == "" {
		if !a.Geo.IsAvailable() {
			a.Logger.Info("No GeoLite2 license key configured; visits will not be geolocated. " +
				"Set GORT_GEOLITE_LICENSE_KEY to enable geolocation.")
		}
		return
	}

	for {
		dbPath := a.Cfg.GeoDbPath()
		stale := true
		if info, err := os.Stat(dbPath); err == nil {
			stale = info.ModTime().UTC().Before(time.Now().UTC().AddDate(0, 0, -7))
		}
		if stale {
			ok, err := a.downloadGeoDb(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				a.Logger.Warn("GeoLite2 download failed", "error", err)
			} else if !ok {
				a.Logger.Warn("GeoLite2 download did not contain an .mmdb file")
			} else {
				a.Geo.Reload()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(12 * time.Hour):
		}
	}
}

func (a *App) downloadGeoDb(ctx context.Context) (bool, error) {
	downloadUrl := "https://download.maxmind.com/app/geoip_download" +
		"?edition_id=GeoLite2-City&license_key=" + url.QueryEscape(a.Cfg.GeoLiteLicenseKey) +
		"&suffix=tar.gz"

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, downloadUrl, nil)
	if err != nil {
		return false, err
	}
	resp, err := a.geoClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return false, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !strings.HasSuffix(strings.ToLower(header.Name), ".mmdb") {
			continue
		}
		if err := os.MkdirAll(a.Cfg.DataDir, 0o755); err != nil {
			return false, err
		}
		target := a.Cfg.GeoDbPath()
		tmp := target + ".tmp"
		out, err := os.Create(tmp)
		if err != nil {
			return false, err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			_ = os.Remove(tmp)
			return false, err
		}
		if err := out.Close(); err != nil {
			return false, err
		}
		if err := os.Rename(tmp, target); err != nil {
			return false, err
		}
		return true, nil
	}
}
