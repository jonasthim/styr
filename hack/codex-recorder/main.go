// Command codex-recorder spawns the OpenAI Codex CLI in `codex exec --json` mode with a
// single prompt, and records the session verbatim: every stdout line goes to the file given
// as -out, every stderr line to "<out>.stderr", and the process exit code is appended to the
// fixture as a final ">>> exit N" line. The recorded files are the fixtures
// internal/harness/codex/testdata/*.jsonl is built from; see that directory's PROTOCOL.md.
//
// Unlike hack/recorder (the Claude Code recorder) this tool never writes to the child's
// stdin: `codex exec` takes exactly one prompt per process and exits when the turn is done,
// so a multi-turn session is recorded as several invocations, the later ones with -resume.
//
// The CLI's two bypass flags -- the one that skips approvals and sandboxing outright, and the
// one that runs hooks without persisted trust -- are never emitted, and this program has no
// flag that could produce them. Their names are deliberately not written out anywhere in this
// repository so that a plain grep for them stays a reliable check.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

func main() {
	out := flag.String("out", "", "fixture path (stdout lines); stderr goes to <out>.stderr")
	cwd := flag.String("cwd", ".", "working directory, passed as -C and used as the child's cwd")
	prompt := flag.String("prompt", "", "prompt, passed as the positional PROMPT argument")
	model := flag.String("model", "", "model passed as -m (empty = omit)")
	sandbox := flag.String("sandbox", "", "sandbox policy passed as --sandbox: read-only|workspace-write (empty = omit)")
	schema := flag.String("schema", "", "JSON schema written to a temp file and passed as --output-schema (empty = omit)")
	skipGit := flag.Bool("skip-git-repo-check", false, "pass --skip-git-repo-check")
	resume := flag.String("resume", "", "thread/session id: record `codex exec resume <id>` instead of a fresh `codex exec`")
	binary := flag.String("binary", "codex", "codex binary")
	flag.Parse()
	if *out == "" || *prompt == "" {
		fmt.Fprintln(os.Stderr, "usage: codex-recorder -out FILE -prompt TEXT [-cwd DIR] [-sandbox MODE] [-schema JSON] [-resume ID]")
		os.Exit(2)
	}

	args := []string{"exec"}
	if *resume != "" {
		args = append(args, "resume", *resume)
	}
	args = append(args, "--json")
	if *resume == "" {
		// `codex exec resume` accepts neither -C nor --sandbox (codex-cli 0.154.0); the child's
		// cwd still selects the workspace, and the resumed session keeps its own policy.
		args = append(args, "-C", *cwd)
		if *sandbox != "" {
			args = append(args, "--sandbox", *sandbox)
		}
	}
	if *model != "" {
		args = append(args, "-m", *model)
	}
	if *schema != "" {
		f, err := os.CreateTemp("", "codex-schema-*.json")
		must(err)
		_, err = f.WriteString(*schema)
		must(err)
		must(f.Close())
		defer os.Remove(f.Name())
		args = append(args, "--output-schema", f.Name())
	}
	if *skipGit {
		args = append(args, "--skip-git-repo-check")
	}
	args = append(args, *prompt)

	fixture, err := os.Create(*out)
	must(err)
	defer fixture.Close()
	errFile, err := os.Create(*out + ".stderr")
	must(err)
	defer errFile.Close()

	fmt.Fprintf(os.Stderr, "codex-recorder: %s %q\n", *binary, args)

	cmd := exec.Command(*binary, args...)
	cmd.Dir = *cwd
	stdout, err := cmd.StdoutPipe()
	must(err)
	stderr, err := cmd.StderrPipe()
	must(err)
	must(cmd.Start())

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); copyLines(fixture, stdout, true) }()
	go func() { defer wg.Done(); copyLines(errFile, stderr, false) }()
	wg.Wait()

	code := 0
	if err := cmd.Wait(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	fmt.Fprintf(fixture, ">>> exit %d\n", code)
	fmt.Fprintf(os.Stderr, "codex-recorder: wrote %s (exit %d)\n", filepath.Base(*out), code)
}

// copyLines streams r into w line by line, flushing after each line so a killed recorder
// still leaves a usable partial fixture. tee echoes stdout lines to the terminal too.
func copyLines(w io.Writer, r io.Reader, tee bool) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		line := sc.Bytes()
		w.Write(line)
		w.Write([]byte("\n"))
		if tee {
			fmt.Fprintf(os.Stderr, "  | %s\n", truncate(string(line), 160))
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
