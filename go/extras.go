package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"ccsgo/engine"
)

// ───────────────────────── repo info (no exec: endpoint-security agents scan every fork) ─────────────────────────

// ───────────────────────── fuzzy match + highlight ─────────────────────────

// highlight renders text truncated/padded to w, with search-word matches in hi.
func highlight(text string, words []string, base, hi lipgloss.Style, w int) string {
	text = ansi.Truncate(text, w, "…")
	rs := []rune(text)
	lt := []rune(strings.ToLower(text))
	mark := make([]bool, len(rs))
	if len(lt) == len(rs) {
		for _, wd := range words {
			for _, p := range engine.MatchPos(lt, []rune(wd), true) {
				mark[p] = true
			}
		}
	}
	var b strings.Builder
	for i := 0; i < len(rs); {
		j := i
		for j < len(rs) && mark[j] == mark[i] {
			j++
		}
		st := base
		if mark[i] {
			st = hi
		}
		b.WriteString(st.Render(string(rs[i:j])))
		i = j
	}
	if g := w - lipgloss.Width(text); g > 0 {
		b.WriteString(base.Render(strings.Repeat(" ", g)))
	}
	return b.String()
}

// ───────────────────────── undo delete ─────────────────────────

// ───────────────────────── forks ─────────────────────────

const (
	forkGlyph = "⎇" // branch glyph, shown in its own column next to the status badge
)

// forkCol is the one-cell fork column of a list row.
func forkCol(s *session) string {
	if s.Fork {
		return forkGlyph
	}
	return " "
}

// ───────────────────────── incremental reload ─────────────────────────

// truncLines cuts every line of s to w columns.
func truncLines(s string, w int) string {
	ls := strings.Split(s, "\n")
	for i, l := range ls {
		ls[i] = ansi.Truncate(l, w, "")
	}
	return strings.Join(ls, "\n")
}

// ───────────────────────── help popup ─────────────────────────

// overlay draws box over base, centered, leaving the rest of the screen visible around it.
func overlay(base, box string, w, h int) string {
	bl, pl := strings.Split(base, "\n"), strings.Split(box, "\n")
	pw := 0
	for _, l := range pl {
		pw = max(pw, lipgloss.Width(l))
	}
	x, y := max(0, (w-pw)/2), max(0, (h-len(pl))/2)
	for i, p := range pl {
		r := y + i
		if r >= len(bl) {
			break
		}
		left := ansi.Truncate(bl[r], x, "")
		if g := x - lipgloss.Width(left); g > 0 {
			left += strings.Repeat(" ", g)
		}
		bl[r] = left + "\x1b[0m" + pad(p, pw) + "\x1b[0m" + ansi.TruncateLeft(bl[r], x+pw, "")
	}
	return strings.Join(bl, "\n")
}

func helpPopup(w, h int) string {
	k := func(key, desc string) string { return sAcc.Render(pad(key, 9)) + sDim.Render(desc) }
	g := func(st lipgloss.Style, glyph, desc string) string {
		return st.Render(pad(glyph, 3)) + sDim.Render(desc)
	}
	sec := func(t string) string { return sHead.Render(t) }
	left := []string{
		sec("OPEN"),
		k("enter", "resume (asks where)"),
		k("f", "fork (asks where)"),
		k("O", "open marked as tabs/window"),
		k("R", "restore after reboot"),
		k("y", "copy resume command"),
		"",
		sec("MARK · DELETE"),
		k("space", "mark · A all shown · u clear"),
		k("X", "delete marked (or current)"),
		k("Z", "undo last delete"),
		"",
		sec("FIND"),
		k("/", "search · #tag filters"),
		k("D", "deep search: all replies"),
		k("t / T", "tag · T = auto-tag with claude"),
		k("#", "filter by tag (press again = next)"),
		k("a", "ask claude in plain English"),
		k("s", "one-line summary"),
	}
	right := []string{
		sec("FILTER · VIEW"),
		k("p", "pin · o sort · d dir"),
		k("w", "open the session's PR in the browser"),
		k("l", "running only"),
		k("b", "before reboot"),
		k("F", "forks + originals"),
		k("v", "detail pane · J/K scroll"),
		k("S", "detailed brief (popup)"),
		k("z", "stale: ≤2 prompts, >7d old"),
		k("r", "reload · q quit"),
		"",
		sec("LEGEND"),
		g(sGood, "●", "running, idle"),
		g(sWarn, "◐", "running, working"),
		g(sAcc, "?", "running, waiting for you"),
		g(sWarn, "↻", "open before reboot"),
		g(sAcc, forkGlyph, "fork (own column)"),
		g(sDim, "↗", "has a pull request"),
		g(sWarn, "★", "pinned"),
		g(sGood, "✓", "marked · "+sBad.Render("✗")+sDim.Render(" to delete")),
	}
	for len(left) < len(right) {
		left = append(left, "")
	}
	colW := 0
	for _, l := range left {
		colW = max(colW, lipgloss.Width(l))
	}
	inner := lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(colW+3).Render(strings.Join(left, "\n")), strings.Join(right, "\n"))
	body := sBold.Render("Keys & legend") + "\n\n" + inner + "\n\n" + sDim.Render("any key closes")
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).Padding(0, 2).Render(body)
}

// rowLabel is the list title: the fork column already says "fork", so the name's leading mark is dropped.
func rowLabel(s *session) string {
	l := s.Label()
	if s.Fork {
		l = strings.TrimPrefix(strings.TrimPrefix(l, forkMark), legacyMark)
	}
	return l
}

// ───────────────────────── tags ─────────────────────────

// ───────────────────────── missing directories ─────────────────────────

// askOpen asks where to open the sessions, dropping any whose directory is gone.
func (m *model) askOpen(kind string, ss []*session) tea.Cmd {
	var ok []*session
	var gone *session
	for _, s := range ss {
		if eng.DirGone(s) {
			gone = s
		} else {
			ok = append(ok, s)
		}
	}
	if len(ok) == 0 {
		return m.flash("directory is gone, cannot resume: " + eng.Short(gone.Cwd))
	}
	m.pend, m.pendSS = kind, ok
	if gone != nil {
		return m.flash(fmt.Sprintf("skipped %d session(s) whose directory is gone", len(ss)-len(ok)))
	}
	return nil
}

// ───────────────────────── list header ─────────────────────────

// colSort maps a click on the list header to a sort name; columns follow the row layout.
func colSort(x, titleW, tagW int, cur string) string {
	switch {
	case x >= 4 && x < 8:
		if cur == "recent" {
			return "oldest"
		}
		return "recent"
	case x >= 9 && x < 29:
		return "dir"
	case tagW > 0 && x >= 30+titleW+1 && x < 30+titleW+1+tagW:
		return "tags"
	case x >= 30+titleW+1+tagCols(tagW):
		return "prompts"
	}
	return ""
}

func listHeader(titleW, tagW int, cur string, rev bool) string {
	mk := func(label, on string, w int) string {
		arrow := ""
		switch {
		case cur == on && (on == "oldest" || rev):
			arrow = "▴"
		case cur == on:
			arrow = "▾"
		}
		st := sDim
		if arrow != "" {
			st = sAcc
		}
		return st.Render(pad(label+arrow, w))
	}
	age := mk("age", cur, 4)
	if cur == "recent" || cur == "oldest" {
		age = mk("age", cur, 4)
	} else {
		age = sDim.Render(pad("age", 4))
	}
	return " " + "   " + age + " " + mk("dir", "dir", 20) + " " + sDim.Render(pad("title", titleW)) + " " + tagHead(tagW, cur, rev) + mk("  #", "prompts", 4)
}

func tagHead(w int, cur string, rev bool) string {
	if w == 0 {
		return ""
	}
	l, st := "tags", sDim
	if cur == "tags" {
		l, st = "tags▾", sAcc
		if rev {
			l = "tags▴"
		}
	}
	return st.Render(pad(l, w)) + " "
}

func tagCols(tagW int) int {
	if tagW == 0 {
		return 0
	}
	return tagW + 1
}

const (
	staleMaxPrompts = 2
	staleDays       = 7
)
