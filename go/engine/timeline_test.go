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

func TestTimelineArtifacts(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "walk.html")
	os.WriteFile(src, []byte("<html>"), 0o644)
	lines := []string{
		`{"type":"user","timestamp":"2026-10-02T17:00:00Z","message":{"content":"make a page"}}`,
		`{"type":"frame-link","path":"` + src + `","frameUrl":"https://claude.ai/artifact/abc","title":"Walkthrough","artifactCount":1,"timestamp":"2026-10-02T17:12:20Z"}`,
		`{"type":"frame-link","artifactCount":1,"timestamp":"2026-10-02T17:20:23Z"}`,
		`{"type":"frame-link","path":"/gone.html","frameUrl":"https://claude.ai/artifact/abc","title":"Walkthrough","timestamp":"2026-10-02T17:30:00Z"}`,
		`{"type":"frame-link","path":"/gone2.html","frameUrl":"https://claude.ai/artifact/def","title":"Second","timestamp":"2026-10-02T17:40:00Z"}`,
	}
	p := filepath.Join(dir, "s.jsonl")
	os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	tl := ReadTimeline(p)
	if len(tl.Artifacts) != 2 {
		t.Fatalf("want 2 artifacts (a repeat and a bare count line are skipped), got %d: %+v", len(tl.Artifacts), tl.Artifacts)
	}
	a, b := tl.Artifacts[0], tl.Artifacts[1]
	if a.Title != "Walkthrough" || a.URL != "https://claude.ai/artifact/abc" || a.File != src {
		t.Errorf("first artifact = %+v", a)
	}
	if b.File != "" {
		t.Errorf("a source file that no longer exists must not be offered: %+v", b)
	}
	n := 0
	for _, e := range tl.Events {
		if e.Kind == EvArtifact {
			n++
			if !strings.HasPrefix(e.Detail, "https://claude.ai/artifact/") {
				t.Errorf("event should carry the link: %+v", e)
			}
		}
	}
	if n != 2 {
		t.Errorf("want 2 artifact events, got %d", n)
	}
}

func TestTimelineKeepsEveryPR(t *testing.T) {
	lines := []string{
		`{"type":"user","timestamp":"2026-10-01T10:00:00Z","message":{"content":"go"}}`,
		`{"type":"pr-link","timestamp":"2026-10-01T10:01:00Z","prNumber":7,"prUrl":"https://github.com/o/a/pull/7"}`,
		`{"type":"pr-link","timestamp":"2026-10-01T10:02:00Z","prNumber":7,"prUrl":"https://github.com/o/a/pull/7"}`,
		`{"type":"pr-link","timestamp":"2026-10-01T10:03:00Z","prNumber":7,"prUrl":"https://github.com/o/b/pull/7"}`,
		`{"type":"pr-link","timestamp":"2026-10-01T10:04:00Z","prNumber":9,"prUrl":"https://github.com/o/a/pull/9"}`,
	}
	p := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	tl := ReadTimeline(p)
	if len(tl.PRs) != 3 {
		t.Fatalf("want 3 PRs (a repeat is dropped, the same number in another repo is kept), got %+v", tl.PRs)
	}
	if tl.PRs[0].URL != "https://github.com/o/a/pull/7" || tl.PRs[2].Number != 9 {
		t.Errorf("order or content wrong: %+v", tl.PRs)
	}
}
