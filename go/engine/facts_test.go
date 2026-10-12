package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadFacts(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")
	lines := `{"type":"user","timestamp":"2026-10-01T10:00:00Z","message":{"content":"hi"}}
{"type":"assistant","timestamp":"2026-10-01T10:01:00Z","message":{"id":"m1","model":"claude-x","usage":{"input_tokens":5,"output_tokens":1,"cache_read_input_tokens":90,"cache_creation_input_tokens":5},"content":[{"type":"text","text":"ok"}]}}
{"type":"assistant","timestamp":"2026-10-01T10:01:02Z","message":{"id":"m1","model":"claude-x","usage":{"input_tokens":5,"output_tokens":40,"cache_read_input_tokens":90,"cache_creation_input_tokens":5},"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/w/a.go"}},{"type":"tool_use","name":"Bash","input":{"command":"go test ./...\nmore"}}]}}
{"type":"assistant","timestamp":"2026-10-01T10:03:00Z","message":{"id":"m2","model":"claude-x","usage":{"input_tokens":1,"output_tokens":2},"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/w/a.go"}},{"type":"tool_use","name":"Read","input":{"file_path":"/other/b.md"}}]}}
{"type":"user","timestamp":"2026-10-01T12:00:00Z","message":{"content":"much later"}}
`
	if err := os.WriteFile(p, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	f := ReadFacts(p, "/w")
	if f.Turns != 2 || f.Out != 42 || f.In != 6 || f.CacheRead != 90 {
		t.Errorf("usage should count each message once, taking its last line: %+v", f)
	}
	if f.ActiveMin != 3 {
		t.Errorf("active minutes = %d, want 3 (the 2h gap is idle)", f.ActiveMin)
	}
	if len(f.Files) != 2 || f.Files[0].Rel != "a.go" || f.Files[0].Edits != 1 || f.Files[0].Reads != 1 || f.Files[1].Rel != "/other/b.md" {
		t.Errorf("files = %+v", f.Files)
	}
	if f.CommandCount != 1 || len(f.Commands) != 1 || f.Commands[0] != "go test ./..." {
		t.Errorf("commands = %v (%d)", f.Commands, f.CommandCount)
	}
	if f.Model != "claude-x" || f.CacheHit() != 89 {
		t.Errorf("model %q cacheHit %d", f.Model, f.CacheHit())
	}
}
