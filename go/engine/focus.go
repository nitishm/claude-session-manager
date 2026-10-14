package engine

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
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

// FocusResult says what FocusSession did.
type FocusResult struct {
	Focused bool   `json:"focused"`
	Host    string `json:"host,omitempty"`  // the app the session runs in, such as Ghostty or Zed
	Exact   bool   `json:"exact,omitempty"` // true when the session's own terminal was selected, false when only its app or window was raised
}

var appRe = regexp.MustCompile(`^(.*?/([^/]+)\.app)(/|$)`)

// bundleOf returns the outermost app bundle in a process command line: "/Applications/Zed.app" and "Zed".
func bundleOf(cmd string) (path, name string) {
	cmd = strings.TrimLeft(strings.TrimSpace(cmd), "-")
	// a bundle path is one path: when " /" appears inside the match, the app is only an argument
	// of another program (login -flp user /bin/bash -c '/x/CCS.app/...')
	if m := appRe.FindStringSubmatch(cmd); m != nil && !strings.Contains(m[1], " /") {
		return m[1], m[2]
	}
	return "", ""
}

// editorApps are hosts that can open a folder, which raises the window that already has it open.
var editorApps = map[string]bool{"Zed": true, "Zed Preview": true, "Visual Studio Code": true, "Visual Studio Code - Insiders": true, "Cursor": true, "Windsurf": true, "VSCodium": true}

// hostOf walks up from pid and returns the first app bundle that holds the process, for example the
// Zed or Ghostty that owns the terminal a session runs in. It is empty under tmux, ssh and the like.
func hostOf(pid int) (path, name string) {
	for i := 0; i < 16 && pid > 1; i++ {
		out, err := exec.Command("ps", "-o", "ppid=,command=", "-p", strconv.Itoa(pid)).Output()
		line := strings.TrimSpace(string(out))
		if err != nil || line == "" {
			return "", ""
		}
		ppid, cmd, _ := strings.Cut(line, " ")
		if p, n := bundleOf(cmd); p != "" {
			return p, n
		}
		next, err := strconv.Atoi(strings.TrimSpace(ppid))
		if err != nil {
			return "", ""
		}
		pid = next
	}
	return "", ""
}

// FocusSession switches to where a running session lives. Ghostty: its own terminal, found by title.
// Zed, VS Code and the like: the window that has the session's folder open. Any other terminal app is raised.
// It reports Focused false, with no error, when the host cannot be found or is not an app.
func (e *Engine) FocusSession(s *Session, li LiveInfo) (FocusResult, error) {
	path, name := hostOf(li.PID)
	switch {
	case name == "Ghostty" || (name == "" && terminal() == "ghostty"):
		id, err := findTerminal(s, li)
		if err != nil || id == "" {
			return FocusResult{Host: "Ghostty"}, err
		}
		focus := fmt.Sprintf(`tell application "Ghostty"
	focus (first terminal whose id is %q)
	activate
end tell`, id)
		if b, err := exec.Command("osascript", "-e", focus).CombinedOutput(); err != nil {
			return FocusResult{Host: "Ghostty"}, fmt.Errorf("ghostty: %s", strings.TrimSpace(string(b)))
		}
		return FocusResult{Focused: true, Host: "Ghostty", Exact: true}, nil
	case editorApps[name]:
		dir := li.Cwd
		if dir == "" {
			dir = s.Cwd
		}
		if err := exec.Command("open", "-a", path, dir).Run(); err != nil {
			return FocusResult{Host: name}, fmt.Errorf("%s: %v", name, err)
		}
		return FocusResult{Focused: true, Host: name}, nil
	case name != "":
		if err := exec.Command("open", "-a", path).Run(); err != nil {
			return FocusResult{Host: name}, fmt.Errorf("%s: %v", name, err)
		}
		return FocusResult{Focused: true, Host: name}, nil
	}
	return FocusResult{}, nil
}
