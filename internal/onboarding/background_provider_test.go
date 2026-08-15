package onboarding

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestBackgroundProviderVerify(t *testing.T) {
	p := NewBackgroundProvider("test", "https://example.invalid", "key", "secret")
	payload := []byte(`{"event_id":"evt_1"}`)
	mac := hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write(payload)
	signature := hex.EncodeToString(mac.Sum(nil))
	if !p.Verify(payload, signature) {
		t.Fatal("expected valid signature")
	}
	if !p.Verify(payload, "sha256="+signature) {
		t.Fatal("expected prefixed signature")
	}
	if p.Verify([]byte("tampered"), signature) {
		t.Fatal("accepted tampered payload")
	}
}
