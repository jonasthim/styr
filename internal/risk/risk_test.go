package risk

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jonasthim/styr/internal/domain"
)

func in(fields map[string]string) json.RawMessage {
	b, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	return b
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name  string
		tool  string
		input json.RawMessage
		want  domain.RiskTier
	}{
		// Read-tier tools (rule 1)
		{"Read tool", "Read", in(map[string]string{"file_path": "a.go"}), domain.RiskRead},
		{"Glob tool", "Glob", in(map[string]string{"pattern": "*.go"}), domain.RiskRead},
		{"Grep tool", "Grep", in(map[string]string{"pattern": "foo"}), domain.RiskRead},
		{"LS tool", "LS", in(map[string]string{"path": "/tmp"}), domain.RiskRead},
		{"WebSearch tool", "WebSearch", in(map[string]string{"query": "go"}), domain.RiskRead},
		{"TodoWrite tool", "TodoWrite", in(map[string]string{}), domain.RiskRead},
		{"Task tool", "Task", in(map[string]string{}), domain.RiskRead},
		{"AskUserQuestion tool", "AskUserQuestion", in(map[string]string{}), domain.RiskRead},

		// WebFetch (rule 2)
		{"WebFetch tool", "WebFetch", in(map[string]string{"url": "https://example.com"}), domain.RiskExec},

		// Write-tier tools (rule 3)
		{"Edit tool", "Edit", in(map[string]string{"file_path": "a.go"}), domain.RiskWrite},
		{"Write tool", "Write", in(map[string]string{"file_path": "a.go"}), domain.RiskWrite},
		{"MultiEdit tool", "MultiEdit", in(map[string]string{"file_path": "a.go"}), domain.RiskWrite},
		{"NotebookEdit tool", "NotebookEdit", in(map[string]string{"file_path": "a.ipynb"}), domain.RiskWrite},

		// Bash destructive patterns (rule 4, checked before read-only prefixes)
		{"rm -rf", "Bash", in(map[string]string{"command": "rm -rf /tmp/build"}), domain.RiskDestructive},
		{"rm -fr", "Bash", in(map[string]string{"command": "rm -fr /tmp/build"}), domain.RiskDestructive},
		{"git push --force", "Bash", in(map[string]string{"command": "git push origin main --force"}), domain.RiskDestructive},
		{"git reset --hard", "Bash", in(map[string]string{"command": "git reset --hard HEAD~1"}), domain.RiskDestructive},
		{"dd if=", "Bash", in(map[string]string{"command": "dd if=/dev/zero of=/dev/sda"}), domain.RiskDestructive},
		{"mkfs", "Bash", in(map[string]string{"command": "mkfs.ext4 /dev/sdb1"}), domain.RiskDestructive},
		{"shutdown", "Bash", in(map[string]string{"command": "shutdown now"}), domain.RiskDestructive},
		{"reboot", "Bash", in(map[string]string{"command": "reboot"}), domain.RiskDestructive},
		{"pct destroy", "Bash", in(map[string]string{"command": "pct destroy 100"}), domain.RiskDestructive},
		{"pct stop", "Bash", in(map[string]string{"command": "pct stop 100"}), domain.RiskDestructive},
		{"systemctl stop", "Bash", in(map[string]string{"command": "systemctl stop nginx"}), domain.RiskDestructive},
		{"systemctl disable", "Bash", in(map[string]string{"command": "systemctl disable nginx"}), domain.RiskDestructive},
		{"systemctl mask", "Bash", in(map[string]string{"command": "systemctl mask nginx"}), domain.RiskDestructive},
		{"DROP TABLE", "Bash", in(map[string]string{"command": "psql -c 'DROP TABLE users'"}), domain.RiskDestructive},
		{"truncate", "Bash", in(map[string]string{"command": "truncate -s 0 app.log"}), domain.RiskDestructive},
		{"redirect to /dev/sd", "Bash", in(map[string]string{"command": "echo hi > /dev/sda"}), domain.RiskDestructive},
		{"destructive wins over ssh read-only-looking prefix", "Bash", in(map[string]string{"command": "ssh prod rm -rf /var/lib/app"}), domain.RiskDestructive},

		// Bash read-only prefixes (rule 4, second branch)
		{"cat", "Bash", in(map[string]string{"command": "cat foo.txt"}), domain.RiskRead},
		{"ls", "Bash", in(map[string]string{"command": "ls -la"}), domain.RiskRead},
		{"grep", "Bash", in(map[string]string{"command": "grep foo bar.txt"}), domain.RiskRead},
		{"rg", "Bash", in(map[string]string{"command": "rg foo"}), domain.RiskRead},
		{"find", "Bash", in(map[string]string{"command": "find . -name '*.go'"}), domain.RiskRead},
		{"head", "Bash", in(map[string]string{"command": "head -n 20 file.txt"}), domain.RiskRead},
		{"tail", "Bash", in(map[string]string{"command": "tail -f file.txt"}), domain.RiskRead},
		{"less", "Bash", in(map[string]string{"command": "less file.txt"}), domain.RiskRead},
		{"journalctl", "Bash", in(map[string]string{"command": "journalctl -u vector"}), domain.RiskRead},
		{"systemctl status", "Bash", in(map[string]string{"command": "systemctl status nginx"}), domain.RiskRead},
		{"git status", "Bash", in(map[string]string{"command": "git status"}), domain.RiskRead},
		{"git log", "Bash", in(map[string]string{"command": "git log -1"}), domain.RiskRead},
		{"git diff", "Bash", in(map[string]string{"command": "git diff HEAD~1"}), domain.RiskRead},
		{"df", "Bash", in(map[string]string{"command": "df -h"}), domain.RiskRead},
		{"du", "Bash", in(map[string]string{"command": "du -sh ."}), domain.RiskRead},
		{"free", "Bash", in(map[string]string{"command": "free -m"}), domain.RiskRead},
		{"uptime", "Bash", in(map[string]string{"command": "uptime"}), domain.RiskRead},
		{"ps", "Bash", in(map[string]string{"command": "ps aux"}), domain.RiskRead},
		{"top", "Bash", in(map[string]string{"command": "top -bn1"}), domain.RiskRead},
		{"curl", "Bash", in(map[string]string{"command": "curl https://example.com"}), domain.RiskRead},
		{"wget -qO-", "Bash", in(map[string]string{"command": "wget -qO- https://example.com"}), domain.RiskRead},
		{"dig", "Bash", in(map[string]string{"command": "dig example.com"}), domain.RiskRead},
		{"ping", "Bash", in(map[string]string{"command": "ping -c1 example.com"}), domain.RiskRead},
		{"kubectl get", "Bash", in(map[string]string{"command": "kubectl get pods"}), domain.RiskRead},
		{"docker ps", "Bash", in(map[string]string{"command": "docker ps"}), domain.RiskRead},
		{"docker logs", "Bash", in(map[string]string{"command": "docker logs mycontainer"}), domain.RiskRead},
		{"pct list", "Bash", in(map[string]string{"command": "pct list"}), domain.RiskRead},
		{"pct status", "Bash", in(map[string]string{"command": "pct status 100"}), domain.RiskRead},
		{"qm list", "Bash", in(map[string]string{"command": "qm list"}), domain.RiskRead},

		// ssh prefix stripping before read-only prefix matching
		{"ssh user@host prefix", "Bash", in(map[string]string{"command": "ssh user@host cat /var/log/syslog"}), domain.RiskRead},
		{"ssh host prefix", "Bash", in(map[string]string{"command": "ssh myhost git status"}), domain.RiskRead},
		{"ssh -p PORT user@host prefix", "Bash", in(map[string]string{"command": "ssh -p 2222 user@host docker ps"}), domain.RiskRead},

		// Bash exec fallback (rule 4, else branch)
		{"unmatched bash command", "Bash", in(map[string]string{"command": "python script.py"}), domain.RiskExec},

		// Any other tool, including MCP tools (rule 5)
		{"MCP tool", "mcp__homelab__deploy", in(map[string]string{}), domain.RiskExec},
		{"unknown tool", "SomeUnknownTool", in(map[string]string{}), domain.RiskExec},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.tool, tc.input)
			if got != tc.want {
				t.Fatalf("Classify(%q, %s) = %q, want %q", tc.tool, tc.input, got, tc.want)
			}
		})
	}
}

func TestSummary(t *testing.T) {
	longCmd := "echo " + strings.Repeat("x", 200)

	cases := []struct {
		name  string
		tool  string
		input json.RawMessage
		want  string
	}{
		{"Bash short command", "Bash", in(map[string]string{"command": "ls -la"}), "Bash: ls -la"},
		{"Bash long command truncated to 120 chars", "Bash", in(map[string]string{"command": longCmd}), "Bash: " + longCmd[:120]},
		{"Edit uses file_path", "Edit", in(map[string]string{"file_path": "src/auth.go"}), "Edit: src/auth.go"},
		{"Write uses file_path", "Write", in(map[string]string{"file_path": "src/new.go"}), "Write: src/new.go"},
		{"Read uses file_path", "Read", in(map[string]string{"file_path": "README.md"}), "Read: README.md"},
		{"Grep uses pattern", "Grep", in(map[string]string{"pattern": "TODO"}), "Grep: TODO"},
		{"Glob uses pattern", "Glob", in(map[string]string{"pattern": "**/*.go"}), "Glob: **/*.go"},
		{"MCP tool uses tool name as is", "mcp__homelab__deploy", in(map[string]string{}), "mcp__homelab__deploy"},
		{"unknown tool falls back to tool name", "SomeUnknownTool", in(map[string]string{}), "SomeUnknownTool"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Summary(tc.tool, tc.input)
			if got != tc.want {
				t.Fatalf("Summary(%q, %s) = %q, want %q", tc.tool, tc.input, got, tc.want)
			}
			if len(got) > 128 {
				t.Fatalf("summary too long: %d chars", len(got))
			}
		})
	}
}
