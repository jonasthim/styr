package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestGenerateAPIToken_Shape(t *testing.T) {
	raw, hash, prefix, err := GenerateAPIToken()
	if err != nil {
		t.Fatalf("GenerateAPIToken: %v", err)
	}
	if !strings.HasPrefix(raw, APITokenPrefix) {
		t.Errorf("raw = %q, want prefix %q", raw, APITokenPrefix)
	}
	if len(raw) <= len(APITokenPrefix) {
		t.Errorf("raw = %q, want more than just the prefix", raw)
	}
	if len(prefix) != apiTokenPrefixLen {
		t.Errorf("prefix = %q (len %d), want len %d", prefix, len(prefix), apiTokenPrefixLen)
	}
	if !strings.HasPrefix(raw, prefix) {
		t.Errorf("prefix %q is not a prefix of raw %q", prefix, raw)
	}
	sum := sha256.Sum256([]byte(raw))
	wantHash := hex.EncodeToString(sum[:])
	if hash != wantHash {
		t.Errorf("hash = %q, want sha256 hex %q", hash, wantHash)
	}
}

func TestGenerateAPIToken_Unique(t *testing.T) {
	raw1, hash1, _, err := GenerateAPIToken()
	if err != nil {
		t.Fatalf("GenerateAPIToken: %v", err)
	}
	raw2, hash2, _, err := GenerateAPIToken()
	if err != nil {
		t.Fatalf("GenerateAPIToken: %v", err)
	}
	if raw1 == raw2 {
		t.Errorf("two calls returned the same raw token: %q", raw1)
	}
	if hash1 == hash2 {
		t.Errorf("two calls returned the same hash: %q", hash1)
	}
}
