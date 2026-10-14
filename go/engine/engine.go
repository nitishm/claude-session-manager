// Package engine is the backend of ccs: it finds, parses, searches and acts on Claude Code
// sessions. It has no UI code, so the TUI, a JSON server and the Mac app can all sit on it.
//
// Rule: no process launches in hot paths. Endpoint security agents (Jamf Protect, Defender)
// scan every exec, so the 5s refresh and the scans use only file reads and syscalls.
package engine

import (
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Engine holds everything that is not a session: where files live, pins, tags and small caches.
// All methods are safe to call from several goroutines.
type Engine struct {
	Home     string // the user's home; Claude's data lives in Home/.claude
	CacheDir string // ccs state: Home/.cache/ccs

	mu     sync.Mutex
	tags   map[string][]string // session id -> tags
	pins   map[string]bool
	sortBy string
	tagGen int // bumps when tags change, so cached search text is rebuilt

	stateMod   time.Time // mtime of state-go.json when we last read or wrote it
	briefsMod  time.Time // mtime of briefs.json at the last RefreshShared
	briefsSeen bool
	emptyAt    map[string]time.Time
	repos      map[string]repoEntry
	dirs       map[string]dirStat

	factsMu   sync.Mutex
	facts     map[string]*Facts // transcript facts by session id and mtime
	timelines map[string]*Timeline

	repoStates map[string]repoStateEntry // slow jj/git/gh results, guarded by mu
	prCache    map[string]prEntry        // one gh result per PR url, guarded by mu

	sem chan struct{} // limits parallel claude -p calls
}

// New returns an engine rooted at home and loads its saved state.
func New(home string) *Engine {
	e := &Engine{
		Home:     home,
		CacheDir: filepath.Join(home, ".cache", "ccs"),
		tags:     map[string][]string{},
		pins:     map[string]bool{},
		sortBy:   "recent",
		emptyAt:  map[string]time.Time{},
		repos:    map[string]repoEntry{},
		dirs:     map[string]dirStat{},
		sem:      make(chan struct{}, 4),
	}
	e.loadState()
	return e
}

func (e *Engine) file(name string) string { return filepath.Join(e.CacheDir, name) }

func (e *Engine) projectsGlob() string {
	return filepath.Join(e.Home, ".claude", "projects", "*", "*.jsonl")
}

func (e *Engine) sessionsGlob() string {
	return filepath.Join(e.Home, ".claude", "sessions", "*.json")
}

// Short replaces the home directory with ~.
func (e *Engine) Short(p string) string {
	if p == "" {
		return "?"
	}
	return strings.Replace(p, e.Home, "~", 1)
}

// ShortDir keeps the last two path elements.
func (e *Engine) ShortDir(p string) string {
	parts := strings.Split(strings.TrimRight(e.Short(p), "/"), "/")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return strings.Join(parts, "/")
}
