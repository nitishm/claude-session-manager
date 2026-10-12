package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"ccsgo/engine"
)

// repoView is the last repo state fetched for a session. busy marks a fetch that is scheduled or running.
type repoView struct {
	st   engine.RepoState
	at   time.Time
	ok   bool
	busy bool
}

const repoFresh = 60 * time.Second

// ensureRepo fetches branch, dirty count and PR state for the selected session after a short dwell.
// The fetch shells out to jj, git and gh, so it never runs while the cursor is just passing by.
func (m model) ensureRepo() (model, tea.Cmd) {
	s := m.selected()
	if s == nil || s.Cwd == "" {
		return m, nil
	}
	if v, ok := m.repoSt[s.ID]; ok && (v.busy || time.Since(v.at) < repoFresh) {
		return m, nil
	}
	v := m.repoSt[s.ID]
	v.busy = true
	m.repoSt[s.ID] = v
	id := s.ID
	return m, tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return repoWantMsg{id: id} })
}

func fetchRepo(s *session) tea.Cmd {
	cp := *s
	return func() tea.Msg { return repoMsg{id: cp.ID, st: eng.RepoState(&cp)} }
}

// repoLine renders the branch, change count and PR state. It is empty until the first fetch lands.
func repoLine(v repoView) string {
	if !v.ok {
		return ""
	}
	st := v.st
	var parts []string
	if st.Kind != "" {
		br := st.Branch
		if br == "" {
			br = "(no " + map[string]string{"jj": "bookmark", "git": "branch"}[st.Kind] + ")"
		}
		parts = append(parts, sAcc.Render(st.Kind+" "+br))
		if st.Dirty > 0 {
			parts = append(parts, sWarn.Render(fmt.Sprintf("%d changed", st.Dirty)))
		} else {
			parts = append(parts, sDim.Render("clean"))
		}
		if st.Ahead > 0 || st.Behind > 0 {
			parts = append(parts, sDim.Render(fmt.Sprintf("↑%d ↓%d", st.Ahead, st.Behind)))
		}
	}
	if p := st.PR; p != nil {
		parts = append(parts, prText(p))
	} else if st.Err != "" {
		parts = append(parts, sDim.Render(st.Err))
	}
	return strings.Join(parts, sDim.Render(" · "))
}

func prText(p *engine.PRState) string {
	state := strings.ToLower(p.State)
	if p.Draft {
		state = "draft"
	}
	out := []string{"PR " + state}
	st := sDim
	switch p.State {
	case "MERGED":
		st = sAcc
	case "CLOSED":
		st = sBad
	}
	switch p.Checks {
	case "failing":
		out = append(out, fmt.Sprintf("%d checks failing", p.Failed))
		st = sBad
	case "pending":
		out = append(out, fmt.Sprintf("%d checks running", p.Pending))
	case "passing":
		out = append(out, "checks pass")
	}
	switch p.Review {
	case "APPROVED":
		out = append(out, "approved")
	case "CHANGES_REQUESTED":
		out = append(out, "changes requested")
		st = sBad
	case "REVIEW_REQUIRED":
		out = append(out, "needs review")
	}
	return st.Render(strings.Join(out, ", "))
}
