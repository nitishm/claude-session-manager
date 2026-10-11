package engine

import (
	"sort"
	"strings"
	"time"
)

const (
	forkMark   = "[fork] "
	legacyMark = "⑂ " // names written by the first version of the fork command
)

// ForkMark is the prefix ccs gives a forked session's name.
const ForkMark = forkMark

// LegacyMark is the prefix the first version of the fork command used.
const LegacyMark = legacyMark

// IsForkTitle reports whether a session name was written by ccs's fork command.
func IsForkTitle(t string) bool {
	return strings.HasPrefix(t, forkMark) || strings.HasPrefix(t, legacyMark)
}

// ForkName labels a fork so it can be told apart from the original in /resume and in ccs.
func ForkName(s *Session) string {
	return forkMark + Clean(s.Label(), 50) + " · " + time.Now().Format("15:04")
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

// LinkForks marks sessions that ccs forked (named "[fork] …") and links each to its original.
// Transcripts store no parent id, but a fork carries copies of the original's messages with the
// same uuids, so the original is the other session sharing the most of them (3 or more). That
// survives compaction, where the fork no longer starts with the original's first prompt.
// Non-fork sessions win over other forks, so a sibling fork is never taken as the parent.
func (e *Engine) LinkForks(all []*Session) {
	for _, s := range all {
		s.Fork, s.ForkOf, s.Forks = false, nil, 0
	}
	for _, s := range all {
		named := IsForkTitle(s.Title)
		if !named && (s.Started.IsZero() || len(s.UH) < 3) {
			continue
		}
		mine := make(map[uint64]struct{}, len(s.UH))
		for _, h := range s.UH {
			mine[h] = struct{}{}
		}
		var best *Session
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
			cf := IsForkTitle(c.Title)
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

// forkOrigByName finds the newest non-fork session in the same dir whose label the fork name was built from.
func forkOrigByName(f *Session, all []*Session) *Session {
	base := strings.TrimPrefix(strings.TrimPrefix(f.Title, forkMark), legacyMark)
	if i := strings.LastIndex(base, " · "); i >= 0 {
		base = base[:i]
	}
	var best *Session
	for _, c := range all {
		if c == f || c.Path == "" || c.Cwd != f.Cwd || IsForkTitle(c.Title) || Clean(c.Label(), 50) != base {
			continue
		}
		if best == nil || c.MTime.After(best.MTime) {
			best = c
		}
	}
	return best
}

func forkRoot(s *Session) string {
	if s.Fork && s.ForkOf != nil {
		return s.ForkOf.ID
	}
	return s.ID
}

// sortForkGroups puts each original directly above its forks; the most recently active group comes first.
func sortForkGroups(rows []*Session) {
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

// SyncStubs keeps one placeholder row per running session whose transcript does not exist yet.
// Claude writes the file at the first prompt, so a fresh fork would otherwise be invisible.
// It returns the new list and whether anything changed (forks are re-linked when it did).
func (e *Engine) SyncStubs(all []*Session, live map[string]LiveInfo) ([]*Session, bool) {
	real := map[string]bool{}
	for _, s := range all {
		if s.Path != "" {
			real[s.ID] = true
		}
	}
	changed := false
	kept := all[:0:0]
	have := map[string]bool{}
	for _, s := range all {
		if s.Path == "" {
			li, ok := live[s.ID]
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
	for id, li := range live {
		if real[id] || have[id] || li.Kind != "interactive" {
			continue
		}
		title := li.Name
		if title == "" {
			title = "(new session, no prompts yet)"
		}
		kept = append(kept, &Session{ID: id, Cwd: li.Cwd, Title: title, MTime: time.UnixMilli(li.UpdatedAt), Started: time.UnixMilli(li.UpdatedAt), Fork: IsForkTitle(title)})
		changed = true
	}
	if changed {
		e.LinkForks(kept)
	}
	return kept, changed
}
