package db

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

func TestCodexCredentials_SetGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	creds := NewCodexCredentials(testOpenDB(t))

	ciphertext := []byte{9, 8, 7, 6}
	nonce := []byte{5, 4, 3}
	if err := creds.Set(ctx, "user-1", ciphertext, nonce, "…ab12"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	gotCipher, gotNonce, label, addedAt, err := creds.Get(ctx, "user-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(gotCipher, ciphertext) {
		t.Fatalf("ciphertext = %v, want %v", gotCipher, ciphertext)
	}
	if !bytes.Equal(gotNonce, nonce) {
		t.Fatalf("nonce = %v, want %v", gotNonce, nonce)
	}
	if label != "…ab12" {
		t.Fatalf("label = %q, want …ab12", label)
	}
	if addedAt.IsZero() || time.Since(addedAt) > time.Minute {
		t.Fatalf("addedAt = %v, want a recent timestamp", addedAt)
	}
}

func TestCodexCredentials_SetReplaces(t *testing.T) {
	ctx := context.Background()
	creds := NewCodexCredentials(testOpenDB(t))

	if err := creds.Set(ctx, "user-1", []byte{1}, []byte{2}, "…old0"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := creds.Set(ctx, "user-1", []byte{3}, []byte{4}, "…new1"); err != nil {
		t.Fatalf("Set again: %v", err)
	}
	cipher, _, label, _, err := creds.Get(ctx, "user-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(cipher, []byte{3}) || label != "…new1" {
		t.Fatalf("after replace: cipher = %v, label = %q", cipher, label)
	}
}

func TestCodexCredentials_GetMissing(t *testing.T) {
	creds := NewCodexCredentials(testOpenDB(t))
	if _, _, _, _, err := creds.Get(context.Background(), "nobody"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get missing = %v, want ErrNotFound", err)
	}
}

func TestCodexCredentials_Delete(t *testing.T) {
	ctx := context.Background()
	creds := NewCodexCredentials(testOpenDB(t))
	if err := creds.Set(ctx, "user-1", []byte{1}, []byte{2}, "…ab12"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := creds.Delete(ctx, "user-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := creds.Delete(ctx, "user-1"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second Delete = %v, want ErrNotFound", err)
	}
}

// The service-wide key lives in the same table under a sentinel user id, so
// setting it must not disturb a real user's own key.
func TestCodexCredentials_ServiceKeyIsSeparate(t *testing.T) {
	ctx := context.Background()
	creds := NewCodexCredentials(testOpenDB(t))

	if err := creds.Set(ctx, "user-1", []byte{1}, []byte{2}, "…user"); err != nil {
		t.Fatalf("Set user: %v", err)
	}
	if err := creds.SetService(ctx, []byte{7}, []byte{8}, "…svc0"); err != nil {
		t.Fatalf("SetService: %v", err)
	}

	_, _, userLabel, _, err := creds.Get(ctx, "user-1")
	if err != nil {
		t.Fatalf("Get user: %v", err)
	}
	if userLabel != "…user" {
		t.Fatalf("user label = %q", userLabel)
	}
	cipher, _, svcLabel, _, err := creds.GetService(ctx)
	if err != nil {
		t.Fatalf("GetService: %v", err)
	}
	if !bytes.Equal(cipher, []byte{7}) || svcLabel != "…svc0" {
		t.Fatalf("service key = %v/%q", cipher, svcLabel)
	}
}
