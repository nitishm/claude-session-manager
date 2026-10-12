package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"ccsgo/engine"
)

// The brief popup. Generation and caching live in the engine.

// briefLines wraps the brief to the popup width and styles headings.
func briefLines(text string, w int) []string {
	var out []string
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimRight(ln, " ")
		if strings.TrimSpace(ln) == "" {
			out = append(out, "")
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(ln), "-") {
			if len(out) > 0 {
				out = append(out, "")
			}
			out = append(out, sHead.Render(ln))
			continue
		}
		wrapped := lipgloss.NewStyle().Width(w - 6).Render(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ln), "-")))
		for i, wl := range strings.Split(wrapped, "\n") {
			if i == 0 {
				out = append(out, sDim.Render("• ")+strings.TrimRight(wl, " "))
			} else {
				out = append(out, "  "+strings.TrimRight(wl, " "))
			}
		}
	}
	return out
}

// quote renders a dim, indented excerpt of a real message, cut to maxLines with an ellipsis.
func quote(pad, who, text string, width, maxLines int) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	lines := strings.Split(lipgloss.NewStyle().Width(max(10, width-len(who)-3)).Render(text), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = strings.TrimRight(lines[maxLines-1], " ") + "…"
	}
	var out []string
	for i, l := range lines {
		lead := strings.Repeat(" ", len(who)+3)
		if i == 0 {
			lead = sAcc.Render(who) + sDim.Render(" ❝ ")
			if who == "claude" {
				lead = sGood.Render(who) + sDim.Render(" ❝ ")
			}
		}
		out = append(out, pad+lead+sDim.Render(strings.TrimRight(l, " ")))
	}
	return out
}

// briefVisual draws a structured brief: header card, kanban columns, timeline. w is the content width.
func briefVisual(b *engine.BriefData, w int) []string {
	wrap := func(txt string, width int) []string {
		return strings.Split(lipgloss.NewStyle().Width(max(10, width)).Render(txt), "\n")
	}
	statusStyle, statusIcon := map[string]lipgloss.Style{"active": sGood, "blocked": sWarn, "done": sGood, "idle": sDim}[b.Status], map[string]string{"active": "◐", "blocked": "⚠", "done": "✔", "idle": "○"}[b.Status]
	var out []string

	// 1. header card
	out = append(out, sHead.Render("GOAL"))
	for _, l := range wrap(b.Goal, w-2) {
		out = append(out, sBold.Render(strings.TrimRight(l, " ")))
	}
	done, total := b.Progress()
	if total > 0 {
		const cells = 14
		fill := done * cells / total
		bar := sGood.Render(strings.Repeat("█", fill)) + sDim.Render(strings.Repeat("░", cells-fill))
		out = append(out, "", bar+fmt.Sprintf("  %d of %d steps", done, total))
	}
	out = append(out, "")
	state := statusStyle.Render(statusIcon + " " + b.Status)
	if b.StatusNote != "" {
		state += sDim.Render(" · ") + b.StatusNote
	}
	for i, l := range wrap(state, w-8) {
		lead := "      "
		if i == 0 {
			lead = sHead.Render("STATE ")
		}
		out = append(out, lead+strings.TrimRight(l, " "))
	}
	if b.Next != "" {
		for i, l := range wrap(b.Next, w-8) {
			lead := "      "
			if i == 0 {
				lead = sHead.Render("NEXT  ")
			}
			out = append(out, lead+sAcc.Render(strings.TrimRight(l, " ")))
		}
	}
	if len(b.Files) > 0 {
		out = append(out, "")
		var chips []string
		for _, f := range b.Files {
			chips = append(chips, sLink.Render(f))
		}
		for i, l := range wrap(strings.Join(chips, "  "), w-8) {
			lead := "      "
			if i == 0 {
				lead = sHead.Render("FILES ")
			}
			out = append(out, lead+strings.TrimRight(l, " "))
		}
	}

	// 2. kanban
	if total > 0 {
		cols := []struct {
			name, glyph, state string
			color              lipgloss.TerminalColor
			style              lipgloss.Style
		}{
			{"DONE", "✔", "done", cGood, sGood}, {"DOING", "◐", "doing", cLink, sLink.Underline(false)}, {"NEXT", "○", "todo", cDim, sDim},
		}
		colW := (w - 2) / 3
		stacked := colW < 20
		var boxes []string
		for _, c := range cols {
			var items []string
			n := 0
			for _, st := range b.Steps {
				if st.State == c.state {
					n++
					items = append(items, c.style.Render(c.glyph)+" "+st.Text)
				}
			}
			if len(items) == 0 {
				items = []string{sDim.Render("—")}
			}
			head := c.style.Bold(true).Render(fmt.Sprintf("%s (%d)", c.name, n))
			bw := colW - 2
			if stacked {
				bw = w - 2
			}
			boxes = append(boxes, lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(c.color).Padding(0, 1).Width(bw).Render(head+"\n"+strings.Join(items, "\n")))
		}
		out = append(out, "", sHead.Render("STEPS"))
		if stacked {
			for _, bx := range boxes {
				out = append(out, strings.Split(bx, "\n")...)
			}
		} else {
			out = append(out, strings.Split(lipgloss.JoinHorizontal(lipgloss.Top, boxes[0], " ", boxes[1], " ", boxes[2]), "\n")...)
		}
	}

	// 3. timeline
	if len(b.Timeline) > 0 {
		out = append(out, "", sHead.Render("TIMELINE"))
		const whenW = 8
		for i, ev := range b.Timeline {
			last := i == len(b.Timeline)-1
			dot := sGood.Render("●")
			if last && b.Status != "done" {
				dot = sWarn.Render("◐")
				if b.Status == "active" {
					dot = sLink.Render("◐")
				}
			}
			when := lipgloss.NewStyle().Width(whenW).Render(ev.When)
			txt := wrap(ev.Text, w-whenW-5)
			for j, l := range txt {
				l = strings.TrimRight(l, " ")
				if j == 0 {
					out = append(out, dot+sDim.Render("─ ")+sDim.Render(when)+" "+l)
				} else {
					out = append(out, sDim.Render("│  ")+strings.Repeat(" ", whenW+1)+l)
				}
			}
			if ev.Preview != nil {
				pad := sDim.Render("│  ") + strings.Repeat(" ", whenW+1)
				out = append(out, quote(pad, "you", ev.Preview.User, w-whenW-5, 2)...)
				out = append(out, quote(pad, "claude", ev.Preview.Reply, w-whenW-5, 2)...)
			}
			if last && b.Status != "done" {
				out = append(out, sDim.Render("│  ")+strings.Repeat(" ", whenW+1)+sLink.Underline(false).Render("← you are here"))
			}
			if !last {
				out = append(out, sDim.Render("│"))
			}
		}
		if b.Next != "" && b.Status != "done" {
			out = append(out, sDim.Render("│"))
			for j, l := range wrap(b.Next, w-whenW-5) {
				l = strings.TrimRight(l, " ")
				if j == 0 {
					out = append(out, sDim.Render("○─ ")+sDim.Render(lipgloss.NewStyle().Width(whenW).Render("next"))+" "+l)
				} else {
					out = append(out, "   "+strings.Repeat(" ", whenW+1)+l)
				}
			}
		}
	}
	return out
}

func (m model) briefWidth() int { return min(m.w-8, 92) }
func (m model) briefVis() int   { return max(3, m.h-10) }

// briefBody is the popup's scrollable content. The key handler and the renderer both use it,
// so the scroll position can be clamped to the real last line.
func (m model) briefBody() []string {
	w := m.briefWidth()
	switch {
	case m.briefBusy && m.brief == "":
		return []string{sDim.Render("asking claude… (5-15s)")}
	case m.brief == "":
		return []string{sDim.Render("no brief yet")}
	}
	if b, ok := engine.ParseBrief(m.brief); ok {
		return briefVisual(b, w-4)
	}
	return briefLines(m.brief, w) // a brief cached before the structured format; r regenerates
}

// briefMaxScroll is the largest scroll offset that still fills the window.
func (m model) briefMaxScroll() int { return max(0, len(m.briefBody())-m.briefVis()) }

func (m model) briefPopup() string {
	w := m.briefWidth()
	title := "Brief"
	if s := m.selected(); s != nil {
		title += " · " + engine.Clean(s.Label(), w-10)
	}
	body := m.briefBody()
	vis := m.briefVis()
	sc := max(0, min(m.briefScroll, m.briefMaxScroll()))
	end := min(len(body), sc+vis)
	foot := "j/k scroll · g/G top/bottom · r regenerate · Esc closes"
	if len(body) > vis {
		foot = fmt.Sprintf("%d-%d of %d · ", sc+1, end, len(body)) + foot
	}
	txt := sBold.Render(title) + "\n\n" + strings.Join(body[sc:end], "\n") + "\n\n" + sDim.Render(foot)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).Padding(0, 2).Width(w).Render(txt)
}
