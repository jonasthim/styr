package db

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/jonasthim/styr/internal/domain"
)

func TestTokens_SetGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	tokens := NewTokens(testOpenDB(t))

	ciphertext := []byte{1, 2, 3, 4}
	nonce := []byte{5, 6, 7}
	if err := tokens.Set(ctx, "user-1", ciphertext, nonce, "…a1b2c3"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	gotCipher, gotNonce, label, verifiedAt, err := tokens.Get(ctx, "user-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(gotCipher, ciphertext) {
		t.Fatalf("ciphertext = %v, want %v", gotCipher, ciphertext)
	}
	if !bytes.Equal(gotNonce, nonce) {
		t.Fatalf("nonce = %v, want %v", gotNonce, nonce)
	}
	if label != "…a1b2c3" {
		t.Fatalf("label = %q, want …a1b2c3", label)
	}
	if verifiedAt != nil {
		t.Fatalf("verifiedAt = %v, want nil before MarkVerified", verifiedAt)
	}
}

func TestTokens_MarkVerifiedAndDelete(t *testing.T) {
	ctx := context.Background()
	tokens := NewTokens(testOpenDB(t))

	if err := tokens.Set(ctx, "user-1", []byte("c"), []byte("n"), "label"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := tokens.MarkVerified(ctx, "user-1"); err != nil {
		t.Fatalf("MarkVerified: %v", err)
	}
	_, _, _, verifiedAt, err := tokens.Get(ctx, "user-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if verifiedAt == nil {
		t.Fatalf("verifiedAt = nil, want set")
	}

	if err := tokens.Delete(ctx, "user-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, _, _, _, err = tokens.Get(ctx, "user-1")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get after delete: err = %v, want ErrNotFound", err)
	}
}

func TestTokens_ServiceToken(t *testing.T) {
	ctx := context.Background()
	tokens := NewTokens(testOpenDB(t))

	if err := tokens.SetService(ctx, []byte("sc"), []byte("sn"), "svc"); err != nil {
		t.Fatalf("SetService: %v", err)
	}
	cipher, nonce, label, _, err := tokens.GetService(ctx)
	if err != nil {
		t.Fatalf("GetService: %v", err)
	}
	if !bytes.Equal(cipher, []byte("sc")) || !bytes.Equal(nonce, []byte("sn")) || label != "svc" {
		t.Fatalf("GetService = (%v, %v, %q), want (sc, sn, svc)", cipher, nonce, label)
	}
}
