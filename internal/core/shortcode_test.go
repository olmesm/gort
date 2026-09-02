package core

import (
	"strings"
	"testing"
)

func TestGeneratedCodesHaveTheRequestedLength(t *testing.T) {
	if got := len(GenerateShortCode(5).Value()); got != 5 {
		t.Errorf("got %d", got)
	}
	if got := len(GenerateShortCode(12).Value()); got != 12 {
		t.Errorf("got %d", got)
	}
}

func TestGeneratedCodesNeverGoBelowTheMinimumLength(t *testing.T) {
	if got := len(GenerateShortCode(1).Value()); got != MinCodeLength {
		t.Errorf("got %d", got)
	}
}

func TestGeneratedCodesOnlyUseTheAlphabet(t *testing.T) {
	for i := 0; i < 50; i++ {
		code := GenerateShortCode(8)
		for _, c := range code.Value() {
			if !strings.ContainsRune(ShortCodeAlphabet, c) {
				t.Fatalf("character %q not in alphabet", c)
			}
		}
	}
}

func TestGeneratedCodesAreOverwhelminglyUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		seen[GenerateShortCode(8).Value()] = true
	}
	if len(seen) != 1000 {
		t.Errorf("expected 1000 unique codes, got %d", len(seen))
	}
}

func TestValidSlugsAreAccepted(t *testing.T) {
	for _, slug := range []string{"my-slug", "MySlug_2024", "docs/intro", "a.b~c+d"} {
		if _, err := ShortCodeOfSlug(slug); err != nil {
			t.Errorf("slug %q rejected: %s", slug, err)
		}
	}
}

func TestInvalidSlugsAreRejected(t *testing.T) {
	for _, slug := range []string{"", "   ", "has space", "emoji😀", "a//b", "per%cent"} {
		if code, err := ShortCodeOfSlug(slug); err == nil {
			t.Errorf("slug %q accepted as %q", slug, code.Value())
		}
	}
}

func TestSlugsAreTrimmedOfSurroundingSlashes(t *testing.T) {
	code, err := ShortCodeOfSlug("/abc/")
	if err != nil {
		t.Fatal(err)
	}
	if code.Value() != "abc" {
		t.Errorf("got %q", code.Value())
	}
}
