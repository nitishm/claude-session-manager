package engine

import (
	"fmt"
	"os/exec"
	"strings"
	"unicode"
)

// Claude Code sets the terminal title to "<status glyph> <session title>", and Ghostty's scripting
// interface lists every terminal with its title. That is enough to find the window a live session is in.

// terminalTitle drops the leading status glyph ("✳ ", "◐ ") that Claude Code puts before the session title.
func terminalTitle(t string) string {
	t = strings.TrimSpace(t)
	if i := strings.IndexFunc(t, unicode.IsSpace); i > 0 {
		if first := []rune(t[:i]); len(first) == 1 && !unicode.IsLetter(first[0]) && !unicode.IsDigit(first[0]) {
			t = strings.TrimSpace(t[i:])
		}
	}
	return t
}

// matchTerminal returns the id of the terminal whose title is one of names. lines are "id<TAB>title".
func matchTerminal(lines []string, names ...string) string {
	want := map[string]bool{}
	for _, n := range names {
		if n = strings.ToLower(strings.TrimSpace(n)); n != "" {
			want[n] = true
		}
	}
	for _, l := range lines {
		id, title, ok := strings.Cut(l, "\t")
		if !ok {
			continue
		}
		if want[strings.ToLower(terminalTitle(title))] {
			return strings.TrimSpace(id)
		}
	}
	return ""
}

// terminalListScript prints "id<TAB>title" for every Ghostty terminal. The tab is spelled as a character code
// because inside a Ghostty tell block the word "tab" means Ghostty's tab class.
const terminalListScript = `tell application "Ghostty"
	set out to ""
	repeat with t in terminals
		set out to out & (id of t) & (ASCII character 9) & (name of t) & linefeed
	end repeat
	return out
end tell`

// findTerminal returns the id of the Ghostty terminal that holds s, or "".
func findTerminal(s *Session, li LiveInfo) (string, error) {
	out, err := exec.Command("osascript", "-e", terminalListScript).Output()
	if err != nil {
		return "", fmt.Errorf("ghostty: %v", err)
	}
	return matchTerminal(strings.Split(string(out), "\n"), s.Label(), li.Name), nil
}

// FocusSession brings the Ghostty window that holds a live session to the front. It reports false, with no
// error, when Ghostty is not the terminal or no window carries that session's title.
func (e *Engine) FocusSession(s *Session, li LiveInfo) (bool, error) {
	if terminal() != "ghostty" {
		return false, nil
	}
	id, err := findTerminal(s, li)
	if err != nil || id == "" {
		return false, err
	}
	focus := fmt.Sprintf(`tell application "Ghostty"
	focus (first terminal whose id is %q)
	activate
end tell`, id)
	if b, err := exec.Command("osascript", "-e", focus).CombinedOutput(); err != nil {
		return false, fmt.Errorf("ghostty: %s", strings.TrimSpace(string(b)))
	}
	return true, nil
}
