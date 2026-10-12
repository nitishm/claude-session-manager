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
