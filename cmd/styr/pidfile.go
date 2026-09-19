package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// pidFileName is the file styr serve writes under its data directory while
// it runs: <data_dir>/styr.pid, containing the serving process's PID as
// decimal text. backup, restore and doctor read it to tell whether a
// `styr serve` process is currently up against this data_dir, without any
// network call or database lock.
const pidFileName = "styr.pid"

// pidFilePath returns the pid file's path under dataDir.
func pidFilePath(dataDir string) string { return filepath.Join(dataDir, pidFileName) }

// writePIDFile writes the current process's PID to path, creating or
// truncating it. serve calls this once at startup and removes it again on
// shutdown (see removePIDFile).
func writePIDFile(path string) error {
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		return fmt.Errorf("write pid file %s: %w", path, err)
	}
	return nil
}

// removePIDFile deletes the pid file at path. A missing file is not an
// error: shutdown must not fail just because the file was already gone
// (someone removed it by hand, or it was never written).
func removePIDFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove pid file %s: %w", path, err)
	}
	return nil
}

// readPIDFile reads and parses the PID stored at path. ok is false (with a
// nil error) when the file does not exist; a present-but-unparsable file is
// reported as an error so callers can surface it rather than silently
// treating a corrupt pid file as "not running".
func readPIDFile(path string) (pid int, ok bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("read pid file %s: %w", path, err)
	}
	pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false, fmt.Errorf("parse pid file %s: %w", path, err)
	}
	return pid, true, nil
}

// pidLive reports whether pid names a live process, the same check
// `kill -0 <pid>` performs on the command line: os.FindProcess always
// succeeds on Unix, so the real test is sending signal 0, which the kernel
// validates (permission, existence) without delivering anything.
func pidLive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// serverRunning reports whether a styr serve process appears to be running
// against dataDir: its pid file exists, parses, and names a live process. A
// stale pid file (the process is gone) is reported as not running, not as
// an error.
func serverRunning(dataDir string) (pid int, running bool, err error) {
	pid, ok, err := readPIDFile(pidFilePath(dataDir))
	if err != nil || !ok {
		return 0, false, err
	}
	return pid, pidLive(pid), nil
}
