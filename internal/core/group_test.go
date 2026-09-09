package core

import (
	"reflect"
	"testing"
)

func TestGroupNormalizationStripsTheLeadingSlash(t *testing.T) {
	if got := NormalizeGroup(" /marketing "); got != "marketing" {
		t.Errorf("got %q", got)
	}
	if got := NormalizeGroup("/a/b"); got != "a/b" {
		t.Errorf("nested: got %q", got)
	}
	if got := NormalizeGroup("plain"); got != "plain" {
		t.Errorf("plain: got %q", got)
	}
}

func TestGroupListsAreNormalizedAndDeduplicated(t *testing.T) {
	got := NormalizeGroups([]string{"/team-a", "team-a", "", "  ", "/team-b"})
	if !reflect.DeepEqual(got, []string{"team-a", "team-b"}) {
		t.Errorf("got %v", got)
	}
}

func TestGroupNamesRejectEmptyAndCommas(t *testing.T) {
	if _, err := NewGroupName("  /  "); err == nil {
		t.Error("empty group must be rejected")
	}
	if _, err := NewGroupName("a,b"); err == nil {
		t.Error("comma group must be rejected")
	}
	group, err := NewGroupName("/ops")
	if err != nil || group.Value() != "ops" {
		t.Errorf("got %v %v", group.Value(), err)
	}
}

func TestSpecsCarryTheValidatedGroup(t *testing.T) {
	group := "/team-a"
	spec, serr := NewShortUrlSpec(ShortUrlSpecInput{LongUrl: "https://example.com", Group: &group})
	if serr != nil {
		t.Fatal(serr)
	}
	if spec.Group == nil || spec.Group.Value() != "team-a" {
		t.Errorf("group: %v", spec.Group)
	}

	empty := "  "
	spec, serr = NewShortUrlSpec(ShortUrlSpecInput{LongUrl: "https://example.com", Group: &empty})
	if serr != nil || spec.Group != nil {
		t.Errorf("blank group should mean no group: %v %v", spec.Group, serr)
	}
}
