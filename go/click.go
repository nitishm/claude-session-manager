package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Mouse reporting makes most terminals swallow link clicks, so the TUI opens URLs and files itself.

// tokenAt returns the whitespace-delimited word under display column col, minus wrapping punctuation.
func tokenAt(line string, col int) string {
	line = ansi.Strip(line)
	rs := []rune(line)
	at, x := -1, 0
	for i, r := range rs {
		w := max(lipgloss.Width(string(r)), 1)
		if col >= x && col < x+w {
			at = i
			break
		}
		x += w
	}
	if at < 0 || unicode.IsSpace(rs[at]) {
		return ""
	}
	lo, hi := at, at
	for lo > 0 && !unicode.IsSpace(rs[lo-1]) {
		lo--
	}
	for hi < len(rs)-1 && !unicode.IsSpace(rs[hi+1]) {
		hi++
	}
	return strings.Trim(string(rs[lo:hi+1]), "()[]<>,;:'\"`")
}

// clickDetail opens the URL or file under a click in the detail pane. col and row are relative to the pane.
func (m model) clickDetail(col, row, w int) tea.Cmd {
	s := m.selected()
	lines := m.detailLines(w)
	if s == nil || lines == nil {
		return nil
	}
	_, _, h := m.layout()
	scroll := min(m.dscroll, max(0, len(lines)-h))
	li := scroll + row
	if li < 0 || li >= len(lines) || row >= h {
		return nil
	}
	if (scroll > 0 && row == 0) || (scroll+h < len(lines) && row == h-1) {
		return nil // the "more above/below" markers cover those lines
	}
	tok := tokenAt(lines[li], col)
	if len(tok) < 2 {
		return nil
	}
	if strings.HasPrefix(tok, "http://") || strings.HasPrefix(tok, "https://") {
		if s.PR != "" && strings.Contains(s.PR, tok) {
			tok = s.PR // a long URL may be cut by wrapping; the PR link is the one we know in full
		}
		if err := exec.Command("open", tok).Start(); err != nil {
			return m.flash("could not open: " + err.Error())
		}
		return m.flash("opened " + tok)
	}
	path := resolvePath(tok, s.Cwd)
	if path == "" {
		return nil
	}
	if err := openInEditor(path); err != nil {
		return m.flash("could not open: " + err.Error())
	}
	return m.flash("opened " + tok)
}

// resolvePath returns the absolute path tok names, or "" when nothing exists there.
func resolvePath(tok, cwd string) string {
	p := tok
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	} else if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// openInEditor opens files in Zed when it is installed and folders in the default app.
func openInEditor(path string) error {
	const zed = "/Applications/Zed.app"
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		if _, err := os.Stat(zed); err == nil {
			return exec.Command("open", "-a", zed, path).Start()
		}
	}
	return exec.Command("open", path).Start()
}
