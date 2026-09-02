package core

import (
	"fmt"
	"strings"
)

// RedirectStatus is the HTTP status used when redirecting a short URL to its
// long URL. Only the four redirect statuses are representable.
type RedirectStatus int

const (
	MovedPermanently  RedirectStatus = 301
	Found             RedirectStatus = 302
	TemporaryRedirect RedirectStatus = 307
	PermanentRedirect RedirectStatus = 308
)

func (s RedirectStatus) Code() int { return int(s) }

// RedirectStatusOfCode returns the status for a code, or false for anything
// that is not a supported redirect status.
func RedirectStatusOfCode(code int) (RedirectStatus, bool) {
	switch code {
	case 301, 302, 307, 308:
		return RedirectStatus(code), true
	default:
		return 0, false
	}
}

// Device is the device family detected from a visitor's user agent, used by
// redirect rules.
type Device string

const (
	DeviceAndroid Device = "android"
	DeviceIos     Device = "ios"
	DeviceDesktop Device = "desktop"
	DeviceMobile  Device = "mobile"
)

func (d Device) Slug() string { return string(d) }

func DeviceOfSlug(s string) (Device, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "android":
		return DeviceAndroid, true
	case "ios":
		return DeviceIos, true
	case "desktop":
		return DeviceDesktop, true
	case "mobile":
		return DeviceMobile, true
	default:
		return "", false
	}
}

// RuleCondition is a single condition inside a redirect rule. All conditions
// of a rule must match for the rule's target URL to be used.
type ConditionType string

const (
	CondDevice     ConditionType = "device"
	CondLanguage   ConditionType = "language"
	CondQueryParam ConditionType = "query-param"
	CondIPAddress  ConditionType = "ip-address"
)

type RuleCondition struct {
	Type ConditionType
	// Key is only set for query-param conditions.
	Key string
	// Value: device slug, language tag, query value, or IP/CIDR.
	Value string
}

func DeviceIs(d Device) RuleCondition {
	return RuleCondition{Type: CondDevice, Value: d.Slug()}
}

func LanguageIs(lang string) RuleCondition {
	return RuleCondition{Type: CondLanguage, Value: lang}
}

func QueryParamIs(key, value string) RuleCondition {
	return RuleCondition{Type: CondQueryParam, Key: key, Value: value}
}

func IPInRange(cidr string) RuleCondition {
	return RuleCondition{Type: CondIPAddress, Value: cidr}
}

// RedirectRule is a conditional redirect target attached to a short URL.
// Rules are evaluated in ascending priority order; the first rule whose
// conditions all match wins.
type RedirectRule struct {
	Priority   int
	LongUrl    string
	Conditions []RuleCondition
}

// VisitorContext is everything known about the incoming request that redirect
// rules can match on.
type VisitorContext struct {
	UserAgent      string
	AcceptLanguage string
	Query          map[string]string
	RemoteIP       string
}

// VisitType is the kind of visit being tracked. Anything except
// VisitValidShortUrl is an "orphan" visit: traffic that reached the service
// but not an active short URL.
type VisitType string

const (
	VisitValidShortUrl      VisitType = "valid"
	VisitOrphanBaseUrl      VisitType = "base_url"
	VisitOrphanInvalidShort VisitType = "invalid_short_url"
	VisitOrphanRegular404   VisitType = "regular_404"
)

func (v VisitType) Slug() string   { return string(v) }
func (v VisitType) IsOrphan() bool { return v != VisitValidShortUrl }

func VisitTypeOfSlug(s string) (VisitType, bool) {
	switch s {
	case "valid":
		return VisitValidShortUrl, true
	case "base_url":
		return VisitOrphanBaseUrl, true
	case "invalid_short_url":
		return VisitOrphanInvalidShort, true
	case "regular_404":
		return VisitOrphanRegular404, true
	default:
		return "", false
	}
}

// ApiKeyRole is the access level attached to an API key. Parsing is partial
// on purpose: an unrecognized stored role must be rejected, never defaulted.
type ApiKeyRoleKind string

const (
	RoleAdmin  ApiKeyRoleKind = "admin"
	RoleAuthor ApiKeyRoleKind = "author"
	RoleDomain ApiKeyRoleKind = "domain"
)

type ApiKeyRole struct {
	Kind ApiKeyRoleKind
	// DomainID is only meaningful when Kind == RoleDomain.
	DomainID DomainID
}

func AdminRole() ApiKeyRole  { return ApiKeyRole{Kind: RoleAdmin} }
func AuthorRole() ApiKeyRole { return ApiKeyRole{Kind: RoleAuthor} }
func DomainRole(id DomainID) ApiKeyRole {
	return ApiKeyRole{Kind: RoleDomain, DomainID: id}
}

func (r ApiKeyRole) Slug() string { return string(r.Kind) }

// ApiKeyRoleOfStored reconstructs a role from its stored representation.
// Returns false for unknown role strings and for a domain role missing its
// domain id.
func ApiKeyRoleOfStored(slug string, domainID *int64) (ApiKeyRole, bool) {
	switch slug {
	case "admin":
		return AdminRole(), true
	case "author":
		return AuthorRole(), true
	case "domain":
		if domainID != nil {
			return DomainRole(DomainID(*domainID)), true
		}
		return ApiKeyRole{}, false
	default:
		return ApiKeyRole{}, false
	}
}

// UserRole is a dashboard user role.
type UserRole string

const (
	UserAdmin   UserRole = "admin"
	UserRegular UserRole = "user"
)

func (r UserRole) Slug() string { return string(r) }

func UserRoleOfSlug(s string) (UserRole, bool) {
	switch s {
	case "admin":
		return UserAdmin, true
	case "user":
		return UserRegular, true
	default:
		return "", false
	}
}

// WebhookEvent names the events webhooks can subscribe to.
type WebhookEvent string

const (
	EventUrlCreated          WebhookEvent = "url.created"
	EventVisitRecorded       WebhookEvent = "visit.recorded"
	EventOrphanVisitRecorded WebhookEvent = "orphan_visit.recorded"
)

func (e WebhookEvent) Slug() string { return string(e) }

func WebhookEventOfSlug(s string) (WebhookEvent, bool) {
	switch s {
	case "url.created":
		return EventUrlCreated, true
	case "visit.recorded":
		return EventVisitRecorded, true
	case "orphan_visit.recorded":
		return EventOrphanVisitRecorded, true
	default:
		return "", false
	}
}

var AllWebhookEvents = []WebhookEvent{EventUrlCreated, EventVisitRecorded, EventOrphanVisitRecorded}

// ExpirationReason says why a short URL, although it exists, refuses to
// redirect right now.
type ExpirationReason string

const (
	NotYetValid      ExpirationReason = "not_yet_valid"
	NoLongerValid    ExpirationReason = "no_longer_valid"
	MaxVisitsReached ExpirationReason = "max_visits_reached"
)

// ShortUrlError is everything that can go wrong creating (or editing) a short
// URL.
type ShortUrlErrorKind string

const (
	ErrInvalidLongUrl          ShortUrlErrorKind = "invalid-long-url"
	ErrInvalidSlug             ShortUrlErrorKind = "invalid-slug"
	ErrInvalidTag              ShortUrlErrorKind = "invalid-tag"
	ErrInvalidGroup            ShortUrlErrorKind = "invalid-group"
	ErrInvalidLifetime         ShortUrlErrorKind = "invalid-lifetime"
	ErrInvalidRedirectStatus   ShortUrlErrorKind = "invalid-redirect-status"
	ErrSlugInUse               ShortUrlErrorKind = "slug-in-use"
	ErrUnknownDomain           ShortUrlErrorKind = "unknown-domain"
	ErrCodeGenerationExhausted ShortUrlErrorKind = "code-generation-exhausted"
)

type ShortUrlError struct {
	Kind ShortUrlErrorKind
	// Detail carries the validation message (or slug/domain data for SlugInUse).
	Detail string
	Slug   string
	Domain string
	Status int
}

func (e *ShortUrlError) Error() string { return e.Message() }

// Message is the human-readable message, for UI banners and problem details.
func (e *ShortUrlError) Message() string {
	switch e.Kind {
	case ErrInvalidRedirectStatus:
		return fmt.Sprintf("'%d' is not a supported redirect status. Use 301, 302, 307 or 308.", e.Status)
	case ErrSlugInUse:
		return fmt.Sprintf("The slug '%s' is already in use on domain '%s'.", e.Slug, e.Domain)
	case ErrCodeGenerationExhausted:
		return "Could not find a free short code; try again or use a custom slug."
	default:
		return e.Detail
	}
}

func NewShortUrlError(kind ShortUrlErrorKind, detail string) *ShortUrlError {
	return &ShortUrlError{Kind: kind, Detail: detail}
}

func SlugInUseError(slug, domain string) *ShortUrlError {
	return &ShortUrlError{Kind: ErrSlugInUse, Slug: slug, Domain: domain}
}

func InvalidRedirectStatusError(status int) *ShortUrlError {
	return &ShortUrlError{Kind: ErrInvalidRedirectStatus, Status: status}
}

func CodeGenerationExhaustedError() *ShortUrlError {
	return &ShortUrlError{Kind: ErrCodeGenerationExhausted}
}
