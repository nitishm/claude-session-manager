package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRename(t *testing.T) {
	home := t.TempDir()
	e := New(home)
	dir := filepath.Join(home, ".claude", "projects", "p")
	os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "abc.jsonl")
	// no trailing newline on purpose: Rename must not glue its line to the last one
	os.WriteFile(path, []byte(`{"type":"user","uuid":"1","timestamp":"2026-01-01T00:00:00Z","cwd":"/w","message":{"content":"hello there"}}`), 0o644)
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	os.Chtimes(path, old, old)

	s := e.scan(path, nil)
	if s == nil || s.ID != "abc" {
		t.Fatalf("scan: %+v", s)
	}
	if err := e.Rename(s, nil, "  My   new\nname "); err != nil {
		t.Fatal(err)
	}
	if s.Label() != "My new name" {
		t.Fatalf("label %q", s.Label())
	}
	if fi, _ := os.Stat(path); !fi.ModTime().Equal(old) {
		t.Fatalf("mtime changed: %v", fi.ModTime())
	}
	// a fresh scan of the file sees the title, and the user line survived intact
	again := e.scan(path, nil)
	if again.Label() != "My new name" || again.Prompts != 1 {
		t.Fatalf("rescan: %q prompts %d", again.Label(), again.Prompts)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"customTitle":"My new name","sessionId":"abc"`) {
		t.Fatalf("transcript: %s", b)
	}
	// the latest rename wins
	e.Rename(again, nil, "Second")
	if e.scan(path, nil).Label() != "Second" {
		t.Fatal("second rename lost")
	}
}

func TestRenameRefuses(t *testing.T) {
	e := New(t.TempDir())
	s := &Session{ID: "x", Path: "/nope"}
	for _, c := range []struct {
		title string
		live  map[string]LiveInfo
		s     *Session
	}{
		{"", nil, s},
		{strings.Repeat("a", MaxTitle+1), nil, s},
		{"ok", nil, &Session{ID: "x"}},
		{"ok", map[string]LiveInfo{"x": {PID: 1}}, s},
	} {
		if err := e.Rename(c.s, c.live, c.title); err == nil {
			t.Errorf("title %.10q live=%v path=%q: want an error", c.title, c.live != nil, c.s.Path)
		}
	}
	if s.Title != "" {
		t.Fatal("title changed despite the errors")
	}
}
