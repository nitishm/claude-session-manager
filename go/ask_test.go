package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestAskSection(t *testing.T) {
	if askSection(nil, false, 60) != "" {
		t.Fatal("empty until asked")
	}
	items := []qaItem{{q: "one?", a: "A1"}, {q: "two?", a: "A2"}, {q: "three?", a: "A3"}}
	out := ansi.Strip(askSection(items, true, 60))
	for _, want := range []string{"ASK", "earlier answers hidden", "? two?", "A3", "thinking"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "one?") {
		t.Error("only the latest answers are shown")
	}
}
