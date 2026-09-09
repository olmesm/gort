package core

import (
	"reflect"
	"testing"
)

func TestValidLongURLsAreAccepted(t *testing.T) {
	for _, url := range []string{"https://example.com", "http://example.com/path?q=1#frag"} {
		if _, err := NewLongURL(url); err != nil {
			t.Errorf("url %q rejected: %s", url, err)
		}
	}
}

func TestInvalidLongURLsAreRejected(t *testing.T) {
	for _, url := range []string{"", "nope", "ftp://example.com", "//relative", "example.com"} {
		if _, err := NewLongURL(url); err == nil {
			t.Errorf("url %q accepted", url)
		}
	}
}

func TestTagsAreNormalizedToLowercaseAndDeduplicated(t *testing.T) {
	tags, err := NewTagNames([]string{" Alpha ", "beta", "ALPHA"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(TagValues(tags), []string{"alpha", "beta"}) {
		t.Errorf("got %v", TagValues(tags))
	}
}

func TestTagsWithCommasAreRejected(t *testing.T) {
	if _, err := NewTagName("a,b"); err == nil {
		t.Error("expected rejection")
	}
}

func TestValidDomainAuthoritiesAreAccepted(t *testing.T) {
	for _, authority := range []string{"example.com", "links.example.com:8443"} {
		if _, err := NewDomainAuthority(authority); err != nil {
			t.Errorf("authority %q rejected: %s", authority, err)
		}
	}
}

func TestInvalidDomainAuthoritiesAreRejected(t *testing.T) {
	for _, authority := range []string{"https://example.com", "example.com/path", ""} {
		if a, err := NewDomainAuthority(authority); err == nil {
			t.Errorf("authority %q accepted as %q", authority, a.Value())
		}
	}
}

func TestQueryForwardingAppendsParams(t *testing.T) {
	got := ForwardQuery("https://example.com/p", [][2]string{{"a", "1"}})
	if got != "https://example.com/p?a=1" {
		t.Errorf("got %q", got)
	}
}

func TestQueryForwardingMergesWithExistingQuery(t *testing.T) {
	got := ForwardQuery("https://example.com/p?x=0", [][2]string{{"a", "1"}, {"b", "2"}})
	if got != "https://example.com/p?x=0&a=1&b=2" {
		t.Errorf("got %q", got)
	}
}

func TestQueryForwardingKeepsTheFragmentLast(t *testing.T) {
	got := ForwardQuery("https://example.com/p#sec", [][2]string{{"a", "1"}})
	if got != "https://example.com/p?a=1#sec" {
		t.Errorf("got %q", got)
	}
}

func TestQueryForwardingURLEncodesValues(t *testing.T) {
	got := ForwardQuery("https://example.com/p", [][2]string{{"q", "a b&c"}})
	if got != "https://example.com/p?q=a%20b%26c" {
		t.Errorf("got %q", got)
	}
}

func TestQueryForwardingWithNoParamsIsIdentity(t *testing.T) {
	got := ForwardQuery("https://example.com/p", nil)
	if got != "https://example.com/p" {
		t.Errorf("got %q", got)
	}
}
