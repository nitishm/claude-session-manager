package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"ccsgo/engine"
)

func TestTokenAt(t *testing.T) {
	line := "\x1b[34mFILES\x1b[0m  go/engine/brief.go, https://github.com/x/y/pull/9)  ~/a.txt"
	cases := map[int]string{
		0:  "FILES",
		8:  "go/engine/brief.go",
		30: "https://github.com/x/y/pull/9",
		64: "~/a.txt",
		6:  "", // the gap between words
	}
	for col, want := range cases {
		if got := tokenAt(line, col); got != want {
			t.Errorf("col %d = %q, want %q", col, got, want)
		}
	}
}

func TestResolvePath(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), nil, 0o644)
	if got := resolvePath("a.go", dir); got != filepath.Join(dir, "a.go") {
		t.Errorf("relative = %q", got)
	}
	if got := resolvePath("missing.go", dir); got != "" {
		t.Errorf("missing = %q", got)
	}
	if got := resolvePath(filepath.Join(dir, "a.go"), "/nowhere"); got == "" {
		t.Error("absolute path should resolve")
	}
}

// A click on a URL or file in the rendered detail pane must hit the line it appears on.
func TestClickDetailOpensWhatIsUnderTheCursor(t *testing.T) {
	bin, cwd := t.TempDir(), t.TempDir()
	log := filepath.Join(bin, "log")
	os.WriteFile(filepath.Join(bin, "open"), []byte("#!/bin/sh\necho \"$@\" >> "+log+"\n"), 0o755)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	os.WriteFile(filepath.Join(cwd, "widget.go"), nil, 0o644)

	b := &engine.BriefData{Goal: "g", Status: "active", Steps: []engine.BriefStep{{Text: "s", State: "done"}}, Files: []string{"widget.go"}}
	raw, _ := json.Marshal(b)
	s := &session{ID: "id1", Path: "/x", Cwd: cwd, Prompts: 3, MTime: time.Now(), PR: "https://example.com/pull/7"}
	m := model{w: 140, h: 40, preview: true, all: []*session{s}, rows: []*session{s}, top: new(int),
		briefs: map[string]briefEntry{"id1": {text: string(raw), at: s.MTime}}, briefGen: map[string]bool{}}
	lw, dw, _ := m.layout()
	lines := m.detailLines(dw - 4)
	click := func(needle string) string {
		os.Remove(log)
		for i, l := range lines {
			if k := strings.Index(ansi.Strip(l), needle); k >= 0 {
				col := lipgloss.Width(ansi.Strip(l)[:k]) + 1
				m.update(tea.MouseMsg{X: lw + 2 + col, Y: 2 + i, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
				time.Sleep(300 * time.Millisecond)
				got, _ := os.ReadFile(log)
				return strings.TrimSpace(string(got))
			}
		}
		t.Fatalf("%q not in the detail pane", needle)
		return ""
	}
	if got := click("example.com"); got != "https://example.com/pull/7" {
		t.Errorf("URL click opened %q", got)
	}
	if got := click("widget.go"); !strings.HasSuffix(got, "widget.go") {
		t.Errorf("file click opened %q", got)
	}
}
