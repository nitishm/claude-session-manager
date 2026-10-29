package main

import (
	"testing"

	"ccsgo/engine"

	tea "github.com/charmbracelet/bubbletea"
)

func key(m model, s string) model {
	k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	switch s {
	case "esc":
		k = tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		k = tea.KeyMsg{Type: tea.KeyDown}
	}
	nm, _ := m.artKey(k)
	return nm.(model)
}

func TestArtifactsTabNavigation(t *testing.T) {
	m := model{h: 24, w: 80, msg: ""}
	rows := []engine.ArtifactRow{{Artifact: engine.Artifact{Title: "a"}}, {Artifact: engine.Artifact{Title: "b"}}, {Artifact: engine.Artifact{Title: "c"}}}
	m.art = artTab{on: true, rows: rows, loaded: true, in: newArtInput()}
	m = key(m, "j")
	m = key(m, "down")
	m = key(m, "j") // clamps at the last row
	if m.art.cur != 2 {
		t.Fatalf("cur = %d, want 2", m.art.cur)
	}
	m = key(m, "k")
	m = key(m, "g")
	if m.art.cur != 0 {
		t.Fatalf("home: cur = %d", m.art.cur)
	}
	if m = key(m, "f"); m.msg == "" {
		t.Fatal("f on a row with no source file should say so")
	}
	if m = key(m, "esc"); m.art.on {
		t.Fatal("esc should leave the Artifacts mode")
	}
}
