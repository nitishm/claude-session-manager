package engine

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"
)

// Sorts are the sort names Query understands.
var Sorts = []string{"recent", "dir", "prompts", "oldest", "tags"}

type stateFile struct {
	Pins []string            `json:"pins"`
	Sort string              `json:"sort"`
	Tags map[string][]string `json:"tags,omitempty"`
}

func (e *Engine) loadState() {
	var st stateFile
	b, err := os.ReadFile(e.file("state-go.json"))
	if err != nil || json.Unmarshal(b, &st) != nil {
		return
	}
	for _, p := range st.Pins {
		e.pins[p] = true
	}
	if st.Sort != "" {
		e.sortBy = st.Sort
	}
	if st.Tags != nil {
		e.tags = st.Tags
	}
}

// save writes pins, sort and tags. The caller must hold e.mu.
func (e *Engine) save() {
	st := stateFile{Sort: e.sortBy, Tags: e.tags}
	for p := range e.pins {
		st.Pins = append(st.Pins, p)
	}
	sort.Strings(st.Pins)
	os.MkdirAll(e.CacheDir, 0o755)
	b, _ := json.Marshal(st)
	os.WriteFile(e.file("state-go.json"), b, 0o644)
}

// ── sort ──

// Sort is the saved sort name.
func (e *Engine) Sort() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sortBy
}

// SetSort saves the sort name.
func (e *Engine) SetSort(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sortBy = name
	e.save()
}

// ── pins ──

// Pinned reports whether a session is pinned.
func (e *Engine) Pinned(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.pins[id]
}

// PinCount is how many sessions are pinned.
func (e *Engine) PinCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.pins)
}

// TogglePin flips a pin and returns the new state.
func (e *Engine) TogglePin(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pins[id] {
		delete(e.pins, id)
	} else {
		e.pins[id] = true
	}
	e.save()
	return e.pins[id]
}

// ── tags ──

// ParseTags turns "a, #b c" into at most 5 distinct lowercase tags.
func ParseTags(v string) []string {
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

// Tags returns a copy of a session's tags.
func (e *Engine) Tags(id string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.tags[id]...)
}

// SetTags replaces a session's tags; an empty list clears them.
func (e *Engine) SetTags(id string, tags []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(tags) == 0 {
		delete(e.tags, id)
	} else {
		e.tags[id] = tags
	}
	e.tagGen++
	e.save()
}

// AddTags merges tags into a session's existing ones (cap 5) and returns the result.
func (e *Engine) AddTags(id string, add []string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	have := e.tags[id]
	seen := map[string]bool{}
	for _, t := range have {
		seen[t] = true
	}
	for _, t := range add {
		if !seen[t] && len(have) < 5 {
			seen[t] = true
			have = append(have, t)
		}
	}
	e.tags[id] = have
	e.tagGen++
	e.save()
	return append([]string(nil), have...)
}

// TagText renders a session's tags as "#a #b" joined by sep.
func (e *Engine) TagText(id, sep string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.tagTextLocked(id, sep)
}

func (e *Engine) tagTextLocked(id, sep string) string {
	var b []string
	for _, t := range e.tags[id] {
		b = append(b, "#"+t)
	}
	return strings.Join(b, sep)
}

func (e *Engine) hasTagLocked(id, tag string) bool {
	for _, t := range e.tags[id] {
		if t == tag {
			return true
		}
	}
	return false
}

// HasTag reports whether a session carries a tag.
func (e *Engine) HasTag(id, tag string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hasTagLocked(id, tag)
}

func (e *Engine) firstTagLocked(id string) string {
	if t := e.tags[id]; len(t) > 0 {
		return t[0]
	}
	return ""
}

// TagCount is a tag with the number of sessions that carry it.
type TagCount struct {
	Name string `json:"name"`
	N    int    `json:"n"`
}

// TagList returns every tag with its session count, most used first.
func (e *Engine) TagList() []TagCount {
	e.mu.Lock()
	defer e.mu.Unlock()
	c := map[string]int{}
	for _, ts := range e.tags {
		for _, t := range ts {
			c[t]++
		}
	}
	out := make([]TagCount, 0, len(c))
	for t, n := range c {
		out = append(out, TagCount{t, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].N != out[j].N {
			return out[i].N > out[j].N
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ── stale ──

const (
	StaleMaxPrompts = 2
	StaleDays       = 7
)

// IsStale marks throwaway sessions: a prompt or two, untouched for a week, and nothing keeping
// them alive (pinned, tagged, running, or open before the last reboot).
func (e *Engine) IsStale(s *Session, live map[string]LiveInfo, pre map[string]bool) bool {
	if s.Path == "" || s.Prompts > StaleMaxPrompts || time.Since(s.MTime) <= StaleDays*24*time.Hour {
		return false
	}
	if _, ok := live[s.ID]; ok || pre[s.ID] {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return !e.pins[s.ID] && len(e.tags[s.ID]) == 0
}
