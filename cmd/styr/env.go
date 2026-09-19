package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// defaultEnvFile is what the systemd unit passes as EnvironmentFile. When a
// subcommand runs by hand (doctor, backup, restore, migrate, or serve from a
// shell) the secrets in it are not in the environment, so every subcommand
// that loads the configuration calls loadEnv first. Variables that are
// already set always win, so under systemd this is a no-op.
const defaultEnvFile = "/etc/styr/env"

// envFilePath is STYR_ENV_FILE when set, else defaultEnvFile.
func envFilePath() string {
	if p := os.Getenv("STYR_ENV_FILE"); p != "" {
		return p
	}
	return defaultEnvFile
}

// loadEnv loads the env file into the process environment and reports on
// stdout what it did, in the same "note"/"warn" vocabulary doctor uses: a
// note when it set at least one variable, a warning when the file exists
// but this user cannot read it (the classic cause of "secret_key must be
// at least 32 bytes" from a hand-run backup), nothing when the file is
// absent or added nothing.
func loadEnv(stdout io.Writer) {
	envFile := envFilePath()
	if n := loadEnvFile(envFile); n > 0 {
		fmt.Fprintf(stdout, "note loaded %d variable(s) from %s\n", n, envFile)
		return
	}
	if _, statErr := os.Stat(envFile); statErr != nil {
		return
	}
	if _, readErr := os.ReadFile(envFile); readErr != nil {
		fmt.Fprintf(stdout, "warn %s exists but is not readable by this user (%v); secrets from it are missing, run as root or as a member of the file's group\n", envFile, readErr)
	}
}

// loadEnvFile sets KEY=VALUE pairs from path for keys not already set. It
// returns the number of keys it set; a missing or unreadable file is not an
// error. Values may be wrapped in single or double quotes.
func loadEnvFile(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok || k == "" {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		if _, exists := os.LookupEnv(k); exists {
			continue
		}
		if os.Setenv(k, v) == nil {
			n++
		}
	}
	return n
}
