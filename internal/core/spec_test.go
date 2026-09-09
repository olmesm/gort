package core

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func int64Ptr(v int64) *int64          { return &v }
func domainIDPtr(v DomainID) *DomainID { return &v }
func intPtr(v int) *int                { return &v }
func strPtr(v string) *string          { return &v }
func timePtr(v time.Time) *time.Time   { return &v }

// ---- Lifetime invariants ----

func TestLifetimesRejectANonPositiveMaxVisitBudget(t *testing.T) {
	if _, err := NewLifetime(nil, nil, int64Ptr(0)); err == nil {
		t.Fatal("expected rejection")
	} else if !strings.Contains(err.Error(), "greater than zero") {
		t.Errorf("wrong error: %s", err)
	}
	if _, err := NewLifetime(nil, nil, int64Ptr(-5)); err == nil {
		t.Fatal("expected rejection")
	}
}

func TestLifetimesRejectAnInvertedValidityWindow(t *testing.T) {
	since := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if _, err := NewLifetime(&since, &until, nil); err == nil {
		t.Fatal("expected rejection")
	} else if !strings.Contains(err.Error(), "earlier than") {
		t.Errorf("wrong error: %s", err)
	}
}

func TestLifetimeActivityChecksCoverAllThreeExpiryReasons(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	if ok, _ := UnboundedLifetime.CheckActive(now, 0); !ok {
		t.Error("unbounded lifetime should be active")
	}

	notYet := Lifetime{ValidSince: timePtr(now.AddDate(0, 0, 1))}
	if ok, reason := notYet.CheckActive(now, 0); ok || reason != NotYetValid {
		t.Errorf("got %v %v", ok, reason)
	}

	expired := Lifetime{ValidUntil: timePtr(now.AddDate(0, 0, -1))}
	if ok, reason := expired.CheckActive(now, 0); ok || reason != NoLongerValid {
		t.Errorf("got %v %v", ok, reason)
	}

	exhausted := Lifetime{MaxVisits: int64Ptr(5)}
	if ok, reason := exhausted.CheckActive(now, 5); ok || reason != MaxVisitsReached {
		t.Errorf("got %v %v", ok, reason)
	}
}

// ---- ShortUrlSpec: the single validation path ----

func TestSpecsCollectEveryValidatedPiece(t *testing.T) {
	spec, serr := NewShortURLSpec(ShortURLSpecInput{
		LongURL:        "https://example.com/x",
		CustomSlug:     strPtr("My-Slug"),
		Tags:           []string{" Marketing ", "LAUNCH"},
		MaxVisits:      int64Ptr(10),
		RedirectStatus: intPtr(301),
	})
	if serr != nil {
		t.Fatalf("unexpected: %s", serr)
	}
	if spec.LongURL.Value() != "https://example.com/x" {
		t.Errorf("long url: %q", spec.LongURL.Value())
	}
	if spec.CustomSlug == nil || spec.CustomSlug.Value() != "My-Slug" {
		t.Errorf("custom slug: %v", spec.CustomSlug)
	}
	if !reflect.DeepEqual(TagValues(spec.Tags), []string{"marketing", "launch"}) {
		t.Errorf("tags: %v", TagValues(spec.Tags))
	}
	if spec.RedirectStatus == nil || *spec.RedirectStatus != MovedPermanently {
		t.Errorf("status: %v", spec.RedirectStatus)
	}
}

func TestSpecsRejectAZeroMaxVisitBudget(t *testing.T) {
	_, serr := NewShortURLSpec(ShortURLSpecInput{LongURL: "https://example.com", MaxVisits: int64Ptr(0)})
	if serr == nil {
		t.Fatal("expected rejection")
	}
	if !errors.Is(serr, ErrInvalidLifetime) {
		t.Errorf("wrong error category: %s", serr)
	}
}

func TestSpecsRejectUnsupportedRedirectStatuses(t *testing.T) {
	_, serr := NewShortURLSpec(ShortURLSpecInput{LongURL: "https://example.com", RedirectStatus: intPtr(418)})
	if serr == nil {
		t.Fatal("expected rejection")
	}
	if !errors.Is(serr, ErrInvalidRedirectStatus) || !strings.Contains(serr.Error(), "'418'") {
		t.Errorf("wrong error: %v", serr)
	}
}

func TestSpecsBlankOutWhitespaceTitles(t *testing.T) {
	spec, serr := NewShortURLSpec(ShortURLSpecInput{LongURL: "https://example.com", Title: strPtr("   ")})
	if serr != nil {
		t.Fatal(serr)
	}
	if spec.Title != nil {
		t.Errorf("expected nil title, got %q", *spec.Title)
	}
}

// ---- API key roles: unknown stored roles must never default to admin ----

func TestAPIKeyRoleParsingIsFailClosed(t *testing.T) {
	if role, ok := APIKeyRoleOfStored("admin", nil); !ok || role.Kind != RoleAdmin {
		t.Error("admin should parse")
	}
	if role, ok := APIKeyRoleOfStored("author", nil); !ok || role.Kind != RoleAuthor {
		t.Error("author should parse")
	}
	if role, ok := APIKeyRoleOfStored("domain", domainIDPtr(7)); !ok || role.Kind != RoleDomain || role.DomainID != DomainID(7) {
		t.Error("domain should parse with id")
	}
	// A domain role without a domain id is corrupt, not admin.
	if _, ok := APIKeyRoleOfStored("domain", nil); ok {
		t.Error("domain without id must not parse")
	}
	// Unknown roles are rejected, not defaulted.
	if _, ok := APIKeyRoleOfStored("superuser", nil); ok {
		t.Error("unknown role must not parse")
	}
	if _, ok := APIKeyRoleOfStored("", nil); ok {
		t.Error("empty role must not parse")
	}
}

func TestTypedIDsDoNotCrossAssign(t *testing.T) {
	// Compile-time guarantee — this test documents the intent.
	shortURLID := ShortURLID(1)
	domainID := DomainID(1)
	if shortURLID.Value() != 1 || domainID.Value() != 1 {
		t.Error("unexpected values")
	}
}
