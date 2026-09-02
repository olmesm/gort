package core

import "testing"

func visitor(ua, lang string, query map[string]string, ip string) VisitorContext {
	if query == nil {
		query = map[string]string{}
	}
	return VisitorContext{UserAgent: ua, AcceptLanguage: lang, Query: query, RemoteIP: ip}
}

func TestDeviceDetection(t *testing.T) {
	cases := []struct {
		ua       string
		expected string
	}{
		{"Mozilla/5.0 (Linux; Android 14)", "android"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0)", "ios"},
		{"Mozilla/5.0 (iPad; CPU OS 17_0)", "ios"},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X)", "desktop"},
		{"Mozilla/5.0 (Mobile; rv:1.0)", "mobile"},
	}
	for _, tc := range cases {
		if got := DetectDevice(tc.ua).Slug(); got != tc.expected {
			t.Errorf("DetectDevice(%q) = %q, want %q", tc.ua, got, tc.expected)
		}
	}
}

func TestMissingUserAgentCountsAsDesktop(t *testing.T) {
	if DetectDevice("") != DeviceDesktop {
		t.Error("expected desktop")
	}
}

func TestLanguageMatching(t *testing.T) {
	cases := []struct {
		wanted   string
		header   string
		expected bool
	}{
		{"en", "en-GB,en;q=0.9", true},
		{"en-GB", "en-GB,en;q=0.9", true},
		{"en-US", "en-GB,en;q=0.9", false},
		{"fr", "en-GB,en;q=0.9", false},
		{"EN", "en;q=0.5", true},
	}
	for _, tc := range cases {
		if got := MatchesLanguage(tc.wanted, tc.header); got != tc.expected {
			t.Errorf("MatchesLanguage(%q, %q) = %v, want %v", tc.wanted, tc.header, got, tc.expected)
		}
	}
}

func TestFirstMatchingRuleByPriorityWins(t *testing.T) {
	rules := []RedirectRule{
		{Priority: 2, LongUrl: "https://example.com/second", Conditions: []RuleCondition{DeviceIs(DeviceAndroid)}},
		{Priority: 1, LongUrl: "https://example.com/first", Conditions: []RuleCondition{DeviceIs(DeviceAndroid)}},
	}
	v := visitor("Android phone", "", nil, "")
	if got := ResolveTarget("https://example.com/default", rules, v); got != "https://example.com/first" {
		t.Errorf("got %q", got)
	}
}

func TestAllConditionsOfARuleMustMatch(t *testing.T) {
	rules := []RedirectRule{
		{Priority: 1, LongUrl: "https://example.com/match",
			Conditions: []RuleCondition{DeviceIs(DeviceAndroid), QueryParamIs("src", "mail")}},
	}
	noParam := visitor("Android", "", nil, "")
	withParam := visitor("Android", "", map[string]string{"src": "mail"}, "")
	if got := ResolveTarget("https://example.com/default", rules, noParam); got != "https://example.com/default" {
		t.Errorf("got %q", got)
	}
	if got := ResolveTarget("https://example.com/default", rules, withParam); got != "https://example.com/match" {
		t.Errorf("got %q", got)
	}
}

func TestMobileMatchesAndroidAndIos(t *testing.T) {
	rules := []RedirectRule{
		{Priority: 1, LongUrl: "https://example.com/mobile", Conditions: []RuleCondition{DeviceIs(DeviceMobile)}},
	}
	if got := ResolveTarget("d", rules, visitor("Android", "", nil, "")); got != "https://example.com/mobile" {
		t.Errorf("android: got %q", got)
	}
	if got := ResolveTarget("d", rules, visitor("iPhone", "", nil, "")); got != "https://example.com/mobile" {
		t.Errorf("iphone: got %q", got)
	}
	if got := ResolveTarget("d", rules, visitor("Macintosh", "", nil, "")); got != "d" {
		t.Errorf("desktop: got %q", got)
	}
}

func TestIpRangeConditionMatchesVisitorIp(t *testing.T) {
	rules := []RedirectRule{
		{Priority: 1, LongUrl: "https://example.com/internal", Conditions: []RuleCondition{IPInRange("10.0.0.0/8")}},
	}
	if got := ResolveTarget("d", rules, visitor("", "", nil, "10.2.3.4")); got != "https://example.com/internal" {
		t.Errorf("internal: got %q", got)
	}
	if got := ResolveTarget("d", rules, visitor("", "", nil, "8.8.8.8")); got != "d" {
		t.Errorf("external: got %q", got)
	}
}

func TestRulesWithoutConditionsNeverFire(t *testing.T) {
	rules := []RedirectRule{{Priority: 1, LongUrl: "https://example.com/never"}}
	if got := ResolveTarget("d", rules, visitor("", "", nil, "")); got != "d" {
		t.Errorf("got %q", got)
	}
}
