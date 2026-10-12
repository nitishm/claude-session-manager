// ccs-go: browse Claude Code sessions across all dirs. Enter resumes.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"ccsgo/engine"
)

// liveGlyph: idle ● (green), busy ◐ (working), anything else ? (needs you).
func liveGlyph(li liveInfo) (string, lipgloss.Style) {
	switch li.Status {
	case "idle":
		return "●", sGood
	case "busy":
		return "◐", sWarn
	}
	return "?", sAcc
}

func rawBadge(m model, s *session) string {
	switch {
	case m.del[s.ID]:
		return "✗"
	case m.marks[s.ID]:
		return "✓"
	case eng.Pinned(s.ID):
		return "★"
	case m.briefGen[s.ID]:
		return "◌"
	case m.isLive(s):
		g, _ := liveGlyph(m.live[s.ID])
		return g
	case m.pre[s.ID]:
		return "↻"
	case s.PR != "":
		return "↗"
	}
	return " "
}

// ───────────────────────── model ─────────────────────────

type (
	clearMsg struct{ seq int }
	tickMsg  time.Time
)

type model struct {
	all        []*session
	rows       []*session
	sortIdx    int
	dirf       string
	tagf       string // only sessions with this tag
	rev        bool   // reverse the current sort (click the active header again)
	in         textinput.Model
	cur        int
	top        *int
	w, h       int
	preview    bool
	help       bool
	busy       map[string]bool
	msg        string
	msgSeq     int
	result     *session
	fork       bool
	dscroll    int
	lastSel    string
	boot       time.Time
	pre        map[string]bool
	preOnly    bool
	marks      map[string]bool
	live       map[string]liveInfo
	confirm    string
	liveOnly   bool
	del        map[string]bool
	delAsk     bool
	restoreAsk bool
	pend       string // pending open: resume | fork | open | restore; waits for t / w / h
	pendSS     []*session
	tickN      int
	forkOnly   bool
	reloading  bool
	askIn      textinput.Model
	tagIn      textinput.Model
	tagFor     string                // session ID being tagged
	tagging    bool                  // an auto-tag call is running
	split      int                   // list width as a percent of the screen; 0 means the default
	dragging   bool                  // the divider is being dragged
	briefs     map[string]briefEntry // per-session brief shown in the detail pane
	briefGen   map[string]bool       // brief generations in flight
	histAll    bool                  // HISTORY shows every event instead of the latest few
	repoSt     map[string]repoView   // branch, dirty count and PR state by session id
	asking     bool
	askKind    string // "" or "deep": what produced the current result list
	deepIn     textinput.Model
	staleOnly  bool
	askOnly    bool
	askQ       string
	askWhy     map[string]string
	askRank    map[string]int
}

// layout returns list width, detail width, body height.
func (m model) layout() (lw, dw, bodyH int) {
	bodyH = max(3, m.h-5)
	lw = m.w
	if m.preview && m.w >= 90 {
		pct := m.split
		if pct == 0 {
			pct = 58
		}
		lw = min(max(m.w*pct/100, 40), m.w-34) // keep both panes usable
		dw = m.w - lw
	}
	return
}

var (
	cAccent = lipgloss.AdaptiveColor{Light: "#5B3FD9", Dark: "#A78BFA"}
	cDim    = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#7C8190"}
	cGood   = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#4ADE80"}
	cWarn   = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FBBF24"}
	cLink   = lipgloss.AdaptiveColor{Light: "#1D4ED8", Dark: "#60A5FA"}
	cBad    = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
	cSelBg  = lipgloss.AdaptiveColor{Light: "#E9E5FF", Dark: "#2B2547"}

	sTitle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#7C3AED")).Padding(0, 1)
	sDim    = lipgloss.NewStyle().Foreground(cDim)
	sAcc    = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sGood   = lipgloss.NewStyle().Foreground(cGood)
	sWarn   = lipgloss.NewStyle().Foreground(cWarn)
	sLink   = lipgloss.NewStyle().Foreground(cLink).Underline(true)
	sBold   = lipgloss.NewStyle().Bold(true)
	sSel    = lipgloss.NewStyle().Background(cSelBg).Bold(true)
	sCursor = lipgloss.NewStyle().Foreground(cAccent).Background(cSelBg).Bold(true)
	sBad    = lipgloss.NewStyle().Foreground(cBad).Bold(true)
	sHead   = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
)

func divColor(drag bool) lipgloss.AdaptiveColor {
	if drag {
		return cAccent
	}
	return cDim
}

// hyperlink wraps text in an OSC 8 link, so terminals that support it (Ghostty, iTerm2) open it on Cmd-click.
// Others ignore the escape and show the text unchanged.
func hyperlink(url, text string) string {
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

func newModel(all []*session) model {
	ti := textinput.New()
	ti.Prompt = " ⌕ "
	ti.PromptStyle = sAcc
	ti.Placeholder = "press / to search titles, summaries, prompts, dirs"
	ti.PlaceholderStyle = sDim
	ai := textinput.New()
	ai.Prompt = " ✦ ask "
	ai.PromptStyle = sAcc
	ai.Placeholder = "describe the session you are looking for, in plain English"
	ai.PlaceholderStyle = sDim
	di := textinput.New()
	di.Prompt = " ⌕ deep "
	di.PromptStyle = sAcc
	di.Placeholder = "words that appear anywhere in your messages or claude's replies (reads every transcript)"
	di.PlaceholderStyle = sDim
	tg := textinput.New()
	tg.Prompt = " # tags "
	tg.PromptStyle = sAcc
	tg.Placeholder = "space separated, empty clears · find with /#tag"
	tg.PlaceholderStyle = sDim
	sortBy := eng.Sort()
	idx := 0
	for i, s := range sorts {
		if s == sortBy {
			idx = i
		}
	}
	if q := strings.Join(os.Args[1:], " "); q != "" {
		ti.SetValue(q)
	}
	boot := engine.BootTime()
	pre := eng.PreBoot(all, boot)
	live, _ := eng.ReadPids()
	m := model{askIn: ai, deepIn: di, tagIn: tg, askWhy: map[string]string{}, askRank: map[string]int{}, del: map[string]bool{}, live: live, boot: boot, pre: pre, marks: map[string]bool{}, preOnly: len(pre) > 0 && time.Since(boot) < 90*time.Minute, top: new(int), all: all, sortIdx: idx, in: ti, preview: true, briefs: map[string]briefEntry{}, briefGen: map[string]bool{}, repoSt: map[string]repoView{}}
	m.refilter()
	return m
}

func (m model) selected() *session {
	if len(m.rows) == 0 {
		return nil
	}
	return m.rows[m.cur]
}

func (m *model) flash(s string) tea.Cmd {
	m.msg = s
	m.msgSeq++
	seq := m.msgSeq
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return clearMsg{seq} })
}

func (m model) Init() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	nm, cmd := m.update(msg)
	mm := nm.(model)
	id := ""
	if s := mm.selected(); s != nil {
		id = s.ID
	}
	if id != mm.lastSel {
		mm.lastSel, mm.dscroll, mm.confirm = id, 0, ""
	}
	mm, bc := mm.ensureBrief()
	mm, rc := mm.ensureRepo()
	return mm, tea.Batch(cmd, bc, rc)
}

func (m model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		lw, dw, _ := m.layout()
		switch {
		case dw > 0 && msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && msg.Y >= 2 && msg.X >= lw-1 && msg.X <= lw+1:
			m.dragging = true // grab the divider
		case dw > 0 && msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && msg.X >= lw+2 && msg.Y >= 2:
			return m, m.clickDetail(msg.X-lw-2, msg.Y-2, dw-4) // the body starts at Y=2, under the title and search rows
		case m.dragging && msg.Action == tea.MouseActionMotion:
			m.split = min(max(msg.X*100/max(m.w, 1), 20), 85)
		case m.dragging && msg.Action == tea.MouseActionRelease:
			m.dragging = false
		case msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown:
			dir := 1
			if msg.Button == tea.MouseButtonWheelUp {
				dir = -1
			}
			if dw > 0 && msg.X >= lw {
				m.dscroll = max(0, m.dscroll+dir*3)
			} else {
				m.cur = max(0, min(len(m.rows)-1, m.cur+dir*3))
			}
		case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && msg.Y == 1:
			m.in.Focus()
			return m, textinput.Blink
		case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && msg.X < lw && msg.Y == 2:
			titleW, tagW := m.colWidths(lw)
			if name := colSort(msg.X, titleW, tagW, sorts[m.sortIdx]); name != "" {
				if name == sorts[m.sortIdx] {
					m.rev = !m.rev // second click on the active column flips the order
				} else {
					m.rev = false
				}
				for i, s := range sorts {
					if s == name {
						m.sortIdx = i
					}
				}
				sel := ""
				if s := m.selected(); s != nil {
					sel = s.ID
				}
				m.saveState()
				m.refilter()
				for i, r := range m.rows {
					if r.ID == sel {
						m.cur = i
					}
				}
				return m, m.flash(fmt.Sprintf("sort: %s%s", name, map[bool]string{true: " (reversed)"}[m.rev]))
			}
		case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && msg.X < lw && msg.Y >= 3:
			if i := *m.top + msg.Y - 3; i >= 0 && i < len(m.rows) {
				if i == m.cur {
					cmd := m.askOpen("resume", []*session{m.rows[i]})
					return m, cmd
				}
				m.cur = i
			}
		}
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case clearMsg:
		if msg.seq == m.msgSeq {
			m.msg = ""
		}
	case tickMsg: // refresh what is running, keep ages fresh
		m.live = eng.LivePids(m.boot)
		if m.syncStubs() || m.liveOnly {
			sel := ""
			if s := m.selected(); s != nil {
				sel = s.ID
			}
			m.refilter()
			for i, r := range m.rows {
				if r.ID == sel {
					m.cur = i
				}
			}
		}
		next := tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
		if m.tickN++; m.tickN%6 == 0 && !m.reloading { // every 30s pick up new and changed transcripts
			m.reloading = true
			return m, tea.Batch(next, reloadCmd(m.all))
		}
		return m, next
	case reloadMsg:
		m.reloading = false
		if !msg.changed {
			return m, nil
		}
		sel := ""
		if s := m.selected(); s != nil {
			sel = s.ID
		}
		m.all = msg.all
		m.syncStubs()
		eng.LinkForks(m.all)
		alive := map[string]bool{}
		for _, s := range m.all {
			alive[s.ID] = true
		}
		for id := range m.marks {
			if !alive[id] {
				delete(m.marks, id)
			}
		}
		for id := range m.del {
			if !alive[id] {
				delete(m.del, id)
			}
		}
		m.refilter()
		for i, r := range m.rows { // keep the cursor on the same session
			if r.ID == sel {
				m.cur = i
			}
		}
		return m, nil
	case repoWantMsg:
		if s := m.selected(); s != nil && s.ID == msg.id {
			return m, fetchRepo(s)
		}
		delete(m.repoSt, msg.id) // moved away before the delay ended
		return m, nil
	case repoMsg:
		m.repoSt[msg.id] = repoView{st: msg.st, at: time.Now(), ok: true}
		return m, nil
	case briefWantMsg:
		// Only generate if the cursor stayed on the session, so scrolling past sessions costs nothing.
		s := m.selected()
		if e, ok := m.briefs[msg.id]; s != nil && s.ID == msg.id && ok && e.text == "" && !e.failed && !m.briefGen[msg.id] {
			m.briefGen[msg.id] = true
			return m, briefSession(s)
		}
		if e, ok := m.briefs[msg.id]; ok && e.text == "" && !e.failed && !m.briefGen[msg.id] {
			delete(m.briefs, msg.id) // moved away before the delay ended; revisiting schedules it again
		}
		return m, nil
	case briefMsg:
		delete(m.briefGen, msg.id)
		if msg.err == nil {
			eng.StoreBrief(msg.key, msg.text)
			m.briefs[msg.id] = briefEntry{text: msg.text, at: msg.at}
		} else {
			m.briefs[msg.id] = briefEntry{failed: true}
		}
		if msg.err != nil {
			return m, m.flash("brief failed: " + msg.err.Error())
		}
		return m, nil
	case tagMsg:
		m.tagging = false
		if msg.err != nil {
			return m, m.flash("auto-tag failed: " + msg.err.Error())
		}
		sel := ""
		if s := m.selected(); s != nil {
			sel = s.ID
		}
		for id, tags := range msg.res {
			eng.AddTags(id, tags)
		}
		m.refilter()
		for i, r := range m.rows {
			if r.ID == sel {
				m.cur = i
			}
		}
		return m, m.flash(fmt.Sprintf("tagged %d session(s) · t edits", len(msg.res)))
	case askMsg:
		m.asking = false
		if msg.err != nil {
			return m, m.flash("ask failed: " + msg.err.Error())
		}
		if len(msg.hits) == 0 {
			return m, m.flash("no matching session found")
		}
		m.askQ, m.askOnly, m.askKind, m.staleOnly = msg.query, true, msg.kind, false
		m.askWhy, m.askRank = map[string]string{}, map[string]int{}
		for i, h := range msg.hits {
			m.askWhy[h.ID], m.askRank[h.ID] = h.Why, i+1
		}
		m.dirf, m.tagf, m.liveOnly, m.preOnly, m.forkOnly = "", "", false, false, false
		m.in.SetValue("")
		m.cur, *m.top = 0, 0
		m.refilter()
		return m, m.flash(fmt.Sprintf("%d matches · Esc clears", len(msg.hits)))
	case tea.KeyMsg:
		if m.deepIn.Focused() {
			switch msg.String() {
			case "enter":
				q := strings.TrimSpace(m.deepIn.Value())
				m.deepIn.Blur()
				if q == "" || m.asking {
					return m, nil
				}
				m.asking = true
				return m, tea.Batch(deepSearch(q, m.all), m.flash("deep search: reading every transcript…"))
			case "esc":
				m.deepIn.SetValue("")
				m.deepIn.Blur()
				return m, nil
			}
			var c tea.Cmd
			m.deepIn, c = m.deepIn.Update(msg)
			return m, c
		}
		if m.tagIn.Focused() {
			switch msg.String() {
			case "enter":
				id := m.tagFor
				m.tagIn.Blur()
				eng.SetTags(id, engine.ParseTags(m.tagIn.Value()))
				m.tagIn.SetValue("")
				m.refilter()
				for i, r := range m.rows {
					if r.ID == id {
						m.cur = i
					}
				}
				return m, m.flash("tags saved")
			case "esc":
				m.tagIn.SetValue("")
				m.tagIn.Blur()
				return m, nil
			}
			var c tea.Cmd
			m.tagIn, c = m.tagIn.Update(msg)
			return m, c
		}
		if m.askIn.Focused() {
			switch msg.String() {
			case "enter":
				q := strings.TrimSpace(m.askIn.Value())
				m.askIn.Blur()
				if q == "" || m.asking {
					return m, nil
				}
				m.asking = true
				return m, tea.Batch(askSessions(q, m.all), m.flash("asking claude… (5-10s)"))
			case "esc":
				m.askIn.SetValue("")
				m.askIn.Blur()
				return m, nil
			}
			var c tea.Cmd
			m.askIn, c = m.askIn.Update(msg)
			return m, c
		}
		if m.help { // the popup swallows the key that closes it
			m.help = false
			return m, nil
		}
		if m.pend != "" {
			p, ss := m.pend, m.pendSS
			m.pend, m.pendSS = "", nil
			switch k := msg.String(); {
			case k == "t" || k == "w":
				tab := k == "t"
				n, err := eng.Open(ss, tab, p == "fork")
				if err != nil {
					return m, m.flash(err.Error())
				}
				if p == "open" {
					m.marks = map[string]bool{}
				}
				where := "a new window"
				if tab {
					where = "new tabs in this window"
				}
				return m, m.flash(fmt.Sprintf("opened %d sessions in %s", n, where))
			case (k == "h" || k == "enter") && (p == "resume" || p == "fork"):
				m.result, m.fork = ss[0], p == "fork"
				return m, tea.Quit
			}
			return m, m.flash("cancelled")
		}
		if msg.String() != "R" {
			m.restoreAsk = false
		}
		if m.delAsk {
			m.delAsk = false
			if msg.String() != "y" {
				m.del = map[string]bool{}
				return m, m.flash("cancelled")
			}
			if msg.String() == "y" {
				var ss []*session
				for _, s := range m.all {
					if m.del[s.ID] {
						ss = append(ss, s)
					}
				}
				res, err := eng.Delete(ss, m.live)
				n, skipped, gone := res.Moved, res.Skipped, res.Gone
				var keep []*session
				for _, s := range m.all {
					if !gone[s.ID] {
						keep = append(keep, s)
					}
				}
				for id := range gone {
					delete(m.marks, id)
				}
				m.all, m.del = keep, map[string]bool{}
				m.refilter()
				if len(m.rows) == 0 && len(m.all) > 0 { // emptied the filtered view: show everything
					m.dirf, m.tagf, m.liveOnly, m.preOnly, m.forkOnly, m.staleOnly = "", "", false, false, false, false
					m.in.SetValue("")
					m.cur, *m.top = 0, 0
					m.refilter()
				}
				text := fmt.Sprintf("moved %d sessions to ~/.Trash/claude-sessions · Z undoes", n)
				if skipped > 0 {
					text += fmt.Sprintf(" (skipped %d running)", skipped)
				}
				if err != nil {
					text = err.Error()
				}
				return m, m.flash(text)
			}
			return m, m.flash("delete cancelled")
		}
		if m.in.Focused() {
			switch msg.String() {
			case "enter":
				m.in.Blur()
				return m, nil
			case "esc":
				m.in.SetValue("")
				m.in.Blur()
				m.refilter()
				return m, nil
			case "up", "down", "ctrl+n", "ctrl+p":
			default:
				var c tea.Cmd
				m.in, c = m.in.Update(msg)
				m.cur, *m.top = 0, 0
				m.refilter()
				return m, c
			}
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "esc":
			switch {
			case m.askOnly:
				m.askOnly = false
			case m.forkOnly:
				m.forkOnly = false
			case m.in.Value() != "":
				m.in.SetValue("")
			case m.staleOnly:
				m.staleOnly = false
			case m.tagf != "":
				m.tagf = ""
			case m.dirf != "":
				m.dirf = ""
			case m.liveOnly:
				m.liveOnly = false
			case m.preOnly:
				m.preOnly = false
			default:
				return m, nil
			}
			m.cur, *m.top = 0, 0
			m.refilter()
		case "F":
			if !m.forkOnly {
				n := 0
				for _, s := range m.all {
					if s.Fork {
						n++
					}
				}
				if n == 0 {
					return m, m.flash("no forks yet: press f on a session to make one")
				}
			}
			m.forkOnly = !m.forkOnly
			m.cur, *m.top = 0, 0
			m.refilter()
		case "<", ">":
			cur := m.split
			if cur == 0 {
				cur = 58
			}
			if msg.String() == "<" {
				cur -= 5
			} else {
				cur += 5
			}
			m.split = min(max(cur, 20), 85)
		case "a":
			m.askIn.Focus()
			return m, textinput.Blink
		case "S":
			s := m.selected()
			if s == nil || s.Path == "" || s.Prompts == 0 {
				return m, m.flash("no transcript yet: ask something in that session first")
			}
			if m.briefGen[s.ID] {
				return m, m.flash("already generating this brief")
			}
			m.briefGen[s.ID] = true
			return m, tea.Batch(briefSession(s), m.flash("regenerating the brief…"))
		case "D":
			m.deepIn.Focus()
			return m, textinput.Blink
		case "z":
			m.staleOnly = !m.staleOnly
			m.cur, *m.top = 0, 0
			m.refilter()
			if m.staleOnly {
				return m, m.flash(fmt.Sprintf("%d stale sessions (≤%d prompts, older than %dd) · A marks all · X deletes · Esc clears", len(m.rows), staleMaxPrompts, staleDays))
			}
		case "T":
			if m.tagging {
				return m, m.flash("already tagging…")
			}
			var ss []*session
			for _, s := range m.all {
				if m.marks[s.ID] && s.Prompts > 0 && len(ss) < engine.AutoTagMax {
					ss = append(ss, s)
				}
			}
			if len(ss) == 0 {
				if s := m.selected(); s != nil && s.Prompts > 0 {
					ss = []*session{s}
				}
			}
			if len(ss) == 0 {
				return m, m.flash("nothing to tag yet (no prompts)")
			}
			m.tagging = true
			return m, tea.Batch(autoTag(ss), m.flash(fmt.Sprintf("asking claude to tag %d session(s)… (5-10s)", len(ss))))
		case "t":
			if s := m.selected(); s != nil {
				m.tagFor = s.ID
				m.tagIn.SetValue(eng.TagText(s.ID, " "))
				m.tagIn.CursorEnd()
				m.tagIn.Focus()
				return m, textinput.Blink
			}
		case "/":
			m.in.Focus()
			return m, textinput.Blink
		case "up", "k", "ctrl+p":
			if m.cur > 0 {
				m.cur--
			}
		case "down", "j", "ctrl+n":
			if m.cur < len(m.rows)-1 {
				m.cur++
			}
		case "pgup", "ctrl+u":
			m.cur = max(0, m.cur-10)
		case "pgdown", "ctrl+d":
			m.cur = min(len(m.rows)-1, m.cur+10)
		case "home", "g":
			m.cur = 0
		case "end", "G":
			m.cur = max(0, len(m.rows)-1)
		case "enter":
			if s := m.selected(); s != nil {
				if li, ok := m.live[s.ID]; ok && m.confirm != s.ID {
					m.confirm = s.ID
					return m, m.flash(fmt.Sprintf("already running (pid %d, %s). Enter again to resume anyway", li.PID, li.Status))
				}
				cmd := m.askOpen("resume", []*session{s})
				return m, cmd
			}
		case "f":
			if s := m.selected(); s != nil {
				cmd := m.askOpen("fork", []*session{s})
				return m, cmd
			}
		case "p":
			if s := m.selected(); s != nil {
				eng.TogglePin(s.ID)
				m.refilter()
				for i, r := range m.rows { // follow the row to its new position
					if r.ID == s.ID {
						m.cur = i
					}
				}
			}
		case "w":
			if s := m.selected(); s != nil {
				if s.PR == "" {
					return m, m.flash("no PR link on this session")
				}
				if !strings.HasPrefix(s.PR, "https://") && !strings.HasPrefix(s.PR, "http://") {
					return m, m.flash("not a web link: " + engine.Clean(s.PR, 40))
				}
				if err := exec.Command("open", s.PR).Start(); err != nil {
					return m, m.flash("open failed: " + err.Error())
				}
				return m, m.flash("opened PR in your browser")
			}
		case "h":
			m.histAll = !m.histAll
			return m, nil
		case "e":
			if s := m.selected(); s != nil {
				return m, m.runAction(s, "editor")
			}
		case "n":
			if s := m.selected(); s != nil {
				return m, m.runAction(s, "terminal")
			}
		case "o":
			m.rev = false
			m.sortIdx = (m.sortIdx + 1) % len(sorts)
			m.saveState()
			m.refilter()
			return m, m.flash("sort: " + sorts[m.sortIdx])
		case "#":
			tags := eng.TagList()
			if len(tags) == 0 {
				return m, m.flash("no tags yet: t tags a session, T auto-tags")
			}
			next := ""
			if m.tagf == "" {
				next = tags[0].Name
			} else {
				for i, tg := range tags {
					if tg.Name == m.tagf && i+1 < len(tags) {
						next = tags[i+1].Name
					}
				}
			}
			m.tagf = next
			m.cur, *m.top = 0, 0
			m.refilter()
			if next == "" {
				return m, m.flash("tag filter off")
			}
			n := 0
			for _, tg := range tags {
				if tg.Name == next {
					n = tg.N
				}
			}
			return m, m.flash(fmt.Sprintf("tag #%s · %d sessions · # next tag · Esc clears", next, n))
		case "d":
			dirs := []string{""}
			seen := map[string]bool{}
			var ds []string
			for _, s := range m.all {
				if s.Cwd != "" && !seen[s.Cwd] {
					seen[s.Cwd] = true
					ds = append(ds, s.Cwd)
				}
			}
			sort.Strings(ds)
			dirs = append(dirs, ds...)
			next := dirs[0] // current dir vanished (all its sessions deleted): fall back to "all"
			for i, d := range dirs {
				if d == m.dirf {
					next = dirs[(i+1)%len(dirs)]
					break
				}
			}
			m.dirf = next
			m.cur, *m.top = 0, 0
			m.refilter()
		case "y":
			if s := m.selected(); s != nil {
				c := exec.Command("pbcopy")
				c.Stdin = strings.NewReader(fmt.Sprintf("cd %s && claude --resume %s", eng.Short(s.Cwd), s.ID))
				if c.Run() == nil {
					return m, m.flash("copied resume command")
				}
			}
		case "X": // deletes the ✓ marked sessions, or the current one when nothing is marked
			m.del = map[string]bool{}
			skipped := 0
			add := func(s *session) {
				if m.isLive(s) || s.Path == "" {
					skipped++
				} else {
					m.del[s.ID] = true
				}
			}
			if len(m.marks) > 0 {
				for _, s := range m.all {
					if m.marks[s.ID] {
						add(s)
					}
				}
			} else if s := m.selected(); s != nil {
				add(s)
			}
			if len(m.del) == 0 {
				if skipped > 0 {
					return m, m.flash("running sessions can't be deleted: close them first")
				}
				return m, m.flash("nothing to delete")
			}
			m.delAsk = true
		case "l":
			m.liveOnly = !m.liveOnly
			m.cur, *m.top = 0, 0
			m.refilter()
		case "b":
			if len(m.pre) == 0 {
				return m, m.flash("no sessions found from before the last reboot")
			}
			m.preOnly = !m.preOnly
			m.cur, *m.top = 0, 0
			m.refilter()
		case " ":
			if s := m.selected(); s != nil {
				if m.marks[s.ID] {
					delete(m.marks, s.ID)
				} else {
					m.marks[s.ID] = true
				}
				if m.cur < len(m.rows)-1 {
					m.cur++
				}
			}
		case "A":
			for _, s := range m.rows {
				m.marks[s.ID] = true
			}
		case "u":
			m.marks, m.del = map[string]bool{}, map[string]bool{}
		case "Z":
			n, err := eng.Undo()
			if n == 0 {
				return m, m.flash("nothing to undo")
			}
			m.all = eng.Load()
			m.syncStubs()
			m.refilter()
			text := fmt.Sprintf("restored %d sessions", n)
			if err != nil {
				text += ": " + err.Error()
			}
			return m, m.flash(text)
		case "R":
			var ss []*session
			for _, s := range m.all {
				if m.pre[s.ID] && !m.isLive(s) {
					ss = append(ss, s)
				}
			}
			if len(ss) == 0 {
				return m, m.flash("nothing to restore: no interrupted sessions")
			}
			cmd := m.askOpen("restore", ss)
			return m, cmd
		case "O":
			var ss []*session
			for _, s := range m.rows {
				if m.marks[s.ID] {
					ss = append(ss, s)
				}
			}
			if len(ss) == 0 && m.preOnly {
				ss = m.rows
			}
			if len(ss) == 0 {
				if s := m.selected(); s != nil {
					ss = []*session{s}
				}
			}
			if len(ss) == 0 {
				return m, nil
			}
			cmd := m.askOpen("open", ss)
			return m, cmd
		case "J", "shift+down":
			m.dscroll += 3
		case "K", "shift+up":
			m.dscroll = max(0, m.dscroll-3)
		case "v":
			m.preview = !m.preview
		case "?":
			m.help = !m.help
		case "r":
			m.all = eng.Load()
			m.syncStubs()
			m.refilter()
			return m, m.flash(fmt.Sprintf("reloaded %d sessions", len(m.all)))
		}
	}
	return m, nil
}

// ───────────────────────── view ─────────────────────────

func pad(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	if g := w - lipgloss.Width(s); g > 0 {
		s += strings.Repeat(" ", g)
	}
	return s
}

func (m model) View() string {
	if m.w == 0 {
		return ""
	}
	lw, dw, bodyH := m.layout()

	head := sTitle.Render("◆ Claude sessions") +
		sDim.Render(fmt.Sprintf("  %d/%d", min(m.cur+1, len(m.rows)), len(m.rows))) +
		sDim.Render(fmt.Sprintf(" of %d", len(m.all)))

	// list
	if m.cur < *m.top {
		*m.top = m.cur
	}
	listH := bodyH - 1 // one row goes to the column header
	if m.cur >= *m.top+listH {
		*m.top = m.cur - listH + 1
	}
	top := *m.top
	titleW, tagW := m.colWidths(lw)
	words := strings.Fields(strings.ToLower(m.in.Value()))
	hiN := lipgloss.NewStyle().Foreground(cWarn).Bold(true).Underline(true)
	hiS := sSel.Foreground(cWarn).Underline(true)
	lines := []string{listHeader(titleW, tagW, sorts[m.sortIdx], m.rev)}
	for i := top; i < min(len(m.rows), top+listH); i++ {
		s := m.rows[i]
		badge := " "
		switch {
		case m.del[s.ID]:
			badge = sBad.Render("✗")
		case m.marks[s.ID]:
			badge = sGood.Render("✓")
		case eng.Pinned(s.ID):
			badge = sWarn.Render("★")
		case m.briefGen[s.ID]:
			badge = sAcc.Render("◌")
		case m.isLive(s):
			g, st := liveGlyph(m.live[s.ID])
			badge = st.Render(g)
		case m.pre[s.ID]:
			badge = sWarn.Render("↻")
		case s.PR != "":
			badge = sDim.Render("↗")
		}
		mark := ""
		if s.Summary != "" {
			mark = "✦ "
		}
		if m.forkOnly && s.Fork {
			mark = "↳ " + mark
		}
		if r, ok := m.askRank[s.ID]; ok && m.askOnly {
			mark = fmt.Sprintf("%d· ", r)
		}
		fk := sAcc.Render(forkCol(s))
		tagCol := ""
		if tagW > 0 {
			tagCol = pad(eng.TagText(s.ID, " "), tagW) + " "
		}
		row := fmt.Sprintf(" %s%s %s %s %s %s%s",
			badge, fk,
			sDim.Render(pad(engine.Ago(s.MTime), 4)),
			dirStyle(s).Render(pad(eng.ShortDir(s.Cwd), 20)),
			highlight(mark+rowLabel(s), words, lipgloss.NewStyle(), hiN, titleW),
			sAcc.Render(tagCol),
			sDim.Render(fmt.Sprintf("%4d", s.Prompts)))
		if i == m.cur {
			// inner styles reset the background, so rebuild the row from plain text
			plain := fmt.Sprintf("%s%s %s %s %s %s%s", rawBadge(m, s), forkCol(s), pad(engine.Ago(s.MTime), 4),
				pad(eng.ShortDir(s.Cwd), 20), "\x00", tagCol, fmt.Sprintf("%4d", s.Prompts))
			pre, post, _ := strings.Cut(plain, "\x00")
			row = sCursor.Render("▌") + sSel.Render(" "+pre) + highlight(mark+rowLabel(s), words, sSel, hiS, titleW) +
				sSel.Render(pad(post, lw-1-1-lipgloss.Width(pre)-titleW))
		} else {
			row = " " + row
		}
		lines = append(lines, row)
	}
	for len(lines) < bodyH {
		lines = append(lines, "")
	}
	list := lipgloss.NewStyle().Width(lw).Render(strings.Join(lines, "\n"))
	body := list
	if dw > 0 {
		det := lipgloss.NewStyle().Width(dw-3).Height(bodyH).MaxHeight(bodyH).PaddingLeft(1).
			Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(divColor(m.dragging)).
			Render(m.detail(dw-4, bodyH))
		body = lipgloss.JoinHorizontal(lipgloss.Top, list, det)
	}

	// bar
	dir := "all"
	if m.dirf != "" {
		dir = eng.Short(m.dirf)
	}
	nsum := 0
	for _, s := range m.all {
		if s.Summary != "" {
			nsum++
		}
	}
	tagBar := ""
	if m.staleOnly {
		tagBar += " · stale"
	}
	if m.tagf != "" {
		tagBar += " · tag #" + m.tagf
	}
	bar := sDim.Render(fmt.Sprintf(" sort %s · dir %s%s · ★%d · ✦%d/%d", sorts[m.sortIdx], dir, tagBar, eng.PinCount(), nsum, len(m.all)))
	if len(m.live) > 0 {
		busy := 0
		for _, li := range m.live {
			if li.Status == "busy" {
				busy++
			}
		}
		lbl := fmt.Sprintf("● %d running", len(m.live))
		if busy > 0 {
			lbl += fmt.Sprintf(" (%d working)", busy)
		}
		if m.liveOnly {
			lbl += " (filter on · l = all)"
		}
		bar += "  " + sGood.Render(lbl)
	}
	if m.delAsk {
		bar += "  " + sBad.Render(fmt.Sprintf("Delete %d sessions? (moved to Trash)  y = yes · any other key = cancel", len(m.del)))
	}
	if len(m.marks) > 0 {
		bar += "  " + sGood.Render(fmt.Sprintf("✓%d marked (O opens · X deletes · u clears)", len(m.marks)))
	}
	if len(m.pre) > 0 {
		when := m.boot.Format("Mon 15:04")
		if m.preOnly {
			bar += "  " + sWarn.Render(fmt.Sprintf("↻ showing %d sessions from before reboot (%s) · O opens all · b = back", len(m.pre), when))
		} else {
			bar += "  " + sWarn.Render(fmt.Sprintf("↻%d before reboot (%s) · b to show", len(m.pre), when))
		}
	}
	if m.forkOnly {
		bar += "  " + sAcc.Render(forkGlyph+" forks and their originals · F = all")
	}
	if m.asking {
		bar += "  " + sAcc.Render("✦ asking claude…")
	}
	if m.msg != "" {
		bar += "  " + sWarn.Render(m.msg)
	}

	if m.pend != "" {
		verb := map[string]string{"resume": "Resume", "fork": "Fork", "open": fmt.Sprintf("Open %d sessions", len(m.pendSS)), "restore": fmt.Sprintf("Restore %d interrupted sessions", len(m.pendSS))}[m.pend]
		opts := "t = new tab here · w = new window · Esc = cancel"
		if m.pend == "resume" || m.pend == "fork" {
			opts = "t = new tab · w = new window · h or Enter = this terminal · Esc = cancel"
		}
		bar = " " + sAcc.Render(verb+" in:  ") + sWarn.Render(opts)
	}
	k := func(key, desc string) string { return sAcc.Render(key) + sDim.Render(" "+desc) }
	help := " " + strings.Join([]string{k("enter", "resume"), k("/", "search"), k("a", "ask claude"), k("p", "pin"),
		k("o", "sort"), k("d", "dir"), k("l", "live"), k("F", "forks"), k("space", "mark"), k("A", "mark all"), k("X", "delete marked"), k("b", "pre-reboot"), k("R", "restore"), k("O", "open marked"), k("?", "keys + legend"), k("q", "quit")}, "  ")
	box := m.in.View()
	if m.deepIn.Focused() {
		box = m.deepIn.View()
	} else if m.tagIn.Focused() {
		box = m.tagIn.View()
	} else if m.askIn.Focused() {
		box = m.askIn.View()
	} else if m.askOnly {
		label := " ✦ ask "
		if m.askKind == "deep" {
			label = " ⌕ deep "
		}
		box = sAcc.Render(label) + m.askQ
	}
	out := strings.Join([]string{head, box, body, bar, truncLines(help, m.w)}, "\n")
	if m.help {
		out = overlay(out, helpPopup(m.w, m.h), m.w, m.h)
	}
	return out
}

func span(s *session) string {
	if s.Started.IsZero() || s.MTime.Sub(s.Started) < time.Minute {
		return ""
	}
	d := s.MTime.Sub(s.Started)
	if d >= 48*time.Hour {
		return fmt.Sprintf(" · spanned %dd", int(d.Hours()/24))
	}
	if d >= time.Hour {
		return fmt.Sprintf(" · spanned %.0fh", d.Hours())
	}
	return fmt.Sprintf(" · spanned %dm", int(d.Minutes()))
}

func (m model) detail(w, h int) string {
	lines := m.detailLines(w)
	if lines == nil {
		return sDim.Render("no match")
	}
	scroll := min(m.dscroll, max(0, len(lines)-h))
	end := min(len(lines), scroll+h)
	view := append([]string(nil), lines[scroll:end]...)
	if scroll > 0 {
		view[0] = sDim.Render("▲ more above (K)")
	}
	if end < len(lines) {
		view[len(view)-1] = sDim.Render("▼ more below (J)")
	}
	return strings.Join(view, "\n")
}

// detailLines is the whole detail pane, wrapped to w columns. Rendering and click handling both use it.
func (m model) detailLines(w int) []string {
	s := m.selected()
	if s == nil {
		return nil
	}
	sec := func(name string) string { return "\n" + sHead.Render(name) + "\n" }
	var b strings.Builder
	b.WriteString(sBold.Render(s.Label()) + "\n")
	b.WriteString(sDim.Render(eng.Short(s.Cwd)) + "\n")
	if eng.DirGone(s) {
		b.WriteString(sBad.Render("✗ directory is gone, cannot resume") + "\n")
	}
	b.WriteString(actionRow(s) + "\n")
	if tg := eng.TagText(s.ID, " "); tg != "" {
		b.WriteString(sAcc.Render(tg) + sDim.Render("  · t edits") + "\n")
	}
	b.WriteString(sDim.Render(fmt.Sprintf("%s · %s · %d prompts%s", s.MTime.Format("Mon 02 Jan 15:04"), engine.AgoLong(s.MTime), s.Prompts, span(s))) + "\n")
	if s.PR != "" {
		b.WriteString(hyperlink(s.PR, sLink.Render(s.PR)) + sDim.Render("  · w opens") + "\n")
	}
	if s.ForkOf != nil {
		b.WriteString(sAcc.Render(forkGlyph+" fork of: ") + engine.Clean(s.ForkOf.Label(), 80) + sDim.Render(" · "+engine.AgoLong(s.ForkOf.MTime)) + "\n")
	} else if s.Fork {
		b.WriteString(sAcc.Render(forkGlyph+" fork") + sDim.Render(" · original not found") + "\n")
	}
	if s.Forks > 0 {
		b.WriteString(sAcc.Render(fmt.Sprintf("%s %d fork(s) made from this session", forkGlyph, s.Forks)) + "\n")
	}
	if why, ok := m.askWhy[s.ID]; ok && m.askOnly {
		b.WriteString(sAcc.Render(fmt.Sprintf("✦ match #%d: ", m.askRank[s.ID])) + why + "\n")
	}
	if r := eng.RepoOf(s.Cwd).String(); r != "" {
		b.WriteString(sDim.Render("⎇ "+r) + "\n")
	}
	if rs := repoLine(m.repoSt[s.ID]); rs != "" {
		b.WriteString(rs + "\n")
	}
	if li, ok := m.live[s.ID]; ok {
		g, st := liveGlyph(li)
		b.WriteString(st.Render(fmt.Sprintf("%s running now · pid %d · %s", g, li.PID, li.Status)) + "\n")
	} else if m.pre[s.ID] {
		b.WriteString(sWarn.Render("↻ was open when the machine restarted") + "\n")
	}
	b.WriteString(sec("BRIEF") + m.briefSection(s, w-1) + "\n")
	if fs := factsSection(s, w-1); fs != "" {
		b.WriteString(fs + "\n")
	}
	if hs := m.historySection(s, w-1); hs != "" {
		b.WriteString(hs + "\n")
	}
	b.WriteString(sDim.Render("claude --resume " + s.ID))

	return strings.Split(lipgloss.NewStyle().Width(w).Render(b.String()), "\n")
}

func main() {
	debug.SetGCPercent(25) // small heap, frequent GC: this is a short-lived UI, not a server
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--snapshot":
			st, _ := eng.Snapshot()
			fmt.Printf("live: %d, interrupted: %d\n", len(st.Live), len(st.Interrupted))
			return
		case "--install-agent":
			if err := eng.InstallAgent(); err != nil {
				fmt.Println("failed:", err)
				os.Exit(1)
			}
			fmt.Println("installed: snapshots running sessions every 3 min (" + eng.AgentPlist() + ")")
			return
		case "--uninstall-agent":
			fmt.Println("removed:", eng.UninstallAgent() == nil)
			return
		}
	}
	t0 := time.Now()
	all := eng.Load()
	debug.FreeOSMemory() // hand the scan's transient garbage back to the OS
	if os.Getenv("CCS_TIME") != "" {
		np, nt := 0, 0
		for _, s := range all {
			np += s.Prompts
			nt += len(s.Turns)
		}
		fmt.Printf("loaded %d sessions in %s (prompts %d, turns %d)\n", len(all), time.Since(t0), np, nt)
		return
	}
	if len(all) == 0 {
		fmt.Println("no sessions found")
		return
	}
	if len(os.Args) > 1 && (os.Args[1] == "--restore" || os.Args[1] == "--restore-list") {
		boot := engine.BootTime()
		pre := eng.PreBoot(all, boot)
		var ss []*session
		for _, s := range all {
			if pre[s.ID] {
				ss = append(ss, s)
			}
		}
		sort.Slice(ss, func(i, j int) bool { return ss[i].MTime.After(ss[j].MTime) })
		fmt.Printf("%d sessions were active before the reboot at %s\n", len(ss), boot.Format("Mon 02 Jan 15:04"))
		for _, s := range ss {
			fmt.Printf("  %s  %-22s %s\n", engine.Ago(s.MTime), eng.ShortDir(s.Cwd), engine.Clean(s.Label(), 60))
		}
		if os.Args[1] == "--restore" && len(ss) > 0 {
			n, err := eng.Open(ss, false, false)
			fmt.Printf("opened %d windows\n", n)
			if err != nil {
				fmt.Println(err)
			}
		}
		return
	}
	fm, err := tea.NewProgram(newModel(all), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	m := fm.(model)
	if m.result == nil {
		return
	}
	if d := m.result.Cwd; d != "" {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			os.Chdir(d)
		}
	}
	bin, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintln(os.Stderr, "claude not found")
		os.Exit(1)
	}
	args := []string{"claude", "--resume", m.result.ID}
	if m.fork {
		args = append(args, "--fork-session", "--name", engine.ForkName(m.result))
	}
	syscall.Exec(bin, args, os.Environ())
}

// dirStyle colors a row's directory red when it no longer exists.
func dirStyle(s *session) lipgloss.Style {
	if eng.DirGone(s) {
		return sBad
	}
	return sAcc
}

// colWidths splits the list width: the tag column exists only while some row has tags, and never squeezes the title below 10.
func (m model) colWidths(lw int) (titleW, tagW int) {
	for _, s := range m.rows {
		tagW = max(tagW, lipgloss.Width(eng.TagText(s.ID, " ")))
	}
	tagW = min(tagW, 22, max(0, lw-2-3-5-21-5-10-1))
	titleW = max(10, lw-2-3-5-21-5)
	if tagW > 0 {
		titleW = max(10, titleW-tagW-1)
	}
	return
}
