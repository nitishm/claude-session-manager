package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// qaItem is one question asked about a session this run. Kept in memory only.
type qaItem struct {
	q, a   string
	failed bool
}

type qaMsg struct {
	id, q, a string
	err      error
}

const askKept = 2 // answers shown in the detail pane; older ones stay in memory

func askThisSession(s *session, q string) tea.Cmd {
	cp := *s
	return func() tea.Msg {
		a, err := eng.AskSession(&cp, q)
		return qaMsg{id: cp.ID, q: q, a: a, err: err}
	}
}

// askSection is the detail pane's ASK block. It is empty until a question has been asked.
func askSection(items []qaItem, busy bool, w int) string {
	if len(items) == 0 && !busy {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n" + sHead.Render("ASK") + sDim.Render("  · Q asks another") + "\n")
	shown := items
	if len(shown) > askKept {
		b.WriteString(sDim.Render("… earlier answers hidden") + "\n")
		shown = shown[len(shown)-askKept:]
	}
	for _, it := range shown {
		b.WriteString(sAcc.Render("? "+it.q) + "\n")
		if it.failed {
			b.WriteString(sBad.Render(it.a) + "\n")
		} else {
			b.WriteString(it.a + "\n")
		}
	}
	if busy {
		b.WriteString(sDim.Render("thinking… (5-15s)") + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
