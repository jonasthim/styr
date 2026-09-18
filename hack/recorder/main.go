// Command recorder spawns the Claude Code CLI in stream-json mode, forwards a scripted
// conversation, auto-allows permission requests, and writes every stdout line plus every
// stdin line (prefixed ">>> ") to the fixture file given as -out.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
)

func main() {
	out := flag.String("out", "", "fixture path")
	cwd := flag.String("cwd", ".", "working directory for claude")
	prompts := flag.String("prompts", "", "prompts separated by ||")
	interruptAfter := flag.Duration("interrupt-after", 0, "send interrupt after this delay (0 = never)")
	mode := flag.String("mode", "default", "permission mode")
	jsonSchema := flag.String("json-schema", "", "JSON schema string passed as --json-schema (empty = omit)")
	systemPrompt := flag.String("system-prompt", "", "system prompt string passed as --append-system-prompt (empty = omit)")
	model := flag.String("model", "", "model alias or name passed as --model (empty = omit)")
	effort := flag.String("effort", "", "reasoning effort passed as --effort (empty = omit)")
	flag.Parse()
	if *out == "" || *prompts == "" {
		fmt.Fprintln(os.Stderr, "usage: recorder -out FILE -prompts 'a||b' [-cwd DIR]")
		os.Exit(2)
	}
	f, err := os.Create(*out)
	must(err)
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()

	sid := uuid.NewString()
	args := []string{"-p", "--verbose",
		"--input-format", "stream-json", "--output-format", "stream-json",
		"--include-partial-messages", "--replay-user-messages",
		"--session-id", sid, "--name", "styr-fixture",
		"--permission-mode", *mode, "--permission-prompt-tool", "stdio",
		"--max-turns", "6"}
	if *jsonSchema != "" {
		args = append(args, "--json-schema", *jsonSchema)
	}
	if *systemPrompt != "" {
		args = append(args, "--append-system-prompt", *systemPrompt)
	}
	if *model != "" {
		args = append(args, "--model", *model)
	}
	if *effort != "" {
		args = append(args, "--effort", *effort)
	}
	cmd := exec.Command("claude", args...)
	cmd.Dir = *cwd
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	must(err)
	stdout, err := cmd.StdoutPipe()
	must(err)
	must(cmd.Start())

	write := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, ">>> %s\n", b)
		w.Flush()
		stdin.Write(append(b, '\n'))
	}
	list := strings.Split(*prompts, "||")
	next := 0
	sendNext := func() bool {
		if next >= len(list) {
			return false
		}
		write(map[string]any{
			"type":               "user",
			"message":            map[string]any{"role": "user", "content": list[next]},
			"parent_tool_use_id": nil,
			"session_id":         sid,
		})
		next++
		return true
	}
	sendNext()
	if *interruptAfter > 0 {
		go func() {
			time.Sleep(*interruptAfter)
			write(map[string]any{"type": "control_request", "request_id": uuid.NewString(),
				"request": map[string]any{"subtype": "interrupt"}})
		}()
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		line := sc.Bytes()
		w.Write(line)
		w.WriteByte('\n')
		w.Flush()
		var probe struct {
			Type      string `json:"type"`
			RequestID string `json:"request_id"`
			Request   struct {
				Subtype string `json:"subtype"`
			} `json:"request"`
		}
		if json.Unmarshal(line, &probe) != nil {
			continue
		}
		switch probe.Type {
		case "control_request":
			if probe.Request.Subtype == "can_use_tool" {
				write(map[string]any{"type": "control_response", "response": map[string]any{
					"subtype": "success", "request_id": probe.RequestID,
					"response": map[string]any{"behavior": "allow"},
				}})
			}
		case "result":
			if !sendNext() {
				stdin.Close()
			}
		}
	}
	cmd.Wait()
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
