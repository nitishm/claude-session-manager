package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"ccsgo/engine"
)

const histRows = 10 // content rows in the timeline window

var histGlyph = map[string]string{
	engine.EvPrompt: "›", engine.EvCommit: "✓", engine.EvPush: "↑", engine.EvPR: "⇄", engine.EvCompact: "≈",
	engine.EvAway: "☾", engine.EvTitle: "✎", engine.EvAgent: "◇", engine.EvAsk: "?", engine.EvResume: "↻",
	engine.EvGap: "…", engine.EvSchedule: "⏲", engine.EvArtifact: "▣", engine.EvAPIError: "!", engine.EvInterrupt: "✗",
}

// historySection is the detail pane's TIMELINE window: a fixed-height, terminal-style box holding every prompt, commit,
// push, PR, compaction and pause, newest at the bottom. The mouse wheel over the box scrolls it.
func (m model) historySection(s *session, w int) string {
	if s.Path == "" || s.Prompts == 0 {
		return ""
	}
	ev := eng.Timeline(s).Events
	if len(ev) == 0 {
		return ""
	}
	inner := max(w-4, 10)
	var rows []string
	prevDay := ""
	for _, e := range ev {
		if day := e.At.Format("Mon 02 Jan"); day != prevDay {
			rows = append(rows, sDim.Render("── "+day))
			prevDay = day
		}
		rows = append(rows, eventLine(e, inner))
	}
	n := len(rows)
	off := min(m.histOff, max(0, n-histRows))
	end := n - off
	start := max(0, end-histRows)
	title := fmt.Sprintf(" timeline · %d events ", len(ev))
	foot := fmt.Sprintf(" %d-%d of %d ", start+1, end, n)
	if n > histRows {
		foot += "· wheel scrolls "
	}
	edge := func(l, r, label string) string {
		fill := max(0, w-2-lipgloss.Width(label)-1)
		return sDim.Render(l+"─") + sAcc.Render(label) + sDim.Render(strings.Repeat("─", fill)+r)
	}
	var b strings.Builder
	b.WriteString("\n" + edge("╭", "╮", title) + "\n")
	for i := 0; i < histRows; i++ {
		row := ""
		if start+i < end {
			row = ansi.Truncate(rows[start+i], inner, "…")
		}
		b.WriteString(sDim.Render("│ ") + row + strings.Repeat(" ", max(0, inner-lipgloss.Width(row))) + sDim.Render(" │") + "\n")
	}
	b.WriteString(edge("╰", "╯", foot))
	return b.String()
}

// histBox finds the timeline window in rendered detail lines: the index of its top and bottom border, or -1.
func histBox(lines []string) (top, bottom int) {
	top, bottom = -1, -1
	for i, l := range lines {
		l = ansi.Strip(l)
		if top < 0 && strings.HasPrefix(l, "╭─ timeline") {
			top = i
		} else if top >= 0 && strings.HasPrefix(l, "╰─") {
			return top, i
		}
	}
	return -1, -1
}

func eventLine(e engine.Event, w int) string {
	g := histGlyph[e.Kind]
	if g == "" {
		g = "·"
	}
	head := sDim.Render(e.At.Format("15:04") + " ")
	text := e.Text
	switch e.Kind {
	case engine.EvPrompt:
		var st []string
		if e.Tools > 0 {
			st = append(st, fmt.Sprintf("%d tools", e.Tools))
		}
		if e.Edits > 0 {
			st = append(st, fmt.Sprintf("%d edits", e.Edits))
		}
		if e.Secs > 0 {
			st = append(st, secsText(e.Secs))
		}
		line := sAcc.Render(g) + " " + text
		out := head + line
		if len(st) > 0 {
			out += sDim.Render("  " + strings.Join(st, " · "))
		}
		if e.Errors > 0 {
			out += sWarn.Render(fmt.Sprintf("  %d failed", e.Errors))
		}
		return out
	case engine.EvCommit, engine.EvPush, engine.EvPR:
		if e.Detail != "" {
			text += " " + e.Detail
		}
		return head + sGood.Render(g) + " " + text
	case engine.EvInterrupt, engine.EvAPIError:
		if e.Detail != "" {
			text += " · " + e.Detail
		}
		return head + sWarn.Render(g+" "+text)
	}
	if e.Detail != "" {
		text += " · " + e.Detail
	}
	return head + sDim.Render(g+" "+text)
}

func secsText(n int) string {
	if n < 60 {
		return fmt.Sprintf("%ds", n)
	}
	return engine.Duration(n / 60)
}

// wheelHistory scrolls the timeline window when the pointer is over it. up is +1 for older events.
func (m *model) wheelHistory(x, y, w, up int) bool {
	lines := m.detailLines(w)
	top, bottom := histBox(lines)
	if top < 0 {
		return false
	}
	_, _, h := m.layout()
	row := y - 2 + min(m.dscroll, max(0, len(lines)-h))
	if row < top || row > bottom {
		return false
	}
	m.histOff = max(0, m.histOff+up*3)
	return true
}
