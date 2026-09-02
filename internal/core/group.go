package core

import (
	"errors"
	"strings"
)

// GroupName is a normalized link-group name, typically sourced from an OIDC
// groups claim (e.g. Keycloak's "/marketing"). Normalization strips
// whitespace and the leading slash of a top-level group path so "/marketing"
// and "marketing" name the same group; nested paths keep their inner slashes
// ("/a/b" → "a/b").
type GroupName struct{ value string }

func (g GroupName) Value() string { return g.value }

func NewGroupName(raw string) (GroupName, error) {
	normalized := NormalizeGroup(raw)
	if normalized == "" {
		return GroupName{}, errors.New("Group names cannot be empty.")
	}
	if len(normalized) > 255 {
		return GroupName{}, errors.New("Group names cannot be longer than 255 characters.")
	}
	if strings.Contains(normalized, ",") {
		return GroupName{}, errors.New("Group names cannot contain commas.")
	}
	return GroupName{value: normalized}, nil
}

// NormalizeGroup maps a raw group value (form field or token claim entry) to
// its canonical form; "" means "no group".
func NormalizeGroup(raw string) string {
	return strings.TrimPrefix(strings.TrimSpace(raw), "/")
}

// NormalizeGroups normalizes a claim's group list, dropping empties and
// duplicates while preserving order.
func NormalizeGroups(raw []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, g := range raw {
		normalized := NormalizeGroup(g)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}

// GroupsContain reports whether a normalized group list contains the group.
func GroupsContain(groups []string, group string) bool {
	for _, g := range groups {
		if g == group {
			return true
		}
	}
	return false
}
