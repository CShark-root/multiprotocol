package auth

import (
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B, SHA-1 secret "12345678901234567890", truncated to the
// 6 digits authenticator apps show.
func TestTOTPRFCVectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for unix, want := range map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"} {
		if got := totpCode(secret, unix/totpPeriod); got != want {
			t.Errorf("t=%d: got %s, want %s", unix, got, want)
		}
	}
}

func TestVerifyTOTPWindow(t *testing.T) {
	secret := NewTOTPSecret()
	key, _ := b32.DecodeString(secret)
	now := time.Unix(1_700_000_000, 0)
	cur := now.Unix() / totpPeriod
	if _, ok := verifyTOTP(secret, totpCode(key, cur-1), now); !ok {
		t.Error("previous step must be accepted (clock skew)")
	}
	if _, ok := verifyTOTP(secret, totpCode(key, cur-3), now); ok {
		t.Error("old codes must be rejected")
	}
	if _, ok := verifyTOTP(secret, "12345", now); ok {
		t.Error("wrong length must be rejected")
	}
	if !strings.HasPrefix(TOTPURI(secret, "admin", "MultiProtocol"), "otpauth://totp/MultiProtocol:admin?") {
		t.Error("bad otpauth URI")
	}
}
