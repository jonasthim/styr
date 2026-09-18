package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeStub writes an executable shell script named "claude" into t.TempDir()
// and returns its path. body is the script's shell body (after the shebang).
func writeStub(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	script := "#!/usr/bin/env bash\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVerifierSuccess(t *testing.T) {
	bin := writeStub(t, `echo '{"type":"result","subtype":"success","is_error":false,"result":"pong"}'`)
	v := Verifier{Bin: bin, Timeout: 5 * time.Second}
	if err := v.Verify(context.Background(), "sk-ant-oat01-test-token"); err != nil {
		t.Fatalf("Verify() = %v, want nil", err)
	}
}

func TestVerifierIsErrorTrue(t *testing.T) {
	bin := writeStub(t, `echo '{"type":"result","subtype":"error","is_error":true,"result":"invalid token"}'`)
	v := Verifier{Bin: bin, Timeout: 5 * time.Second}
	err := v.Verify(context.Background(), "sk-ant-oat01-bad-token")
	if err == nil {
		t.Fatal("Verify() = nil, want error")
	}
	if strings.Contains(err.Error(), "sk-ant-oat01-bad-token") {
		t.Fatalf("error must not contain the token: %v", err)
	}
}

func TestVerifierNonZeroExit(t *testing.T) {
	bin := writeStub(t, `echo 'boom' >&2; exit 1`)
	v := Verifier{Bin: bin, Timeout: 5 * time.Second}
	err := v.Verify(context.Background(), "sk-ant-oat01-secret-value")
	if err == nil {
		t.Fatal("Verify() = nil, want error")
	}
	if strings.Contains(err.Error(), "sk-ant-oat01-secret-value") {
		t.Fatalf("error must not contain the token: %v", err)
	}
}

func TestVerifierUsesFreshHomeAndPassesToken(t *testing.T) {
	// The stub script inspects its own environment and reports back via the
	// JSON result, so the test can assert Verify wired CLAUDE_CODE_OAUTH_TOKEN
	// and a HOME distinct from the test's own HOME without ever logging the
	// token itself.
	bin := writeStub(t, `
if [ "$CLAUDE_CODE_OAUTH_TOKEN" != "sk-ant-oat01-marker" ]; then
  echo '{"type":"result","is_error":true,"result":"missing token"}'
  exit 0
fi
if [ -z "$HOME" ] || [ "$HOME" = "`+os.Getenv("HOME")+`" ]; then
  echo '{"type":"result","is_error":true,"result":"home not overridden"}'
  exit 0
fi
if [ ! -d "$HOME" ]; then
  echo '{"type":"result","is_error":true,"result":"home does not exist"}'
  exit 0
fi
echo '{"type":"result","is_error":false,"result":"pong"}'
`)
	v := Verifier{Bin: bin, Timeout: 5 * time.Second}
	if err := v.Verify(context.Background(), "sk-ant-oat01-marker"); err != nil {
		t.Fatalf("Verify() = %v, want nil", err)
	}
}

func TestVerifierDefaultTimeout(t *testing.T) {
	v := Verifier{Bin: "/does/not/matter"}
	if v.timeout() != defaultVerifyTimeout {
		t.Fatalf("timeout() = %v, want %v", v.timeout(), defaultVerifyTimeout)
	}
	v2 := Verifier{Bin: "/does/not/matter", Timeout: 3 * time.Second}
	if v2.timeout() != 3*time.Second {
		t.Fatalf("timeout() = %v, want 3s", v2.timeout())
	}
}
