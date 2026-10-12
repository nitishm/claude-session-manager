package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"ccsgo/engine"
)

func TestEventLineAndBox(t *testing.T) {
	e := engine.Event{At: time.Date(2026, 10, 1, 10, 0, 0, 0, time.Local), Kind: engine.EvPrompt, Text: "fix the bug", N: 1, Tools: 3, Secs: 7}
	l := ansi.Strip(eventLine(e, 80))
	if !strings.Contains(l, "10:00") || !strings.Contains(l, "fix the bug") || !strings.Contains(l, "3 tools") || !strings.Contains(l, "7s") {
		t.Fatalf("event line %q", l)
	}
	top, bottom := histBox([]string{"x", "╭─ timeline · 3 events ─╮", "│ a │", "╰─ 1-3 of 3 ─╯", "y"})
	if top != 1 || bottom != 3 {
		t.Fatalf("box %d %d", top, bottom)
	}
	if top, _ := histBox([]string{"nothing"}); top != -1 {
		t.Fatal("no box expected")
	}
}
