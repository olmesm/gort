package core

import (
	"errors"
	"net/url"
	"strings"
)

// LongUrl is an absolute http(s) URL that has passed validation. The zero
// value is invalid; values come only from NewLongUrl.
type LongUrl struct{ value string }

func (u LongUrl) Value() string { return u.value }

// NewLongUrl parses and validates a long URL.
func NewLongUrl(raw string) (LongUrl, error) {
	if strings.TrimSpace(raw) == "" {
		return LongUrl{}, errors.New("The long URL is required.")
	}
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return LongUrl{}, errors.New("The long URL must be an absolute http(s) URL.")
	}
	if len(raw) > 2048 {
		return LongUrl{}, errors.New("The long URL cannot be longer than 2048 characters.")
	}
	return LongUrl{value: raw}, nil
}

// TagName is a normalized tag name: trimmed, lowercase, comma-free.
type TagName struct{ value string }

func (t TagName) Value() string { return t.value }

func NewTagName(tag string) (TagName, error) {
	if strings.TrimSpace(tag) == "" {
		return TagName{}, errors.New("Tag names cannot be empty.")
	}
	t := strings.ToLower(strings.TrimSpace(tag))
	if len(t) > 255 {
		return TagName{}, errors.New("Tag names cannot be longer than 255 characters.")
	}
	if strings.Contains(t, ",") {
		return TagName{}, errors.New("Tag names cannot contain commas.")
	}
	return TagName{value: t}, nil
}

// NewTagNames parses many tags, dropping duplicates while preserving
// first-seen order.
func NewTagNames(tags []string) ([]TagName, error) {
	var out []TagName
	seen := map[string]bool{}
	for _, raw := range tags {
		t, err := NewTagName(raw)
		if err != nil {
			return nil, err
		}
		if !seen[t.Value()] {
			seen[t.Value()] = true
			out = append(out, t)
		}
	}
	return out, nil
}

// ParseTagCsv splits a comma-separated form field into tags.
func ParseTagCsv(csv string) ([]TagName, error) {
	var parts []string
	for _, p := range strings.Split(csv, ",") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return NewTagNames(parts)
}

func TagValues(tags []TagName) []string {
	out := make([]string, len(tags))
	for i, t := range tags {
		out[i] = t.Value()
	}
	return out
}

// DomainAuthority is a domain authority (host, optionally with port): no
// scheme, no path.
type DomainAuthority struct{ value string }

func (d DomainAuthority) Value() string { return d.value }

func NewDomainAuthority(authority string) (DomainAuthority, error) {
	if strings.TrimSpace(authority) == "" {
		return DomainAuthority{}, errors.New("The domain authority is required.")
	}
	a := strings.ToLower(strings.TrimSpace(authority))
	if strings.Contains(a, "://") || strings.Contains(a, "/") {
		return DomainAuthority{}, errors.New("The domain must be a plain authority (host or host:port), without scheme or path.")
	}
	parsed, err := url.Parse("http://" + a)
	if err != nil || parsed.Host == "" ||
		(parsed.Host != a && parsed.Host != strings.TrimRight(a, ":")) ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		parsed.User != nil {
		return DomainAuthority{}, errors.New("The domain is not a valid authority.")
	}
	if strings.ContainsAny(a, " \t") {
		return DomainAuthority{}, errors.New("The domain is not a valid authority.")
	}
	return DomainAuthority{value: a}, nil
}

// ForwardQuery merges a redirect target with the incoming request's query
// string, when query forwarding is enabled. Params already present in the
// target are kept; incoming params are appended. Operates on the resolved
// target (which may come from a redirect rule), hence plain strings.
func ForwardQuery(targetUrl string, incoming [][2]string) string {
	if len(incoming) == 0 {
		return targetUrl
	}
	var encoded []string
	for _, kv := range incoming {
		k, v := kv[0], kv[1]
		if v == "" {
			encoded = append(encoded, escapeData(k))
		} else {
			encoded = append(encoded, escapeData(k)+"="+escapeData(v))
		}
	}
	joined := strings.Join(encoded, "&")

	base, fragment := targetUrl, ""
	if idx := strings.IndexByte(targetUrl, '#'); idx >= 0 {
		base, fragment = targetUrl[:idx], targetUrl[idx:]
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + joined + fragment
}

// escapeData mirrors Uri.EscapeDataString: RFC 3986 unreserved characters
// stay, everything else is percent-encoded (space becomes %20, not +).
func escapeData(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
