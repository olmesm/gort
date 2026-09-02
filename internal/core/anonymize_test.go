package core

import "testing"

func TestIpv4AddressesLoseTheirLastOctet(t *testing.T) {
	if got := AnonymizeIP("192.168.1.42"); got != "192.168.1.0" {
		t.Errorf("got %q", got)
	}
}

func TestIpv6AddressesAreTruncatedToTheir48Prefix(t *testing.T) {
	if got := AnonymizeIP("2001:db8:1:2:3:4:5:6"); got != "2001:db8:1::" {
		t.Errorf("got %q", got)
	}
}

func TestGarbageInputYieldsEmpty(t *testing.T) {
	if got := AnonymizeIP("not-an-ip"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestCidrMatching(t *testing.T) {
	cases := []struct {
		cidr     string
		ip       string
		expected bool
	}{
		{"10.0.0.0/8", "10.1.2.3", true},
		{"10.0.0.0/8", "11.0.0.1", false},
		{"192.168.1.0/24", "192.168.1.200", true},
		{"192.168.1.0/24", "192.168.2.1", false},
		{"192.168.1.128/25", "192.168.1.129", true},
		{"192.168.1.128/25", "192.168.1.1", false},
		{"192.168.1.5", "192.168.1.5", true},
		{"192.168.1.5", "192.168.1.6", false},
		{"2001:db8::/32", "2001:db8:ffff::1", true},
		{"2001:db8::/32", "2001:db9::1", false},
	}
	for _, tc := range cases {
		if got := IPInCidr(tc.cidr, tc.ip); got != tc.expected {
			t.Errorf("IPInCidr(%q, %q) = %v, want %v", tc.cidr, tc.ip, got, tc.expected)
		}
	}
}

func TestMixedFamiliesNeverMatch(t *testing.T) {
	if IPInCidr("10.0.0.0/8", "2001:db8::1") {
		t.Error("ipv6 address must not match ipv4 cidr")
	}
}
