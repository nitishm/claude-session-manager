package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/lipgloss"

	"ccsgo/engine"
)

var sample = &engine.BriefData{
	Goal: "Fix failing kaas-ort suites in the dev environment", Status: "blocked", StatusNote: "waiting on the dev cluster",
	Next: "re-run make suites ENV=dev once the cluster is back",
	Steps: []engine.BriefStep{{Text: "Reproduce failure", State: "done"}, {Text: "Find bad fixture", State: "done"},
		{Text: "Patch client", State: "done"}, {Text: "Re-run dev suite", State: "doing"}, {Text: "Update dashboards", State: "todo"},
		{Text: "Open PR", State: "todo"}, {Text: "Notify team", State: "todo"}},
	Timeline: []engine.BriefEvent{{When: "Oct 01", Text: "Suites fail in dev"}, {When: "Oct 02", Text: "Reproduced, found bad fixture in dev.yaml"},
		{When: "Oct 11", Text: "Re-running the dev suite after the client patch"}},
	Files: []string{"suites/client.go", "fixtures/dev.yaml", "Makefile"},
}

func init() {
	kinds := []string{"research", "diagnose", "fix"}
	outs := []string{"login test times out on every run", "stale token in fixtures/dev.yaml", "3 of 4 suites now pass"}
	for i := range sample.Timeline {
		sample.Timeline[i].Kind, sample.Timeline[i].Outcome = kinds[i], outs[i]
	}
}

func TestBriefVisualWidths(t *testing.T) {
	for _, w := range []int{40, 60, 80, 90} {
		lines := briefVisual(sample, w)
		if len(lines) < 15 {
			t.Fatalf("w=%d: only %d lines", w, len(lines))
		}
		for _, l := range lines {
			if got := lipgloss.Width(l); got > w {
				t.Errorf("w=%d: line is %d wide: %q", w, got, l)
			}
		}
		joined := strings.Join(lines, "\n")
		for _, want := range []string{"GOAL", "STATE", "NEXT", "STEPS", "TIMELINE", "3 of 7", "you are here"} {
			if !strings.Contains(joined, want) {
				t.Errorf("w=%d: missing %q", w, want)
			}
		}
	}
	if os.Getenv("SHOW_BRIEF") != "" {
		t.Log("\n" + strings.Join(briefVisual(sample, 84), "\n"))
	}
}

func TestBriefVisualMinimal(t *testing.T) {
	b := &engine.BriefData{Goal: "g", Status: "done", Timeline: []engine.BriefEvent{{When: "Oct 01", Text: "did it"}}}
	if len(briefVisual(b, 60)) == 0 {
		t.Fatal("empty")
	}
}

func TestBriefScrollStopsAtFloor(t *testing.T) {
	j, _ := json.Marshal(sample)
	m := newModel(nil)
	m.w, m.h = 100, 24
	m.briefOn, m.brief = true, string(j)
	max := m.briefMaxScroll()
	if max < 5 {
		t.Fatalf("test needs a brief taller than the window, max=%d", max)
	}
	press := func(k tea.KeyMsg) {
		nm, _ := m.Update(k)
		m = nm.(model)
	}
	for i := 0; i < 200; i++ {
		press(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.briefScroll != max {
		t.Fatalf("after 200 downs scroll=%d, want the floor %d", m.briefScroll, max)
	}
	press(tea.KeyMsg{Type: tea.KeyUp})
	if m.briefScroll != max-1 {
		t.Errorf("one up from the bottom should move one line, got %d", m.briefScroll)
	}
	press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	press(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.briefScroll != max {
		t.Errorf("pgdown past the end: %d, want %d", m.briefScroll, max)
	}
	press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if m.briefScroll != 0 {
		t.Errorf("g should go to the top, got %d", m.briefScroll)
	}
}

func TestHyperlinkKeepsWidth(t *testing.T) {
	u := "https://github.com/o/r/pull/12"
	if got, want := lipgloss.Width(hyperlink(u, u)), lipgloss.Width(u); got != want {
		t.Fatalf("OSC 8 wrapper changes the measured width: %d vs %d", got, want)
	}
	if !strings.Contains(hyperlink(u, "x"), "\x1b]8;;"+u) {
		t.Fatal("link target missing")
	}
}
