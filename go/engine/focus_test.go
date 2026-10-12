package engine

import "testing"

func TestTerminalTitle(t *testing.T) {
	for in, want := range map[string]string{
		"✳ Kaas-ops promote command audit": "Kaas-ops promote command audit",
		"◐ claude-session-manager":         "claude-session-manager",
		"ccs":                              "ccs",
		"2 failing tests":                  "2 failing tests", // a digit is not a status glyph
	} {
		if got := terminalTitle(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestMatchTerminal(t *testing.T) {
	lines := []string{"A\t✳ Agent files setup", "B\t◐ claude-session-manager", "C\tccs", "bad line"}
	if got := matchTerminal(lines, "Claude-Session-Manager", "n-malhotra-b0"); got != "B" {
		t.Errorf("got %q", got)
	}
	if got := matchTerminal(lines, "something else"); got != "" {
		t.Errorf("no match expected, got %q", got)
	}
	if got := matchTerminal(lines, "", " "); got != "" {
		t.Errorf("blank names must not match, got %q", got)
	}
}
