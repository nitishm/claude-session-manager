package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ───────────────────────── repo info (no exec: endpoint-security agents scan every fork) ─────────────────────────

type repoInfo struct {
	Kind, Branch, Root string
}

type repoEntry struct {
	info repoInfo
	at   time.Time
}

var repoCache = map[string]repoEntry{}

// repoOf reads .git/HEAD directly. jj repos report no branch (bookmarks need the jj binary).
func repoOf(dir string) repoInfo {
	if e, ok := repoCache[dir]; ok && time.Since(e.at) < 15*time.Second {
		return e.info
	}
	r := findRepo(dir)
	repoCache[dir] = repoEntry{r, time.Now()}
	return r
}

func findRepo(dir string) repoInfo {
	for d := dir; d != "/" && d != "." && d != ""; d = filepath.Dir(d) {
		jj := false
		if st, err := os.Stat(filepath.Join(d, ".jj")); err == nil && st.IsDir() {
			jj = true
		}
		gp := filepath.Join(d, ".git")
		if st, err := os.Stat(gp); err == nil {
			head := gp
			if !st.IsDir() { // linked worktree: .git is a file "gitdir: <path>"
				b, _ := os.ReadFile(gp)
				head = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(b)), "gitdir:"))
				if !filepath.IsAbs(head) {
					head = filepath.Join(d, head)
				}
			}
			b, _ := os.ReadFile(filepath.Join(head, "HEAD"))
			s := strings.TrimSpace(string(b))
			r := repoInfo{Kind: "git", Root: d}
			if jj {
				r.Kind = "jj"
			} else if br, ok := strings.CutPrefix(s, "ref: refs/heads/"); ok {
				r.Branch = br
			} else if len(s) >= 7 {
				r.Branch = s[:7] + " (detached)"
			}
			return r
		}
		if jj {
			return repoInfo{Kind: "jj", Root: d}
		}
	}
	return repoInfo{}
}

func (r repoInfo) String() string {
	switch {
	case r.Kind == "":
		return ""
	case r.Branch != "":
		return r.Kind + " · " + r.Branch
	}
	return r.Kind
}

// ───────────────────────── fuzzy match + highlight ─────────────────────────

// matchPos finds word in text: a plain substring first, else a tight subsequence
// (each next char within 3 positions of the last). Returns matched rune indexes.
func matchPos(lt, lw []rune, fuzzy bool) []int {
	if len(lw) == 0 || len(lt) < len(lw) {
		return nil
	}
	for i := 0; i+len(lw) <= len(lt); i++ {
		j := 0
		for j < len(lw) && lt[i+j] == lw[j] {
			j++
		}
		if j == len(lw) {
			pos := make([]int, len(lw))
			for k := range pos {
				pos[k] = i + k
			}
			return pos
		}
	}
	if !fuzzy || len(lw) < 3 {
		return nil
	}
	for i := range lt {
		if lt[i] != lw[0] {
			continue
		}
		pos, last := []int{i}, i
		for k := 1; k < len(lw); k++ {
			found := -1
			for p := last + 1; p <= last+3 && p < len(lt); p++ {
				if lt[p] == lw[k] {
					found = p
					break
				}
			}
			if found < 0 {
				break
			}
			pos, last = append(pos, found), found
		}
		if len(pos) == len(lw) {
			return pos
		}
	}
	return nil
}

// fuzzyShort: does word fuzzy-match the short fields (title, dir)? Used when the full haystack misses.
func fuzzyShort(s *session, w string) bool {
	return matchPos([]rune(strings.ToLower(s.label()+" "+shortDir(s.Cwd))), []rune(w), true) != nil
}

// highlight renders text truncated/padded to w, with search-word matches in hi.
func highlight(text string, words []string, base, hi lipgloss.Style, w int) string {
	text = ansi.Truncate(text, w, "…")
	rs := []rune(text)
	lt := []rune(strings.ToLower(text))
	mark := make([]bool, len(rs))
	if len(lt) == len(rs) {
		for _, wd := range words {
			for _, p := range matchPos(lt, []rune(wd), true) {
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

type move struct{ Src, Dst, CompSrc, CompDst string }

var undoF = filepath.Join(cacheDir, "undo.json")

func saveUndo(ms []move) {
	if len(ms) == 0 {
		return
	}
	os.MkdirAll(cacheDir, 0o755)
	b, _ := json.Marshal(ms)
	os.WriteFile(undoF, b, 0o644)
}

// undoDelete puts the last deleted batch back. Never overwrites an existing transcript.
func undoDelete() (n int, err error) {
	b, err := os.ReadFile(undoF)
	if err != nil {
		return 0, os.ErrNotExist
	}
	var ms []move
	if err = json.Unmarshal(b, &ms); err != nil {
		return 0, err
	}
	for _, m := range ms {
		if _, e := os.Stat(m.Src); e == nil {
			continue
		}
		os.MkdirAll(filepath.Dir(m.Src), 0o755)
		if e := os.Rename(m.Dst, m.Src); e != nil {
			err = e
			continue
		}
		if m.CompDst != "" {
			os.Rename(m.CompDst, m.CompSrc)
		}
		n++
	}
	os.Remove(undoF)
	return n, err
}

// forkName labels a fork so it can be told apart from the original in /resume and in ccs.
func forkName(s *session) string {
	return forkMark + clean(s.label(), 50) + " · " + time.Now().Format("15:04")
}

// resumeCmd is the shell-quoted command that resumes (or forks and renames) a session.
func resumeCmd(bin string, s *session, fork bool) string {
	a := []string{bin, "--resume", s.ID}
	if fork {
		a = append(a, "--fork-session", "--name", forkName(s))
	}
	return shellJoin(a...)
}

// ───────────────────────── forks ─────────────────────────

const (
	forkMark   = "[fork] "
	legacyMark = "⑂ " // names written by the first version of the fork command
	forkGlyph  = "⎇"  // Nerd Font branch glyph, shown in its own column next to the status badge
)

func isForkTitle(t string) bool {
	return strings.HasPrefix(t, forkMark) || strings.HasPrefix(t, legacyMark)
}

// forkCol is the one-cell fork column of a list row.
func forkCol(s *session) string {
	if s.Fork {
		return forkGlyph
	}
	return " "
}

// h64 is FNV-1a: a compact stand-in for a message uuid.
func h64(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

// linkForks marks sessions that ccs forked (named "[fork] …") and links each to its original.
// Transcripts store no parent id, but a fork carries copies of the original's messages with the
// same uuids, so the original is the other session sharing the most of them (3 or more). That
// survives compaction, where the fork no longer starts with the original's first prompt.
// Non-fork sessions win over other forks, so a sibling fork is never taken as the parent.
func linkForks(all []*session) {
	for _, s := range all {
		s.Fork, s.ForkOf, s.Forks = false, nil, 0
	}
	for _, s := range all {
		named := isForkTitle(s.Title)
		if !named && (s.Started.IsZero() || len(s.UH) < 3) {
			continue
		}
		mine := make(map[uint64]struct{}, len(s.UH))
		for _, h := range s.UH {
			mine[h] = struct{}{}
		}
		var best *session
		bestN, bestFork := 0, false
		for _, c := range all {
			if c == s || (!named && (c.Started.IsZero() || !c.Started.Before(s.Started))) {
				continue // an unnamed fork is only ever the later of the pair
			}
			n := 0
			for _, h := range c.UH {
				if _, ok := mine[h]; ok {
					n++
				}
			}
			cf := isForkTitle(c.Title)
			switch {
			case n < 3:
			case best == nil, bestFork && !cf, cf == bestFork && n > bestN:
				best, bestN, bestFork = c, n, cf
			}
		}
		if best != nil {
			s.Fork, s.ForkOf = true, best
			best.Forks++
		} else if named {
			s.Fork = true
			if s.Path == "" { // placeholder: no history yet, so match on the name the fork was given
				if o := forkOrigByName(s, all); o != nil {
					s.ForkOf = o
					o.Forks++
				}
			}
		}
	}
}

// ───────────────────────── incremental reload ─────────────────────────

type reloadMsg struct {
	all     []*session
	changed bool
}

// emptyAt remembers transcripts that held no real prompts, so they are not rescanned every time.
var emptyAt = map[string]time.Time{}

// reloadCmd rescans only transcripts whose mtime changed; unchanged sessions keep their parsed
// data. Runs off the UI goroutine and touches none of the shared sessions.
func reloadCmd(prev []*session) tea.Cmd {
	return func() tea.Msg {
		files, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", "*.jsonl"))
		old := make(map[string]*session, len(prev))
		for _, s := range prev {
			old[s.Path] = s
		}
		var cache map[string]string
		var out []*session
		changed := false
		for _, p := range files {
			st, err := os.Stat(p)
			if err != nil {
				continue
			}
			if o, ok := old[p]; ok && o.MTime.Equal(st.ModTime()) {
				out = append(out, o)
				continue
			}
			if t, ok := emptyAt[p]; ok && t.Equal(st.ModTime()) {
				continue
			}
			if cache == nil {
				cache = loadCache()
			}
			s := scan(p, cache)
			if s == nil {
				emptyAt[p] = st.ModTime()
				if _, was := old[p]; was {
					changed = true
				}
				continue
			}
			changed = true
			out = append(out, s)
		}
		if len(out) != len(prev) {
			changed = true
		}
		if changed {
			debug.FreeOSMemory()
		}
		return reloadMsg{out, changed}
	}
}

// truncLines cuts every line of s to w columns.
func truncLines(s string, w int) string {
	ls := strings.Split(s, "\n")
	for i, l := range ls {
		ls[i] = ansi.Truncate(l, w, "")
	}
	return strings.Join(ls, "\n")
}

func forkRoot(s *session) string {
	if s.Fork && s.ForkOf != nil {
		return s.ForkOf.ID
	}
	return s.ID
}

// sortForkGroups puts each original directly above its forks; the most recently active group comes first.
func sortForkGroups(rows []*session) {
	gmax := map[string]time.Time{}
	for _, r := range rows {
		if k := forkRoot(r); r.MTime.After(gmax[k]) {
			gmax[k] = r.MTime
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if ka, kb := forkRoot(a), forkRoot(b); ka != kb {
			return gmax[ka].After(gmax[kb])
		}
		if a.Fork != b.Fork {
			return !a.Fork
		}
		return a.MTime.After(b.MTime)
	})
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
	l := s.label()
	if s.Fork {
		l = strings.TrimPrefix(strings.TrimPrefix(l, forkMark), legacyMark)
	}
	return l
}

// realSessions drops placeholder rows so a rescan only sees transcripts on disk.
func realSessions(all []*session) []*session {
	out := make([]*session, 0, len(all))
	for _, s := range all {
		if s.Path != "" {
			out = append(out, s)
		}
	}
	return out
}

// syncStubs keeps one placeholder row per running session whose transcript does not exist yet.
// Claude writes the file at the first prompt, so a fresh fork would otherwise be invisible.
func (m *model) syncStubs() bool {
	real := map[string]bool{}
	for _, s := range m.all {
		if s.Path != "" {
			real[s.ID] = true
		}
	}
	changed := false
	kept := m.all[:0:0]
	have := map[string]bool{}
	for _, s := range m.all {
		if s.Path == "" {
			li, ok := m.live[s.ID]
			if !ok || real[s.ID] {
				changed = true
				continue
			}
			if t := time.UnixMilli(li.UpdatedAt); !t.Equal(s.MTime) {
				s.MTime, changed = t, true
			}
			have[s.ID] = true
		}
		kept = append(kept, s)
	}
	for id, li := range m.live {
		if real[id] || have[id] || li.Kind != "interactive" {
			continue
		}
		title := li.Name
		if title == "" {
			title = "(new session, no prompts yet)"
		}
		kept = append(kept, &session{ID: id, Cwd: li.Cwd, Title: title, MTime: time.UnixMilli(li.UpdatedAt), Started: time.UnixMilli(li.UpdatedAt), Fork: isForkTitle(title)})
		changed = true
	}
	m.all = kept
	if changed {
		linkForks(m.all)
	}
	return changed
}

// forkOrigByName finds the newest non-fork session in the same dir whose label the fork name was built from.
func forkOrigByName(f *session, all []*session) *session {
	base := strings.TrimPrefix(strings.TrimPrefix(f.Title, forkMark), legacyMark)
	if i := strings.LastIndex(base, " · "); i >= 0 {
		base = base[:i]
	}
	var best *session
	for _, c := range all {
		if c == f || c.Path == "" || c.Cwd != f.Cwd || isForkTitle(c.Title) || clean(c.label(), 50) != base {
			continue
		}
		if best == nil || c.MTime.After(best.MTime) {
			best = c
		}
	}
	return best
}

// ───────────────────────── tags ─────────────────────────

// tagStore maps session ID to its tags; it lives in the state file next to the pins.
var tagStore = map[string][]string{}

func parseTags(v string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(v), func(r rune) bool { return r == ',' || r == ' ' }) {
		w = strings.TrimLeft(w, "#")
		if w != "" && !seen[w] && len(out) < 5 {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

func tagText(id, sep string) string {
	var b []string
	for _, t := range tagStore[id] {
		b = append(b, "#"+t)
	}
	return strings.Join(b, sep)
}

// ───────────────────────── missing directories ─────────────────────────

type dirStat struct {
	gone bool
	at   time.Time
}

var dirCache = map[string]dirStat{}

// dirGone reports whether a session's directory no longer exists; claude resumes by directory, so it cannot be resumed.
func dirGone(s *session) bool {
	if s.Cwd == "" {
		return false
	}
	if d, ok := dirCache[s.Cwd]; ok && time.Since(d.at) < 10*time.Second {
		return d.gone
	}
	st, err := os.Stat(s.Cwd)
	gone := err != nil || !st.IsDir()
	dirCache[s.Cwd] = dirStat{gone, time.Now()}
	return gone
}

// askOpen asks where to open the sessions, dropping any whose directory is gone.
func (m *model) askOpen(kind string, ss []*session) tea.Cmd {
	var ok []*session
	var gone *session
	for _, s := range ss {
		if dirGone(s) {
			gone = s
		} else {
			ok = append(ok, s)
		}
	}
	if len(ok) == 0 {
		return m.flash("directory is gone, cannot resume: " + short(gone.Cwd))
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

func hasTag(id, tag string) bool {
	for _, t := range tagStore[id] {
		if t == tag {
			return true
		}
	}
	return false
}

func firstTag(id string) string {
	if t := tagStore[id]; len(t) > 0 {
		return t[0]
	}
	return ""
}

type tagCount struct {
	name string
	n    int
}

// tagList returns every tag with its session count, most used first.
func tagList() []tagCount {
	c := map[string]int{}
	for _, ts := range tagStore {
		for _, t := range ts {
			c[t]++
		}
	}
	out := make([]tagCount, 0, len(c))
	for t, n := range c {
		out = append(out, tagCount{t, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].n != out[j].n {
			return out[i].n > out[j].n
		}
		return out[i].name < out[j].name
	})
	return out
}

const (
	staleMaxPrompts = 2
	staleDays       = 7
)

// isStale marks throwaway sessions: a prompt or two, untouched for a week, and nothing keeping them alive.
func (m model) isStale(s *session) bool {
	return s.Path != "" && s.Prompts <= staleMaxPrompts && time.Since(s.MTime) > staleDays*24*time.Hour &&
		!m.pins[s.ID] && !m.pre[s.ID] && !m.isLive(s) && len(tagStore[s.ID]) == 0
}
