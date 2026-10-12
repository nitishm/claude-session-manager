package main

import (
	"os"
	"strings"
	"testing"

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

func TestHyperlinkKeepsWidth(t *testing.T) {
	u := "https://github.com/o/r/pull/12"
	if got, want := lipgloss.Width(hyperlink(u, u)), lipgloss.Width(u); got != want {
		t.Fatalf("OSC 8 wrapper changes the measured width: %d vs %d", got, want)
	}
	if !strings.Contains(hyperlink(u, "x"), "\x1b]8;;"+u) {
		t.Fatal("link target missing")
	}
}

func TestSplitResizeClamps(t *testing.T) {
	m := model{w: 120, h: 40, preview: true}
	lw, dw, _ := m.layout()
	if lw != 120*58/100 || lw+dw != 120 {
		t.Fatalf("default split lw=%d dw=%d", lw, dw)
	}
	for _, pct := range []int{1, 20, 85, 99} {
		m.split = pct
		lw, dw, _ = m.layout()
		if lw < 40 || dw < 34 || lw+dw != 120 {
			t.Errorf("split %d%% gave lw=%d dw=%d", pct, lw, dw)
		}
	}
}
