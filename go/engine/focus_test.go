package engine

import (
	"os"
	"testing"
)

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

func TestBundleOf(t *testing.T) {
	for cmd, want := range map[string][2]string{
		"/Applications/Ghostty.app/Contents/MacOS/ghostty": {"/Applications/Ghostty.app", "Ghostty"},
		"/Applications/Zed.app/Contents/MacOS/zed":         {"/Applications/Zed.app", "Zed"},
		"/Applications/Visual Studio Code.app/Contents/Frameworks/Code Helper (Plugin).app/Contents/MacOS/Code Helper (Plugin) --type=ptyHost": {"/Applications/Visual Studio Code.app", "Visual Studio Code"},
		"-zsh":                             {"", ""},
		"/usr/bin/login -flp me /bin/bash": {"", ""},
	} {
		if p, n := bundleOf(cmd); p != want[0] || n != want[1] {
			t.Errorf("%q -> %q, %q; want %q, %q", cmd, p, n, want[0], want[1])
		}
	}
}

func TestHostOfCurrentProcess(t *testing.T) {
	// Under go test there is usually no app in the chain; the walk must still terminate without error.
	hostOf(os.Getpid())
}
