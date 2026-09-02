package core

import (
	"errors"
	"strings"
	"time"
)

// Lifetime says when a short URL is allowed to redirect: an optional validity
// window plus an optional visit budget. Invariants (MaxVisits > 0,
// ValidSince < ValidUntil) are enforced by NewLifetime — build values
// through it.
type Lifetime struct {
	ValidSince *time.Time
	ValidUntil *time.Time
	MaxVisits  *int64
}

var UnboundedLifetime = Lifetime{}

func NewLifetime(validSince, validUntil *time.Time, maxVisits *int64) (Lifetime, error) {
	if maxVisits != nil && *maxVisits <= 0 {
		return Lifetime{}, errors.New("maxVisits must be greater than zero.")
	}
	if validSince != nil && validUntil != nil && !validSince.Before(*validUntil) {
		return Lifetime{}, errors.New("validSince must be earlier than validUntil.")
	}
	return Lifetime{ValidSince: validSince, ValidUntil: validUntil, MaxVisits: maxVisits}, nil
}

// CheckActive reports whether the short URL is currently allowed to redirect;
// the second return value carries the reason when it is not.
func (l Lifetime) CheckActive(now time.Time, validVisitCount int64) (bool, ExpirationReason) {
	if l.ValidSince != nil && now.Before(*l.ValidSince) {
		return false, NotYetValid
	}
	if l.ValidUntil != nil && now.After(*l.ValidUntil) {
		return false, NoLongerValid
	}
	if l.MaxVisits != nil && validVisitCount >= *l.MaxVisits {
		return false, MaxVisitsReached
	}
	return true, ""
}

// ShortUrlSpec is a fully validated request to create a short URL. This is
// the single sanctioned construction path — the REST API, the dashboard and
// any future entry point all go through NewShortUrlSpec, so every invariant
// is enforced exactly once.
type ShortUrlSpec struct {
	LongUrl        LongUrl
	CustomSlug     *ShortCode
	CodeLength     *int
	Domain         *DomainAuthority
	Title          *string
	Tags           []TagName
	Lifetime       Lifetime
	RedirectStatus *RedirectStatus
	ForwardQuery   *bool
	Crawlable      *bool
	FindIfExists   bool
}

// ShortUrlSpecInput is raw, unvalidated creation input as it arrives from a
// JSON body or form.
type ShortUrlSpecInput struct {
	LongUrl        string
	CustomSlug     *string
	CodeLength     *int
	Domain         *string
	Title          *string
	Tags           []string
	ValidSince     *time.Time
	ValidUntil     *time.Time
	MaxVisits      *int64
	RedirectStatus *int
	ForwardQuery   *bool
	Crawlable      *bool
	FindIfExists   bool
}

func normalizeTitle(t *string) *string {
	if t == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*t)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func parseStatus(code *int) (*RedirectStatus, *ShortUrlError) {
	if code == nil {
		return nil, nil
	}
	status, ok := RedirectStatusOfCode(*code)
	if !ok {
		return nil, InvalidRedirectStatusError(*code)
	}
	return &status, nil
}

// NewShortUrlSpec parses and validates raw input into a spec.
func NewShortUrlSpec(input ShortUrlSpecInput) (*ShortUrlSpec, *ShortUrlError) {
	longUrl, err := NewLongUrl(input.LongUrl)
	if err != nil {
		return nil, NewShortUrlError(ErrInvalidLongUrl, err.Error())
	}

	var customSlug *ShortCode
	if input.CustomSlug != nil {
		code, err := ShortCodeOfSlug(*input.CustomSlug)
		if err != nil {
			return nil, NewShortUrlError(ErrInvalidSlug, err.Error())
		}
		customSlug = &code
	}

	var domain *DomainAuthority
	if input.Domain != nil {
		d, err := NewDomainAuthority(*input.Domain)
		if err != nil {
			return nil, NewShortUrlError(ErrUnknownDomain, err.Error())
		}
		domain = &d
	}

	tags, err := NewTagNames(input.Tags)
	if err != nil {
		return nil, NewShortUrlError(ErrInvalidTag, err.Error())
	}

	lifetime, err := NewLifetime(input.ValidSince, input.ValidUntil, input.MaxVisits)
	if err != nil {
		return nil, NewShortUrlError(ErrInvalidLifetime, err.Error())
	}

	status, serr := parseStatus(input.RedirectStatus)
	if serr != nil {
		return nil, serr
	}

	return &ShortUrlSpec{
		LongUrl:        longUrl,
		CustomSlug:     customSlug,
		CodeLength:     input.CodeLength,
		Domain:         domain,
		Title:          normalizeTitle(input.Title),
		Tags:           tags,
		Lifetime:       lifetime,
		RedirectStatus: status,
		ForwardQuery:   input.ForwardQuery,
		Crawlable:      input.Crawlable,
		FindIfExists:   input.FindIfExists,
	}, nil
}

// ShortUrlEdit is a fully validated edit: the final values every mutable
// field should take. PATCH-merging (absent = keep current) happens *before*
// validation, so the resulting state is checked as a whole.
type ShortUrlEdit struct {
	LongUrl        LongUrl
	Title          *string
	Lifetime       Lifetime
	RedirectStatus RedirectStatus
	ForwardQuery   bool
	Crawlable      bool
	// Tags == nil means leave tags unchanged. An empty non-nil slice clears them.
	Tags       []TagName
	ChangeTags bool
}

type ShortUrlEditInput struct {
	LongUrl        string
	Title          *string
	ValidSince     *time.Time
	ValidUntil     *time.Time
	MaxVisits      *int64
	RedirectStatus int
	ForwardQuery   bool
	Crawlable      bool
	// Tags is only applied when ChangeTags is true.
	Tags       []string
	ChangeTags bool
}

func NewShortUrlEdit(input ShortUrlEditInput) (*ShortUrlEdit, *ShortUrlError) {
	longUrl, err := NewLongUrl(input.LongUrl)
	if err != nil {
		return nil, NewShortUrlError(ErrInvalidLongUrl, err.Error())
	}

	lifetime, err := NewLifetime(input.ValidSince, input.ValidUntil, input.MaxVisits)
	if err != nil {
		return nil, NewShortUrlError(ErrInvalidLifetime, err.Error())
	}

	status, ok := RedirectStatusOfCode(input.RedirectStatus)
	if !ok {
		return nil, InvalidRedirectStatusError(input.RedirectStatus)
	}

	var tags []TagName
	if input.ChangeTags {
		tags, err = NewTagNames(input.Tags)
		if err != nil {
			return nil, NewShortUrlError(ErrInvalidTag, err.Error())
		}
		if tags == nil {
			tags = []TagName{}
		}
	}

	return &ShortUrlEdit{
		LongUrl:        longUrl,
		Title:          normalizeTitle(input.Title),
		Lifetime:       lifetime,
		RedirectStatus: status,
		ForwardQuery:   input.ForwardQuery,
		Crawlable:      input.Crawlable,
		Tags:           tags,
		ChangeTags:     input.ChangeTags,
	}, nil
}
