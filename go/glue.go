package main

import (
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"ccsgo/engine"
)

// The TUI is a client of the engine: everything that is not drawing or key handling lives there.
// This file holds the aliases and the thin tea.Cmd wrappers around the engine's blocking calls.

type (
	session  = engine.Session
	liveInfo = engine.LiveInfo
)

var (
	home = os.Getenv("HOME")
	eng  = engine.New(home)
)

const (
	forkMark   = engine.ForkMark
	legacyMark = engine.LegacyMark
)

type (
	briefMsg struct {
		id, key, text string
		at            time.Time // the session's mtime when the brief was made
		err           error
	}
	briefWantMsg struct{ id string }
	repoWantMsg  struct{ id string }
	focusMsg     struct {
		id  string
		res engine.FocusResult
		err error
	}
	repoMsg struct {
		id string
		st engine.RepoState
	}
	askMsg struct {
		kind  string // "" = ask claude, "deep" = transcript scan
		query string
		hits  []engine.Hit
		err   error
	}
	tagMsg struct {
		res map[string][]string // session ID -> new tags
		err error
	}
	reloadMsg struct {
		all     []*session
		changed bool
	}
)

func briefSession(s *session) tea.Cmd {
	return func() tea.Msg {
		txt, err := eng.Brief(s)
		return briefMsg{id: s.ID, key: engine.BriefKey(s), at: s.MTime, text: txt, err: err}
	}
}

func askSessions(query string, all []*session) tea.Cmd {
	return func() tea.Msg {
		hits, err := eng.Ask(query, all)
		return askMsg{query: query, hits: hits, err: err}
	}
}

func deepSearch(query string, all []*session) tea.Cmd {
	return func() tea.Msg {
		return askMsg{kind: "deep", query: query, hits: eng.DeepSearch(query, all)}
	}
}

func autoTag(ss []*session) tea.Cmd {
	return func() tea.Msg {
		res, err := eng.AutoTag(ss)
		return tagMsg{res: res, err: err}
	}
}

// reloadCmd rescans only transcripts that changed. It runs off the UI goroutine and touches none
// of the sessions it is given.
func reloadCmd(prev []*session) tea.Cmd {
	return func() tea.Msg {
		all, changed := eng.Reload(prev)
		return reloadMsg{all, changed}
	}
}

// syncStubs keeps a placeholder row for each running session that has no transcript yet.
func (m *model) syncStubs() bool {
	all, changed := eng.SyncStubs(m.all, m.live)
	m.all = all
	return changed
}

func (m model) isLive(s *session) bool { _, ok := m.live[s.ID]; return ok }

func (m model) isStale(s *session) bool { return eng.IsStale(s, m.live, m.pre) }

// opts is the current view as an engine query.
func (m model) opts() engine.Opts {
	o := engine.Opts{
		Text: m.in.Value(), Dir: m.dirf, Tag: m.tagf, Stale: m.staleOnly,
		PreOnly: m.preOnly, Pre: m.pre, LiveOnly: m.liveOnly, Live: m.live,
		ForkOnly: m.forkOnly, Sort: engine.Sorts[m.sortIdx], Reverse: m.rev,
	}
	if m.askOnly {
		o.AskRank = m.askRank
	}
	return o
}

func (m *model) refilter() {
	m.rows = eng.Query(m.all, m.opts())
	if m.cur >= len(m.rows) {
		m.cur = len(m.rows) - 1
	}
	if m.cur < 0 {
		m.cur = 0
	}
}

func (m *model) saveState() { eng.SetSort(engine.Sorts[m.sortIdx]) }

var sorts = engine.Sorts

// focusSession switches to the Ghostty window that holds a running session.
func focusSession(s *session, li engine.LiveInfo) tea.Cmd {
	cp := *s
	return func() tea.Msg {
		res, err := eng.FocusSession(&cp, li)
		return focusMsg{id: cp.ID, res: res, err: err}
	}
}
