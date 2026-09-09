package core

import (
	"net"
	"strconv"
	"strings"
)

// AnonymizeIP zeroes the host part of an address: last octet for IPv4,
// everything beyond the /48 prefix for IPv6. Returns "" for unparseable input.
func AnonymizeIP(ip string) string {
	addr := net.ParseIP(ip)
	if addr == nil {
		return ""
	}
	if v4 := addr.To4(); v4 != nil {
		bytes := make(net.IP, len(v4))
		copy(bytes, v4)
		bytes[3] = 0
		return bytes.String()
	}
	v6 := addr.To16()
	bytes := make(net.IP, len(v6))
	copy(bytes, v6)
	for i := 6; i < len(bytes); i++ {
		bytes[i] = 0
	}
	return bytes.String()
}

// IPInCidr checks whether an IP falls in a CIDR range (used by redirect
// rules). A bare address means exact match. Mixed address families never
// match.
func IPInCIDR(cidr, ip string) bool {
	addr := net.ParseIP(ip)
	if addr == nil {
		return false
	}
	parts := strings.Split(cidr, "/")
	switch len(parts) {
	case 2:
		network := net.ParseIP(parts[0])
		prefix, err := strconv.Atoi(parts[1])
		if network == nil || err != nil {
			return false
		}
		// Normalize both to the same representation so families compare.
		netBytes, addrBytes := canonical(network), canonical(addr)
		if len(netBytes) != len(addrBytes) || prefix < 0 || prefix > len(netBytes)*8 {
			return false
		}
		fullBytes := prefix / 8
		remainder := prefix % 8
		for i := 0; i < fullBytes; i++ {
			if netBytes[i] != addrBytes[i] {
				return false
			}
		}
		if remainder != 0 && fullBytes < len(netBytes) {
			mask := byte(0xFF) << (8 - remainder)
			if netBytes[fullBytes]&mask != addrBytes[fullBytes]&mask {
				return false
			}
		}
		return true
	case 1:
		other := net.ParseIP(parts[0])
		if other == nil {
			return false
		}
		na, nb := canonical(other), canonical(addr)
		return len(na) == len(nb) && other.Equal(addr)
	default:
		return false
	}
}

// canonical returns the 4-byte form for IPv4 addresses and the 16-byte form
// for IPv6, mirroring how address families are compared byte-wise.
func canonical(ip net.IP) []byte {
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip.To16()
}
