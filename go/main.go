// ccs-go: browse Claude Code sessions across all dirs. Enter resumes.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ───────────────────────── data ─────────────────────────

type session struct {
	ID, Path, Cwd, Title, AI, First, Last, PR, AText, Summary string
	Prompts                                                   int
	MTime, Started                                            time.Time
	PList                                                     []string
	Turns                                                     []turn
	hayC                                                      string
	UH                                                        []uint64 // hashed message uuids; a fork copies its original's
	Fork                                                      bool     // created by ccs fork (named with a leading [fork])
	ForkOf                                                    *session // best guess at the original
	Forks                                                     int      // forks made from this session
}

var (
	home       = os.Getenv("HOME")
	cacheDir   = filepath.Join(home, ".cache", "ccs")
	summaryF   = filepath.Join(cacheDir, "summaries.json")
	stateF     = filepath.Join(cacheDir, "state-go.json")
	wsRe       = regexp.MustCompile(`\s+`)
	pasteRe    = regexp.MustCompile(`<pasted_content[^>]*>.*`)
	noisePfx   = []string{"<local-command", "<command-name", "<command-message", "<system-reminder", "Caveat:", "[Request interrupted", "<task-notification", "<pasted_content", "<bash-", "<ide_"}
	summarySem = make(chan struct{}, 4)
)

var mdRe = strings.NewReplacer("```", "", "**", "", "###", "", "##", "")

func clean(s string, n int) string {
	s = mdRe.Replace(s)
	s = strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

type turn struct {
	user bool
	text string
}

func (s *session) addTurn(user bool, text string) {
	s.Turns = append(s.Turns, turn{user, text})
	if len(s.Turns) > 4 {
		s.Turns = s.Turns[1:]
	}
}

type rawLine struct {
	Timestamp   string `json:"timestamp"`
	UUID        string `json:"uuid"`
	Type        string `json:"type"`
	CustomTitle string `json:"customTitle"`
	AITitle     string `json:"aiTitle"`
	LastPrompt  string `json:"lastPrompt"`
	PRURL       string `json:"prUrl"`
	URL         string `json:"url"`
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	Cwd         string `json:"cwd"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

func textOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func scan(path string, cache map[string]string) *session {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, _ := f.Stat()
	s := &session{ID: strings.TrimSuffix(filepath.Base(path), ".jsonl"), Path: path, MTime: st.ModTime()}
	rd := bufio.NewReaderSize(f, 64<<10)
	for {
		line, err := rd.ReadBytes('\n')
		if len(line) > 2 && wanted(line) {
			var d rawLine
			if json.Unmarshal(line, &d) == nil {
				if d.UUID != "" && (d.Type == "user" || d.Type == "assistant") {
					s.UH = append(s.UH, h64(d.UUID))
				}
				switch d.Type {
				case "custom-title":
					if d.CustomTitle != "" {
						s.Title = d.CustomTitle
					}
				case "ai-title":
					if d.AITitle != "" {
						s.AI = d.AITitle
					}
				case "pr-link":
					if d.PRURL != "" {
						s.PR = d.PRURL
					} else if d.URL != "" {
						s.PR = d.URL
					}
				case "last-prompt":
					if c := clean(d.LastPrompt, 200); c != "" {
						s.Last = c
					}
				case "user":
					if d.IsMeta || d.IsSidechain {
						break
					}
					if s.Cwd == "" {
						s.Cwd = d.Cwd
					}
					if s.Started.IsZero() {
						if ts, err := time.Parse(time.RFC3339, d.Timestamp); err == nil {
							s.Started = ts
						}
					}
					txt := unwrapCmd(textOf(d.Message.Content))
					if txt != "" && !hasNoise(txt) {
						s.Prompts++
						c := clean(txt, 300)
						if s.First == "" {
							s.First = c
						}
						s.Last = c
						s.PList = append(s.PList, c)
						s.addTurn(true, c)
					}
				case "assistant":
					if d.IsSidechain {
						break
					}
					if at := textOf(d.Message.Content); len(at) > 40 {
						s.AText = clean(at, 500)
						s.addTurn(false, clean(at, 400))
					}
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				return nil
			}
			break
		}
	}
	if s.Prompts == 0 {
		return nil
	}
	s.Summary = cache[fmt.Sprintf("%s:%d", s.ID, s.MTime.Unix())]
	return s
}

var cmdRe = regexp.MustCompile(`(?s)<command-message>(.*?)</command-message>\s*<command-name>(.*?)</command-name>(?:\s*<command-args>(.*?)</command-args>)?`)

// slash-command prompts arrive as XML wrappers; show them as "/name args".
func unwrapCmd(t string) string {
	if m := cmdRe.FindStringSubmatch(t); m != nil {
		return strings.TrimSpace("/" + strings.TrimPrefix(m[2], "/") + " " + m[3])
	}
	return t
}

var wantedTypes = [][]byte{[]byte(`"type":"user"`), []byte(`"type":"assistant"`), []byte(`"type":"custom-title"`),
	[]byte(`"type":"ai-title"`), []byte(`"type":"pr-link"`), []byte(`"type":"last-prompt"`)}

// wanted is a cheap byte check so big attachment/snapshot lines are never JSON-decoded.
func wanted(line []byte) bool {
	// "type" sits in the first 400 bytes for most lines and in the last 700 for assistant lines
	// (their big message body comes first), so the huge middle never needs scanning.
	head, tail := line, []byte(nil)
	if len(line) > 1100 {
		head, tail = line[:400], line[len(line)-700:]
	}
	for _, w := range wantedTypes {
		if bytes.Contains(head, w) || (tail != nil && bytes.Contains(tail, w)) {
			return true
		}
	}
	return false
}

func hasNoise(t string) bool {
	t = strings.TrimLeft(t, " \n\t")
	if t == "/clear" || strings.HasPrefix(t, "/clear ") {
		return true
	}
	for _, p := range noisePfx {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

func loadCache() map[string]string {
	m := map[string]string{}
	if b, err := os.ReadFile(summaryF); err == nil {
		json.Unmarshal(b, &m)
	}
	return m
}

func saveSummary(key, text string) {
	m := loadCache() // read-modify-write so the python tool's entries survive
	m[key] = text
	os.MkdirAll(cacheDir, 0o755)
	b, _ := json.Marshal(m)
	os.WriteFile(summaryF, b, 0o644)
}

func loadAll() []*session {
	files, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", "*.jsonl"))
	cache := loadCache()
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out []*session
		sem = make(chan struct{}, 6)
	)
	for _, p := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(p string) {
			defer wg.Done()
			defer func() { <-sem }()
			if s := scan(p, cache); s != nil {
				mu.Lock()
				out = append(out, s)
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()
	linkForks(out)
	return out
}

func (s *session) label() string {
	for _, v := range []string{s.Title, s.AI, s.First} {
		if v != "" {
			return v
		}
	}
	return "(untitled)"
}

func short(p string) string {
	if p == "" {
		return "?"
	}
	return strings.Replace(p, home, "~", 1)
}

func shortDir(p string) string {
	parts := strings.Split(strings.TrimRight(short(p), "/"), "/")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return strings.Join(parts, "/")
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return "now"
}

func (m model) isLive(s *session) bool { _, ok := m.live[s.ID]; return ok }

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
	case m.pins[s.ID]:
		return "★"
	case m.busy[s.ID]:
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

var bootRe = regexp.MustCompile(`sec = (\d+)`)

func bootTime() time.Time {
	out, err := exec.Command("sysctl", "-n", "kern.boottime").Output()
	if err != nil {
		return time.Time{}
	}
	if m := bootRe.FindStringSubmatch(string(out)); m != nil {
		var sec int64
		fmt.Sscan(m[1], &sec)
		return time.Unix(sec, 0)
	}
	return time.Time{}
}

// preBoot finds the sessions that were in use when the machine last went down:
// everything active within 8h of the last activity before the boot.
func preBoot(all []*session, boot time.Time) map[string]bool {
	pre := map[string]bool{}
	st, hadHistory := snapshot()
	alive, _ := readPidFiles()
	for _, s := range all {
		if b, ok := st.Interrupted[s.ID]; ok {
			if _, running := alive[s.ID]; !running && s.MTime.Unix() <= b {
				pre[s.ID] = true
			}
		}
	}
	if len(pre) > 0 || hadHistory || boot.IsZero() || time.Since(boot) > 14*24*time.Hour {
		return pre
	}
	// no snapshot history yet: guess from file times (everything active within 8h before the boot)
	var last time.Time
	for _, s := range all {
		if s.MTime.Before(boot) && s.MTime.After(last) {
			last = s.MTime
		}
	}
	if last.IsZero() {
		return pre
	}
	for _, s := range all {
		if s.MTime.Before(boot) && s.MTime.After(last.Add(-8*time.Hour)) {
			pre[s.ID] = true
		}
	}
	return pre
}

// openSessions starts sessions in Ghostty, iTerm2 or Terminal.app. tab=false: one new window (Ghostty: a tab per session
// inside it); tab=true: new tab(s) in the front window, which is the one ccs is running in.
func openSessions(ss []*session, tab, fork bool) (int, error) {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return 0, fmt.Errorf("claude not found")
	}
	var cmds []string
	for _, s := range ss {
		dir := s.Cwd
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			dir = home
		}
		cmds = append(cmds, fmt.Sprintf("cd %q && %s", dir, resumeCmd(bin, s, fork)))
	}
	if os.Getenv("TERM_PROGRAM") == "ghostty" {
		return openGhostty(ss, bin, tab, fork)
	}
	n := 0
	for i, s := range ss {
		var err error
		switch os.Getenv("TERM_PROGRAM") {
		case "ghostty":
			dir := s.Cwd
			if st, e := os.Stat(dir); e != nil || !st.IsDir() {
				dir = home
			}
			// --working-directory is ignored when -e is given, so cd explicitly
			err = exec.Command("open", "-na", "Ghostty.app", "--args", "-e", "/bin/sh", "-c",
				fmt.Sprintf("cd %q && exec %s", dir, resumeCmd(bin, s, fork))).Run()
		case "iTerm.app":
			script := fmt.Sprintf(`tell application "iTerm2" to tell (create window with default profile) to tell current session to write text %q`, cmds[i])
			if tab {
				script = fmt.Sprintf(`tell application "iTerm2" to tell current window to tell (create tab with default profile) to tell current session to write text %q`, cmds[i])
			}
			err = exec.Command("osascript", "-e", script).Run()
		case "Apple_Terminal":
			script := fmt.Sprintf(`tell application "Terminal" to do script %q`, cmds[i])
			if tab {
				script = fmt.Sprintf(`tell application "Terminal" to do script %q in front window`, cmds[i])
			}
			err = exec.Command("osascript", "-e", script).Run()
		default:
			c := exec.Command("pbcopy")
			c.Stdin = strings.NewReader(strings.Join(cmds, "\n"))
			c.Run()
			return 0, fmt.Errorf("unknown terminal: copied %d resume commands to clipboard", len(cmds))
		}
		if err != nil {
			return n, err
		}
		n++
		time.Sleep(400 * time.Millisecond)
	}
	return n, nil
}

// openGhostty puts every session in one new Ghostty window, one tab each (Ghostty 1.3+ AppleScript).
// With tab=true the tabs join the front window instead (a new window only if none is open).
func openGhostty(ss []*session, bin string, tab, fork bool) (int, error) {
	var b strings.Builder
	b.WriteString("tell application \"Ghostty\"\n  set win to missing value\n")
	if tab {
		b.WriteString("  if (count of windows) > 0 then set win to front window\n")
	}
	for _, s := range ss {
		dir := s.Cwd
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			dir = home
		}
		fmt.Fprintf(&b, "  set cfg to new surface configuration\n  set initial working directory of cfg to %q\n"+
			"  set command of cfg to %q\n  set wait after command of cfg to true\n"+
			"  if win is missing value then\n    set win to new window with configuration cfg\n"+
			"  else\n    new tab in win with configuration cfg\n  end if\n",
			dir, resumeCmd(bin, s, fork))
	}
	b.WriteString("end tell\n")
	if out, err := exec.Command("osascript", "-e", b.String()).CombinedOutput(); err != nil {
		return 0, fmt.Errorf("ghostty: %s", strings.TrimSpace(string(out)))
	}
	return len(ss), nil
}

// deleteSessions moves transcripts (and their companion dir) to ~/.Trash/claude-sessions. Running sessions are skipped.
func deleteSessions(ss []*session, live map[string]liveInfo) (moves []move, skipped int, err error) {
	for _, s := range ss {
		if _, ok := live[s.ID]; ok {
			skipped++
			continue
		}
		proj := filepath.Base(filepath.Dir(s.Path))
		dst := filepath.Join(home, ".Trash", "claude-sessions", proj)
		if err = os.MkdirAll(dst, 0o755); err != nil {
			return
		}
		stamp := time.Now().Format("150405")
		target := filepath.Join(dst, s.ID+".jsonl")
		if _, e := os.Stat(target); e == nil {
			target = filepath.Join(dst, s.ID+"-"+stamp+".jsonl")
		}
		if err = os.Rename(s.Path, target); err != nil {
			return
		}
		mv := move{Src: s.Path, Dst: target}
		comp := strings.TrimSuffix(s.Path, ".jsonl") // subagent/tool-result dir, if any
		if st, e := os.Stat(comp); e == nil && st.IsDir() {
			cd := strings.TrimSuffix(target, ".jsonl")
			if os.Rename(comp, cd) == nil {
				mv.CompSrc, mv.CompDst = comp, cd
			}
		}
		moves = append(moves, mv)
	}
	return
}

func shellJoin(a ...string) string {
	for i, s := range a {
		a[i] = "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return strings.Join(a, " ")
}

func agoLong(t time.Time) string {
	if a := ago(t); a != "now" {
		return a + " ago"
	}
	return "just now"
}

// excerpt: first ask ⟶ last ask, or the LLM summary when cached.
func (s *session) excerpt() string {
	if s.Summary != "" {
		return s.Summary
	}
	var good []string
	for _, p := range s.PList {
		if len(strings.Fields(p)) >= 5 {
			good = append(good, pasteRe.ReplaceAllString(p, "[paste]"))
		}
	}
	switch {
	case len(good) == 0:
		return s.Last
	case len(good) == 1 || good[0] == good[len(good)-1]:
		return good[0]
	}
	return clean(good[0], 55) + "  ⟶  " + clean(good[len(good)-1], 55)
}

func (s *session) hay() string {
	if s.hayC == "" {
		s.hayC = strings.ToLower(strings.Join(append([]string{s.label(), s.Summary, short(s.Cwd), s.PR, tagText(s.ID, " ")}, s.PList...), " "))
	}
	return s.hayC
}

// ───────────────────────── state ─────────────────────────

type state struct {
	Pins []string            `json:"pins"`
	Sort string              `json:"sort"`
	Tags map[string][]string `json:"tags,omitempty"`
}

func loadState() (map[string]bool, string) {
	pins, sortBy := map[string]bool{}, "recent"
	var st state
	if b, err := os.ReadFile(stateF); err == nil && json.Unmarshal(b, &st) == nil {
		for _, p := range st.Pins {
			pins[p] = true
		}
		if st.Sort != "" {
			sortBy = st.Sort
		}
		if st.Tags != nil {
			tagStore = st.Tags
		}
	}
	return pins, sortBy
}

func (m *model) saveState() {
	st := state{Sort: sorts[m.sortIdx], Tags: tagStore}
	for p := range m.pins {
		st.Pins = append(st.Pins, p)
	}
	sort.Strings(st.Pins)
	os.MkdirAll(cacheDir, 0o755)
	b, _ := json.Marshal(st)
	os.WriteFile(stateF, b, 0o644)
}

// ───────────────────────── model ─────────────────────────

var sorts = []string{"recent", "dir", "prompts", "oldest", "tags"}

type (
	summaryMsg struct {
		id, key, text string
		err           error
	}
	clearMsg struct{ seq int }
	tickMsg  time.Time
)

type model struct {
	all         []*session
	rows        []*session
	pins        map[string]bool
	sortIdx     int
	dirf        string
	tagf        string // only sessions with this tag
	rev         bool   // reverse the current sort (click the active header again)
	in          textinput.Model
	cur         int
	top         *int
	w, h        int
	preview     bool
	help        bool
	busy        map[string]bool
	msg         string
	msgSeq      int
	result      *session
	fork        bool
	dscroll     int
	lastSel     string
	boot        time.Time
	pre         map[string]bool
	preOnly     bool
	marks       map[string]bool
	live        map[string]liveInfo
	confirm     string
	liveOnly    bool
	del         map[string]bool
	delAsk      bool
	restoreAsk  bool
	pend        string // pending open: resume | fork | open | restore; waits for t / w / h
	pendSS      []*session
	tickN       int
	forkOnly    bool
	reloading   bool
	askIn       textinput.Model
	tagIn       textinput.Model
	tagFor      string // session ID being tagged
	tagging     bool   // an auto-tag call is running
	briefOn     bool   // the brief popup is open
	briefBusy   bool
	brief       string
	briefID     string
	briefScroll int
	asking      bool
	askKind     string // "" or "deep": what produced the current result list
	deepIn      textinput.Model
	staleOnly   bool
	askOnly     bool
	askQ        string
	askWhy      map[string]string
	askRank     map[string]int
}

// layout returns list width, detail width, body height.
func (m model) layout() (lw, dw, bodyH int) {
	bodyH = max(3, m.h-5)
	lw = m.w
	if m.preview && m.w >= 90 {
		lw = m.w * 58 / 100
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
	pins, sortBy := loadState()
	idx := 0
	for i, s := range sorts {
		if s == sortBy {
			idx = i
		}
	}
	if q := strings.Join(os.Args[1:], " "); q != "" {
		ti.SetValue(q)
	}
	boot := bootTime()
	pre := preBoot(all, boot)
	live, _ := readPidFiles()
	m := model{askIn: ai, deepIn: di, tagIn: tg, askWhy: map[string]string{}, askRank: map[string]int{}, del: map[string]bool{}, live: live, boot: boot, pre: pre, marks: map[string]bool{}, preOnly: len(pre) > 0 && time.Since(boot) < 90*time.Minute, top: new(int), all: all, pins: pins, sortIdx: idx, in: ti, preview: true, busy: map[string]bool{}}
	m.refilter()
	return m
}

func (m *model) refilter() {
	words := strings.Fields(strings.ToLower(m.in.Value()))
	rows := make([]*session, 0, len(m.all))
	for _, s := range m.all {
		if m.dirf != "" && s.Cwd != m.dirf {
			continue
		}
		if m.tagf != "" && !hasTag(s.ID, m.tagf) {
			continue
		}
		if m.staleOnly && !m.isStale(s) {
			continue
		}
		if m.preOnly && !m.pre[s.ID] {
			continue
		}
		if _, ok := m.askRank[s.ID]; m.askOnly && !ok {
			continue
		}
		if m.forkOnly && !s.Fork && s.Forks == 0 {
			continue
		}
		if m.liveOnly && !m.isLive(s) {
			continue
		}
		h, ok := s.hay(), true
		for _, w := range words {
			if !strings.Contains(h, w) && !fuzzyShort(s, w) {
				ok = false
				break
			}
		}
		if ok {
			rows = append(rows, s)
		}
	}
	less := map[string]func(a, b *session) bool{
		"tags": func(a, b *session) bool {
			ta, tb := firstTag(a.ID), firstTag(b.ID)
			if ta != tb {
				if ta == "" || tb == "" {
					return tb == ""
				}
				return ta < tb
			}
			return a.MTime.After(b.MTime)
		},
		"recent":  func(a, b *session) bool { return a.MTime.After(b.MTime) },
		"oldest":  func(a, b *session) bool { return a.MTime.Before(b.MTime) },
		"prompts": func(a, b *session) bool { return a.Prompts > b.Prompts },
		"dir": func(a, b *session) bool {
			if a.Cwd != b.Cwd {
				return short(a.Cwd) < short(b.Cwd)
			}
			return a.MTime.After(b.MTime)
		},
	}[sorts[m.sortIdx]]
	if m.rev {
		base := less
		less = func(a, b *session) bool { return base(b, a) }
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if m.askOnly {
			return m.askRank[rows[i].ID] < m.askRank[rows[j].ID]
		}
		pi, pj := m.pins[rows[i].ID], m.pins[rows[j].ID]
		if pi != pj {
			return pi
		}
		return less(rows[i], rows[j])
	})
	if m.forkOnly && !m.askOnly {
		sortForkGroups(rows)
	}
	m.rows = rows
	if m.cur >= len(rows) {
		m.cur = len(rows) - 1
	}
	if m.cur < 0 {
		m.cur = 0
	}
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

const prompt = "Summarize this Claude Code session in max 25 words, plain text, no preamble: " +
	"what it was about, then where it ended or what is left. Session excerpt follows.\n\n"

func summarize(s *session) tea.Cmd {
	return func() tea.Msg {
		summarySem <- struct{}{}
		defer func() { <-summarySem }()
		n := len(s.PList)
		first, last := s.PList, s.PList
		if n > 3 {
			first, last = s.PList[:3], s.PList[n-3:]
		}
		ctx := fmt.Sprintf("Title: %s\nDir: %s\nFirst prompts:\n- %s\nLast prompts:\n- %s\nLast assistant message: %s",
			s.label(), short(s.Cwd), strings.Join(first, "\n- "), strings.Join(last, "\n- "), s.AText)
		c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(c, "claude", "-p", "--model", "haiku", "--no-session-persistence", prompt+ctx)
		cmd.Dir = os.TempDir()
		for _, e := range os.Environ() { // an invalid ANTHROPIC_API_KEY makes claude -p 401; use the login instead
			if !strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
				cmd.Env = append(cmd.Env, e)
			}
		}
		out, err := cmd.Output()
		txt := clean(string(out), 300)
		if err == nil && txt == "" {
			err = fmt.Errorf("empty output")
		}
		return summaryMsg{id: s.ID, key: fmt.Sprintf("%s:%d", s.ID, s.MTime.Unix()), text: txt, err: err}
	}
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
	return mm, cmd
}

func (m model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		lw, dw, _ := m.layout()
		switch {
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
		m.live = readPidFilesFast(m.boot)
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
			return m, tea.Batch(next, reloadCmd(realSessions(m.all)))
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
		linkForks(m.all)
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
	case summaryMsg:
		delete(m.busy, msg.id)
		if msg.err != nil {
			return m, m.flash("summary failed: " + msg.err.Error())
		}
		saveSummary(msg.key, msg.text)
		var tagCmd tea.Cmd
		for _, s := range m.all {
			if s.ID == msg.id {
				s.Summary = msg.text
				s.hayC = ""
				if len(tagStore[s.ID]) == 0 && !m.tagging { // a freshly summarized session gets tags too
					m.tagging = true
					tagCmd = autoTag([]*session{s})
				}
			}
		}
		m.refilter()
		return m, tagCmd
	case briefMsg:
		if msg.err == nil {
			saveBrief(msg.key, msg.text)
		}
		if m.briefOn && m.briefID == msg.id {
			m.briefBusy = false
			if msg.err != nil {
				m.briefOn = false
				return m, m.flash("brief failed: " + msg.err.Error())
			}
			m.brief = msg.text
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
			tagStore[id] = parseTags(strings.Join(append(append([]string{}, tagStore[id]...), tags...), " "))
			for _, s := range m.all {
				if s.ID == id {
					s.hayC = ""
				}
			}
		}
		m.saveState()
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
				if tg := parseTags(m.tagIn.Value()); len(tg) > 0 {
					tagStore[id] = tg
				} else {
					delete(tagStore, id)
				}
				m.tagIn.SetValue("")
				for _, s := range m.all {
					if s.ID == id {
						s.hayC = ""
					}
				}
				m.saveState()
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
		if m.briefOn {
			switch msg.String() {
			case "esc", "q", "S", "enter":
				m.briefOn = false
			case "j", "down":
				m.briefScroll++
			case "k", "up":
				m.briefScroll = max(0, m.briefScroll-1)
			case "pgdown", " ":
				m.briefScroll += 8
			case "pgup":
				m.briefScroll = max(0, m.briefScroll-8)
			case "r":
				if s := m.selected(); s != nil && !m.briefBusy {
					m.brief, m.briefBusy, m.briefScroll = "", true, 0
					return m, briefSession(s)
				}
			}
			return m, nil
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
				n, err := openSessions(ss, tab, p == "fork")
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
				moves, skipped, err := deleteSessions(ss, m.live)
				saveUndo(moves)
				n := len(moves)
				gone := map[string]bool{}
				for _, s := range ss {
					if _, err := os.Stat(s.Path); err != nil {
						gone[s.ID] = true
					}
				}
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
		case "a":
			m.askIn.Focus()
			return m, textinput.Blink
		case "S":
			s := m.selected()
			if s == nil || s.Path == "" || s.Prompts == 0 {
				return m, m.flash("no transcript yet: ask something in that session first")
			}
			m.briefOn, m.briefID, m.briefScroll = true, s.ID, 0
			if txt, ok := loadBriefs()[briefKey(s)]; ok {
				m.brief, m.briefBusy = txt, false
				return m, nil
			}
			m.brief, m.briefBusy = "", true
			return m, briefSession(s)
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
				if m.marks[s.ID] && s.Prompts > 0 && len(ss) < autoTagMax {
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
				m.tagIn.SetValue(tagText(s.ID, " "))
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
				if m.pins[s.ID] {
					delete(m.pins, s.ID)
				} else {
					m.pins[s.ID] = true
				}
				m.saveState()
				m.refilter()
				for i, r := range m.rows { // follow the row to its new position
					if r.ID == s.ID {
						m.cur = i
					}
				}
			}
		case "o":
			m.rev = false
			m.sortIdx = (m.sortIdx + 1) % len(sorts)
			m.saveState()
			m.refilter()
			return m, m.flash("sort: " + sorts[m.sortIdx])
		case "#":
			tags := tagList()
			if len(tags) == 0 {
				return m, m.flash("no tags yet: t tags a session, T auto-tags")
			}
			next := ""
			if m.tagf == "" {
				next = tags[0].name
			} else {
				for i, tg := range tags {
					if tg.name == m.tagf && i+1 < len(tags) {
						next = tags[i+1].name
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
				if tg.name == next {
					n = tg.n
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
				c.Stdin = strings.NewReader(fmt.Sprintf("cd %s && claude --resume %s", short(s.Cwd), s.ID))
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
			n, err := undoDelete()
			if n == 0 {
				return m, m.flash("nothing to undo")
			}
			m.all = loadAll()
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
			m.all = loadAll()
			m.syncStubs()
			m.refilter()
			return m, m.flash(fmt.Sprintf("reloaded %d sessions", len(m.all)))
		case "s":
			if s := m.selected(); s != nil && !m.busy[s.ID] {
				m.busy[s.ID] = true
				return m, tea.Batch(summarize(s), m.flash("summarizing…"))
			}
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
		case m.pins[s.ID]:
			badge = sWarn.Render("★")
		case m.busy[s.ID]:
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
			tagCol = pad(tagText(s.ID, " "), tagW) + " "
		}
		row := fmt.Sprintf(" %s%s %s %s %s %s%s",
			badge, fk,
			sDim.Render(pad(ago(s.MTime), 4)),
			dirStyle(s).Render(pad(shortDir(s.Cwd), 20)),
			highlight(mark+rowLabel(s), words, lipgloss.NewStyle(), hiN, titleW),
			sAcc.Render(tagCol),
			sDim.Render(fmt.Sprintf("%4d", s.Prompts)))
		if i == m.cur {
			// inner styles reset the background, so rebuild the row from plain text
			plain := fmt.Sprintf("%s%s %s %s %s %s%s", rawBadge(m, s), forkCol(s), pad(ago(s.MTime), 4),
				pad(shortDir(s.Cwd), 20), "\x00", tagCol, fmt.Sprintf("%4d", s.Prompts))
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
			Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(cDim).
			Render(m.detail(dw-4, bodyH))
		body = lipgloss.JoinHorizontal(lipgloss.Top, list, det)
	}

	// bar
	dir := "all"
	if m.dirf != "" {
		dir = short(m.dirf)
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
	bar := sDim.Render(fmt.Sprintf(" sort %s · dir %s%s · ★%d · ✦%d/%d", sorts[m.sortIdx], dir, tagBar, len(m.pins), nsum, len(m.all)))
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
	help := " " + strings.Join([]string{k("enter", "resume"), k("/", "search"), k("a", "ask claude"), k("s", "summarize"), k("p", "pin"),
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
	if m.briefOn {
		out = overlay(out, m.briefPopup(), m.w, m.h)
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
	s := m.selected()
	if s == nil {
		return sDim.Render("no match")
	}
	sec := func(name string) string { return "\n" + sHead.Render(name) + "\n" }
	var b strings.Builder
	b.WriteString(sBold.Render(s.label()) + "\n")
	b.WriteString(sDim.Render(short(s.Cwd)) + "\n")
	if dirGone(s) {
		b.WriteString(sBad.Render("✗ directory is gone, cannot resume") + "\n")
	}
	if tg := tagText(s.ID, " "); tg != "" {
		b.WriteString(sAcc.Render(tg) + sDim.Render("  · t edits") + "\n")
	}
	b.WriteString(sDim.Render(fmt.Sprintf("%s · %s · %d prompts%s", s.MTime.Format("Mon 02 Jan 15:04"), agoLong(s.MTime), s.Prompts, span(s))) + "\n")
	if s.PR != "" {
		b.WriteString(sLink.Render(s.PR) + "\n")
	}
	if s.ForkOf != nil {
		b.WriteString(sAcc.Render(forkGlyph+" fork of: ") + clean(s.ForkOf.label(), 80) + sDim.Render(" · "+agoLong(s.ForkOf.MTime)) + "\n")
	} else if s.Fork {
		b.WriteString(sAcc.Render(forkGlyph+" fork") + sDim.Render(" · original not found") + "\n")
	}
	if s.Forks > 0 {
		b.WriteString(sAcc.Render(fmt.Sprintf("%s %d fork(s) made from this session", forkGlyph, s.Forks)) + "\n")
	}
	if why, ok := m.askWhy[s.ID]; ok && m.askOnly {
		b.WriteString(sAcc.Render(fmt.Sprintf("✦ match #%d: ", m.askRank[s.ID])) + why + "\n")
	}
	if r := repoOf(s.Cwd).String(); r != "" {
		b.WriteString(sDim.Render("⎇ "+r) + "\n")
	}
	if li, ok := m.live[s.ID]; ok {
		g, st := liveGlyph(li)
		b.WriteString(st.Render(fmt.Sprintf("%s running now · pid %d · %s", g, li.PID, li.Status)) + "\n")
	} else if m.pre[s.ID] {
		b.WriteString(sWarn.Render("↻ was open when the machine restarted") + "\n")
	}
	switch {
	case s.Summary != "":
		b.WriteString(sec("SUMMARY") + sGood.Render(s.Summary) + "\n")
	case m.busy[s.ID]:
		b.WriteString(sec("SUMMARY") + sDim.Render("generating…") + "\n")
	default:
		b.WriteString(sec("SUMMARY") + sDim.Render("press s to generate") + "\n")
	}
	b.WriteString(sec("STARTED WITH") + clean(s.First, 400) + "\n")
	if len(s.Turns) > 0 {
		b.WriteString(sec("HOW IT ENDED"))
		for _, t := range s.Turns {
			who := sDim.Render("claude ›")
			if t.user {
				who = sAcc.Render("you ›")
			}
			b.WriteString(who + " " + t.text + "\n\n")
		}
	}
	b.WriteString(sDim.Render("claude --resume " + s.ID))

	lines := strings.Split(lipgloss.NewStyle().Width(w).Render(b.String()), "\n")
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

func main() {
	debug.SetGCPercent(25) // small heap, frequent GC: this is a short-lived UI, not a server
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--snapshot":
			st, _ := snapshot()
			fmt.Printf("live: %d, interrupted: %d\n", len(st.Live), len(st.Interrupted))
			return
		case "--install-agent":
			if err := installAgent(); err != nil {
				fmt.Println("failed:", err)
				os.Exit(1)
			}
			fmt.Println("installed: snapshots running sessions every 3 min (" + agentPlist() + ")")
			return
		case "--uninstall-agent":
			fmt.Println("removed:", uninstallAgent() == nil)
			return
		}
	}
	t0 := time.Now()
	all := loadAll()
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
		boot := bootTime()
		pre := preBoot(all, boot)
		var ss []*session
		for _, s := range all {
			if pre[s.ID] {
				ss = append(ss, s)
			}
		}
		sort.Slice(ss, func(i, j int) bool { return ss[i].MTime.After(ss[j].MTime) })
		fmt.Printf("%d sessions were active before the reboot at %s\n", len(ss), boot.Format("Mon 02 Jan 15:04"))
		for _, s := range ss {
			fmt.Printf("  %s  %-22s %s\n", ago(s.MTime), shortDir(s.Cwd), clean(s.label(), 60))
		}
		if os.Args[1] == "--restore" && len(ss) > 0 {
			n, err := openSessions(ss, false, false)
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
		args = append(args, "--fork-session", "--name", forkName(m.result))
	}
	syscall.Exec(bin, args, os.Environ())
}

// dirStyle colors a row's directory red when it no longer exists.
func dirStyle(s *session) lipgloss.Style {
	if dirGone(s) {
		return sBad
	}
	return sAcc
}

// colWidths splits the list width: the tag column exists only while some row has tags, and never squeezes the title below 10.
func (m model) colWidths(lw int) (titleW, tagW int) {
	for _, s := range m.rows {
		tagW = max(tagW, lipgloss.Width(tagText(s.ID, " ")))
	}
	tagW = min(tagW, 22, max(0, lw-2-3-5-21-5-10-1))
	titleW = max(10, lw-2-3-5-21-5)
	if tagW > 0 {
		titleW = max(10, titleW-tagW-1)
	}
	return
}
