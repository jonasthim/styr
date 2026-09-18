package crypto

import (
	"bytes"
	"strings"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	b, err := NewBox(strings.Repeat("a", 32))
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	plain := []byte("sk-ant-oat01-super-secret-token")
	ciphertext, nonce, err := b.Seal(plain)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	got, err := b.Open(ciphertext, nonce)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("round trip mismatch: got %q, want %q", got, plain)
	}
}

func TestOpenWithWrongKeyFails(t *testing.T) {
	b1, err := NewBox(strings.Repeat("a", 32))
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	b2, err := NewBox(strings.Repeat("b", 32))
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	ciphertext, nonce, err := b1.Seal([]byte("hello"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := b2.Open(ciphertext, nonce); err == nil {
		t.Fatal("expected error opening with wrong key")
	}
}

func TestOpenWithTamperedCiphertextFails(t *testing.T) {
	b, err := NewBox(strings.Repeat("a", 32))
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	ciphertext, nonce, err := b.Seal([]byte("hello"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	tampered := append([]byte(nil), ciphertext...)
	tampered[0] ^= 0xFF
	if _, err := b.Open(tampered, nonce); err == nil {
		t.Fatal("expected error opening tampered ciphertext")
	}
}

func TestNewBoxRejectsShortSecret(t *testing.T) {
	if _, err := NewBox(strings.Repeat("a", 31)); err == nil {
		t.Fatal("expected error for secret shorter than 32 bytes")
	}
}

func TestSuffix(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"sk-ant-oat01-abcdef", "abcdef"},
		{"ab", "ab"},
	}
	for _, c := range cases {
		if got := Suffix(c.in); got != c.want {
			t.Fatalf("Suffix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
