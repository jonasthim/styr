package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_IdenticalFilesExitsZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openapi.yaml")
	writeFile(t, path, baseFixture)

	var stdout, stderr bytes.Buffer
	code := run([]string{path, path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "OK: no breaking changes") {
		t.Errorf("stdout = %q, want it to report no breaking changes", stdout.String())
	}
}

func TestRun_BreakingChangeExitsOne(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.yaml")
	newPath := filepath.Join(dir, "new.yaml")
	writeFile(t, oldPath, baseFixture)
	writeFile(t, newPath, `
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: OK
components:
  schemas:
    Widget:
      type: object
      properties:
        id:
          type: string
`)

	var stdout, stderr bytes.Buffer
	code := run([]string{oldPath, newPath}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run() = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "BREAKING:") {
		t.Errorf("stderr = %q, want it to list at least one BREAKING change", stderr.String())
	}
}

func TestRun_MissingFileExitsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"/no/such/file.yaml", "/no/such/file.yaml"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run() = %d, want 2", code)
	}
}

func TestRun_WrongArgCountExitsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"one.yaml"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run() = %d, want 2", code)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
}
