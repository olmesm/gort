package core

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// ShortCode is a short code / custom slug that has passed validation.
// Values come only from GenerateShortCode or ShortCodeOfSlug.
type ShortCode struct{ value string }

func (c ShortCode) Value() string { return c.value }

// ShortCodeAlphabet is the unambiguous base-62 alphabet used for generated codes.
const ShortCodeAlphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

const (
	MinCodeLength     = 4
	DefaultCodeLength = 5
	MaxSlugLength     = 255
)

// slugChars are the characters allowed in a custom slug: url-safe, no
// percent-encoding needed.
const slugExtraChars = "-_.~+"

func isSlugChar(c rune) bool {
	return strings.ContainsRune(ShortCodeAlphabet, c) || strings.ContainsRune(slugExtraChars, c)
}

// GenerateShortCode generates a cryptographically random short code of the
// given length (clamped to the minimum).
func GenerateShortCode(length int) ShortCode {
	if length < MinCodeLength {
		length = MinCodeLength
	}
	chars := make([]byte, length)
	max := big.NewInt(int64(len(ShortCodeAlphabet)))
	for i := range chars {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err) // crypto/rand failure is not recoverable
		}
		chars[i] = ShortCodeAlphabet[n.Int64()]
	}
	return ShortCode{value: string(chars)}
}

// ShortCodeOfSlug parses a caller-supplied custom slug. Slugs may contain
// slashes to allow "path-style" short URLs (e.g. "docs/intro"), but no empty
// segments.
func ShortCodeOfSlug(slug string) (ShortCode, error) {
	if strings.TrimSpace(slug) == "" {
		return ShortCode{}, errors.New("Custom slug cannot be empty.")
	}
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if len(slug) == 0 {
		return ShortCode{}, errors.New("Custom slug cannot be empty.")
	}
	if len(slug) > MaxSlugLength {
		return ShortCode{}, fmt.Errorf("Custom slug cannot be longer than %d characters.", MaxSlugLength)
	}
	for _, segment := range strings.Split(slug, "/") {
		if len(segment) == 0 {
			return ShortCode{}, errors.New("Custom slug cannot contain empty path segments.")
		}
	}
	for _, c := range slug {
		if c != '/' && !isSlugChar(c) {
			return ShortCode{}, fmt.Errorf("Custom slug contains an invalid character: '%c'.", c)
		}
	}
	return ShortCode{value: slug}, nil
}

// IsValidSlug reports whether this string would be accepted as a slug.
// Used to classify orphan traffic.
func IsValidSlug(candidate string) bool {
	_, err := ShortCodeOfSlug(candidate)
	return err == nil
}
