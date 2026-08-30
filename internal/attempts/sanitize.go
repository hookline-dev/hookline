package attempts

import "strings"

func RedactHeaders(headers map[string]string) map[string]string {
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		if strings.EqualFold(k, "Authorization") || strings.EqualFold(k, "X-Hookline-Signature") {
			v = "[REDACTED]"
		}
		out[k] = v
	}
	return out
}

func TruncateUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	cut := maxBytes
	for cut > 0 && (value[cut]&0xc0) == 0x80 {
		cut--
	}
	return value[:cut]
}
