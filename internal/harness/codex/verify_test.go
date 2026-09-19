package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeVerifyStub writes an executable shell script named "codex" into t.TempDir() and returns
// its path. body is the script's shell body (after the shebang).
func writeVerifyStub(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "codex")
	script := "#!/usr/bin/env bash\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVerifier_Success(t *testing.T) {
	bin := writeVerifyStub(t, `echo '{"type":"item.completed","item":{"type":"agent_message","text":"pong"}}'`)
	v := Verifier{Bin: bin, Timeout: 5 * time.Second}
	if err := v.Verify(context.Background(), "sk-test-key"); err != nil {
		t.Fatalf("Verify() = %v, want nil", err)
	}
}

func TestVerifier_NonZeroExitIsRejection(t *testing.T) {
	bin := writeVerifyStub(t, `echo 'stream error: 401 Unauthorized' >&2; exit 1`)
	v := Verifier{Bin: bin, Timeout: 5 * time.Second}
	err := v.Verify(context.Background(), "sk-secret-value")
	if err == nil {
		t.Fatal("Verify() = nil, want error")
	}
	if !strings.Contains(err.Error(), "401 Unauthorized") {
		t.Fatalf("error should quote the CLI's last line, got %v", err)
	}
	if strings.Contains(err.Error(), "sk-secret-value") {
		t.Fatalf("error must not contain the key: %v", err)
	}
}

// A CLI that echoes the key back must not leak it through the error message.
func TestVerifier_RedactsKeyFromOutput(t *testing.T) {
	bin := writeVerifyStub(t, `echo "rejected key $OPENAI_API_KEY" >&2; exit 2`)
	v := Verifier{Bin: bin, Timeout: 5 * time.Second}
	err := v.Verify(context.Background(), "sk-leaky-key-value")
	if err == nil {
		t.Fatal("Verify() = nil, want error")
	}
	if strings.Contains(err.Error(), "sk-leaky-key-value") {
		t.Fatalf("error must not contain the key: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("error should show the redaction marker, got %v", err)
	}
}

func TestVerifier_MissingBinary(t *testing.T) {
	v := Verifier{Bin: filepath.Join(t.TempDir(), "not-there"), Timeout: 5 * time.Second}
	err := v.Verify(context.Background(), "sk-test-key")
	if err == nil {
		t.Fatal("Verify() = nil, want error")
	}
	if !strings.Contains(err.Error(), "could not start") {
		t.Fatalf("error = %v, want a start failure", err)
	}
}

func TestVerifier_Timeout(t *testing.T) {
	bin := writeVerifyStub(t, `sleep 5`)
	v := Verifier{Bin: bin, Timeout: 100 * time.Millisecond}
	err := v.Verify(context.Background(), "sk-test-key")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Verify() = %v, want a timeout error", err)
	}
}

// The child gets the key in OPENAI_API_KEY, a HOME of its own, and the read-only sandbox with
// an empty working directory: the stub writes what it saw into a file the test reads back, so
// nothing is asserted by logging the key.
func TestVerifier_ArgvAndEnvironment(t *testing.T) {
	out := filepath.Join(t.TempDir(), "seen")
	bin := writeVerifyStub(t, `{ printf '%s\n' "$@"; echo "HOME=$HOME"; echo "KEYSET=${OPENAI_API_KEY:+yes}"; echo "PWDFILES=$(ls -A . | wc -l)"; } > `+out)

	v := Verifier{Bin: bin, Timeout: 5 * time.Second}
	if err := v.Verify(context.Background(), "sk-test-key"); err != nil {
		t.Fatalf("Verify() = %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read stub output: %v", err)
	}
	got := string(b)
	for _, want := range []string{"exec", "--skip-git-repo-check", "--sandbox", "read-only", "-C", "Reply with pong.", "KEYSET=yes", "PWDFILES=0"} {
		if !strings.Contains(got, want) {
			t.Fatalf("stub saw %q, missing %q", got, want)
		}
	}
	if home, _ := os.UserHomeDir(); home != "" && strings.Contains(got, "HOME="+home+"\n") {
		t.Fatalf("Verify ran with the operator's own HOME: %q", got)
	}
	if strings.Contains(got, "sk-test-key") {
		t.Fatalf("the key must not reach argv: %q", got)
	}
	// Neither bypass flag can appear: they are not in the argv this package builds.
	if strings.Contains(got, "danger") {
		t.Fatalf("argv contained a dangerous sandbox mode: %q", got)
	}
}
