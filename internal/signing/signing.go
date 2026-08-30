package signing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"hookline/internal/domain"
)

func Sign(payload []byte, secret string, ts time.Time) string {
	stamp := strconv.FormatInt(ts.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(stamp + "."))
	_, _ = mac.Write(payload)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func Verify(payload []byte, sig, stamp, secret string, now time.Time, tolerance time.Duration) error {
	unix, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil || tolerance < 0 {
		return domain.ErrInvalidSignature
	}
	age := now.Sub(time.Unix(unix, 0))
	if age < 0 {
		age = -age
	}
	if age > tolerance {
		return domain.ErrSignatureExpired
	}
	if !strings.HasPrefix(sig, "v1=") || len(sig) != 67 {
		return domain.ErrInvalidSignature
	}
	if !hmac.Equal([]byte(Sign(payload, secret, time.Unix(unix, 0))), []byte(sig)) {
		return domain.ErrInvalidSignature
	}
	return nil
}

func VerifyGitHub(payload []byte, sig, secret string) error {
	if !strings.HasPrefix(sig, "sha256=") || len(sig) != 71 {
		return domain.ErrInvalidSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return domain.ErrInvalidSignature
	}
	return nil
}
