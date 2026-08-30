package attempts

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitize(t *testing.T) {
	h := RedactHeaders(map[string]string{"authorization": "secret", "X-Hookline-Signature": "sig", "Safe": "ok"})
	if strings.Contains(strings.Join([]string{h["authorization"], h["X-Hookline-Signature"]}, ""), "secret") || h["Safe"] != "ok" {
		t.Fatal(h)
	}
	for _, n := range []int{-1, 0, 1, 4, 100} {
		if !utf8.ValidString(TruncateUTF8("привет", n)) {
			t.Fatal(n)
		}
	}
}
