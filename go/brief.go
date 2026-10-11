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

func (m model) briefPopup() string {
	w := min(m.w-8, 92)
	title := "Brief"
	if s := m.selected(); s != nil {
		title += " · " + engine.Clean(s.Label(), w-10)
	}
	var body []string
	switch {
	case m.briefBusy && m.brief == "":
		body = []string{sDim.Render("asking claude… (5-15s)")}
	case m.brief == "":
		body = []string{sDim.Render("no brief yet")}
	default:
		body = briefLines(m.brief, w)
	}
	vis := max(3, m.h-10)
	sc := max(0, min(m.briefScroll, max(0, len(body)-vis)))
	end := min(len(body), sc+vis)
	foot := "j/k scroll · r regenerate · Esc closes"
	if len(body) > vis {
		foot = fmt.Sprintf("%d-%d of %d · ", sc+1, end, len(body)) + foot
	}
	txt := sBold.Render(title) + "\n\n" + strings.Join(body[sc:end], "\n") + "\n\n" + sDim.Render(foot)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).Padding(0, 2).Width(w).Render(txt)
}
