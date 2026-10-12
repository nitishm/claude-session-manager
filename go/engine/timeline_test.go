package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadTimeline(t *testing.T) {
	lines := []string{
		`{"type":"user","timestamp":"2026-10-01T10:00:00Z","message":{"content":"fix the bug"}}`,
		`{"type":"assistant","timestamp":"2026-10-01T10:00:05Z","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/x/a.go"}},{"type":"tool_use","name":"Bash","input":{"command":"git commit -m \"fix bug\""}}]}}`,
		`{"type":"user","timestamp":"2026-10-01T10:00:06Z","message":{"content":[{"type":"tool_result","is_error":true,"content":"boom"}]}}`,
		`{"type":"system","subtype":"turn_duration","timestamp":"2026-10-01T10:00:07Z","durationMs":7000}`,
		`{"type":"assistant","timestamp":"2026-10-01T10:00:08Z","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"git push origin main"}}]}}`,
		`{"type":"pr-link","timestamp":"2026-10-01T10:00:09Z","prNumber":7,"prUrl":"https://github.com/o/r/pull/7"}`,
		`{"type":"system","subtype":"compact_boundary","timestamp":"2026-10-01T10:01:00Z","compactMetadata":{"trigger":"auto","preTokens":200000,"postTokens":20000}}`,
		`{"type":"user","timestamp":"2026-10-01T13:00:00Z","message":{"content":"now the docs"}}`,
		`{"type":"user","isSidechain":true,"timestamp":"2026-10-01T13:00:01Z","message":{"content":"subagent prompt"}}`,
	}
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ev := ReadTimeline(p).Events
	var kinds []string
	for _, e := range ev {
		kinds = append(kinds, e.Kind)
	}
	if got, want := strings.Join(kinds, ","), "prompt,commit,push,pr,compact,gap,prompt"; got != want {
		t.Fatalf("kinds %s, want %s", got, want)
	}
	if p0 := ev[0]; p0.Tools != 3 || p0.Edits != 1 || p0.Errors != 1 || p0.Secs != 7 || p0.N != 1 {
		t.Errorf("turn stats %+v", p0)
	}
	if ev[1].Text != "fix bug" {
		t.Errorf("commit text %q", ev[1].Text)
	}
	if ev[3].Detail != "https://github.com/o/r/pull/7" || ev[4].Detail != "auto, 200.0k to 20.0k tokens" {
		t.Errorf("details %q %q", ev[3].Detail, ev[4].Detail)
	}
	if ev[5].Text != "idle 2h 59m" || ev[6].N != 2 {
		t.Errorf("gap/prompt %+v %+v", ev[5], ev[6])
	}
}

func TestCommitText(t *testing.T) {
	if got := commitText("git commit -m \"$(cat <<'EOF'\nAdd thing\n\nbody\nEOF\n)\"", "x"); got != "Add thing" {
		t.Errorf("heredoc: %q", got)
	}
	if got := commitText("jj describe -m 'cat tools'", "x"); got != "cat tools" {
		t.Errorf("plain: %q", got)
	}
}
