package core

import (
	"sort"
	"strings"
)

// DetectDevice is lightweight device detection from a user-agent string; it
// only needs to be accurate enough to drive device-condition redirect rules.
func DetectDevice(userAgent string) Device {
	if userAgent == "" {
		return DeviceDesktop
	}
	ua := strings.ToLower(userAgent)
	switch {
	case strings.Contains(ua, "android"):
		return DeviceAndroid
	case strings.Contains(ua, "iphone"), strings.Contains(ua, "ipad"), strings.Contains(ua, "ipod"):
		return DeviceIos
	case strings.Contains(ua, "mobile"):
		return DeviceMobile
	default:
		return DeviceDesktop
	}
}

// MatchesLanguage reports whether the Accept-Language header includes the
// wanted language. Matches on the primary subtag: wanting "en" matches
// "en-GB"; wanting "en-US" requires "en-US" (case-insensitive).
func MatchesLanguage(wanted, acceptLanguage string) bool {
	if acceptLanguage == "" {
		return false
	}
	wanted = strings.ToLower(strings.TrimSpace(wanted))
	for _, part := range strings.Split(acceptLanguage, ",") {
		lang := strings.ToLower(strings.TrimSpace(strings.Split(part, ";")[0]))
		if lang == "" || lang == "*" {
			continue
		}
		if lang == wanted {
			return true
		}
		if !strings.Contains(wanted, "-") && strings.Split(lang, "-")[0] == wanted {
			return true
		}
	}
	return false
}

func matchesCondition(visitor VisitorContext, cond RuleCondition) bool {
	switch cond.Type {
	case CondDevice:
		wanted, ok := DeviceOfSlug(cond.Value)
		if !ok {
			return false
		}
		device := DetectDevice(visitor.UserAgent)
		if wanted == DeviceMobile {
			return device == DeviceAndroid || device == DeviceIos || device == DeviceMobile
		}
		return wanted == device
	case CondLanguage:
		return MatchesLanguage(cond.Value, visitor.AcceptLanguage)
	case CondQueryParam:
		v, ok := visitor.Query[cond.Key]
		return ok && v == cond.Value
	case CondIPAddress:
		return visitor.RemoteIP != "" && IPInCidr(cond.Value, visitor.RemoteIP)
	default:
		return false
	}
}

// ResolveTarget resolves the target long URL for a visit: the first rule (by
// priority) whose conditions all match wins; otherwise the default long URL.
func ResolveTarget(defaultLongUrl string, rules []RedirectRule, visitor VisitorContext) string {
	sorted := make([]RedirectRule, len(rules))
	copy(sorted, rules)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Priority < sorted[j].Priority })

	for _, rule := range sorted {
		if len(rule.Conditions) == 0 {
			continue
		}
		all := true
		for _, cond := range rule.Conditions {
			if !matchesCondition(visitor, cond) {
				all = false
				break
			}
		}
		if all {
			return rule.LongUrl
		}
	}
	return defaultLongUrl
}
