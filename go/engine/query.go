package engine

import (
	"sort"
	"strings"
)

// Opts describes one list view. The zero value is "everything, most recent first".
type Opts struct {
	Text     string          // space-separated words; each must match (substring, else fuzzy on title/dir)
	Dir      string          // exact cwd
	Tag      string          // sessions carrying this tag
	Stale    bool            // throwaway sessions only
	StaleMax int             // with Stale: most prompts (both fields 0 = the default preset)
	StaleAge int             // with Stale: older than this many days (0 = any age)
	PreOnly  bool            // only sessions open before the last reboot (needs Pre)
	Pre      map[string]bool //
	LiveOnly bool            // only running sessions (needs Live)
	Live     map[string]LiveInfo
	WaitOnly bool           // only running sessions waiting on the user (needs Live)
	ForkOnly bool           // forks and sessions that have forks, grouped
	AskRank  map[string]int // non-nil: only these ids, ordered by rank (an Ask or DeepSearch result)
	Sort     string         // one of Sorts; "" means recent
	Reverse  bool
}

// hay is the lowercased text searched by Query: title, summary, dir, PR, tags and every prompt.
func (e *Engine) hay(s *Session) string {
	e.mu.Lock()
	gen := e.tagGen
	tags := e.tagTextLocked(s.ID, " ")
	e.mu.Unlock()
	if s.hayC == "" || s.hayGen != gen {
		s.hayC = strings.ToLower(strings.Join(append([]string{s.Label(), s.Summary, e.Short(s.Cwd), s.PR, tags}, s.PList...), " "))
		s.hayGen = gen
	}
	return s.hayC
}

func (e *Engine) fuzzyShort(s *Session, w string) bool {
	return MatchPos([]rune(strings.ToLower(s.Label()+" "+e.ShortDir(s.Cwd))), []rune(w), true) != nil
}

// MatchPos finds word lw in text lt: a plain substring first, else (if fuzzy) a tight
// subsequence where each next char is within 3 positions of the last. Returns matched rune indexes.
func MatchPos(lt, lw []rune, fuzzy bool) []int {
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

// Query filters and sorts all into the rows to show. Pinned sessions come first, except in an
// ask view where rank decides. It never changes all.
func (e *Engine) Query(all []*Session, o Opts) []*Session {
	words := strings.Fields(strings.ToLower(o.Text))
	rows := make([]*Session, 0, len(all))
	for _, s := range all {
		if o.Dir != "" && s.Cwd != o.Dir {
			continue
		}
		if o.Tag != "" && !e.HasTag(s.ID, o.Tag) {
			continue
		}
		if o.Stale {
			maxP, days := o.StaleMax, o.StaleAge // both zero: the defaults; otherwise as given, with 0 days = any age
			if maxP == 0 && days == 0 {
				maxP, days = StaleMaxPrompts, StaleDays
			}
			if !e.IsShort(s, o.Live, o.Pre, maxP, days) {
				continue
			}
		}
		if o.PreOnly && !o.Pre[s.ID] {
			continue
		}
		if _, ok := o.AskRank[s.ID]; o.AskRank != nil && !ok {
			continue
		}
		if o.ForkOnly && !s.Fork && s.Forks == 0 {
			continue
		}
		if _, ok := o.Live[s.ID]; o.LiveOnly && !ok {
			continue
		}
		if o.WaitOnly && !o.Live[s.ID].Waiting() {
			continue
		}
		h, ok := e.hay(s), true
		for _, w := range words {
			if !strings.Contains(h, w) && !e.fuzzyShort(s, w) {
				ok = false
				break
			}
		}
		if ok {
			rows = append(rows, s)
		}
	}
	e.mu.Lock()
	firstTag := map[string]string{}
	pinned := map[string]bool{}
	for _, s := range rows {
		firstTag[s.ID] = e.firstTagLocked(s.ID)
		pinned[s.ID] = e.pins[s.ID]
	}
	e.mu.Unlock()
	less := map[string]func(a, b *Session) bool{
		"tags": func(a, b *Session) bool {
			ta, tb := firstTag[a.ID], firstTag[b.ID]
			if ta != tb {
				if ta == "" || tb == "" {
					return tb == ""
				}
				return ta < tb
			}
			return a.When().After(b.When())
		},
		"recent":  func(a, b *Session) bool { return a.When().After(b.When()) },
		"oldest":  func(a, b *Session) bool { return a.When().Before(b.When()) },
		"prompts": func(a, b *Session) bool { return a.Prompts > b.Prompts },
		"dir": func(a, b *Session) bool {
			if a.Cwd != b.Cwd {
				return e.Short(a.Cwd) < e.Short(b.Cwd)
			}
			return a.When().After(b.When())
		},
	}[o.Sort]
	if less == nil {
		less = func(a, b *Session) bool { return a.When().After(b.When()) }
	}
	if o.Reverse {
		base := less
		less = func(a, b *Session) bool { return base(b, a) }
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if o.AskRank != nil {
			return o.AskRank[rows[i].ID] < o.AskRank[rows[j].ID]
		}
		pi, pj := pinned[rows[i].ID], pinned[rows[j].ID]
		if pi != pj {
			return pi
		}
		return less(rows[i], rows[j])
	})
	if o.ForkOnly && o.AskRank == nil {
		sortForkGroups(rows)
	}
	return rows
}
