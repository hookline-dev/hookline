package matcher

import "strings"

// Match reports whether eventType satisfies one subscription pattern.
func Match(eventType, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(eventType, prefix) && len(eventType) > len(prefix)
	}
	return pattern == eventType
}

// MatchAny reports whether eventType satisfies at least one pattern.
func MatchAny(eventType string, patterns []string) bool {
	for _, p := range patterns {
		if Match(eventType, p) {
			return true
		}
	}
	return false
}
