package engine

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RepoInfo says what kind of repo a directory is in. It is read from files, never by running git.
type RepoInfo struct {
	Kind, Branch, Root string
}

type repoEntry struct {
	info RepoInfo
	at   time.Time
}

type dirStat struct {
	gone bool
	at   time.Time
}

// RepoOf reads .git/HEAD directly. jj repos report no branch (bookmarks need the jj binary).
func (e *Engine) RepoOf(dir string) RepoInfo {
	e.mu.Lock()
	c, ok := e.repos[dir]
	e.mu.Unlock()
	if ok && time.Since(c.at) < 15*time.Second {
		return c.info
	}
	r := findRepo(dir)
	e.mu.Lock()
	e.repos[dir] = repoEntry{r, time.Now()}
	e.mu.Unlock()
	return r
}

func findRepo(dir string) RepoInfo {
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
			r := RepoInfo{Kind: "git", Root: d}
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
			return RepoInfo{Kind: "jj", Root: d}
		}
	}
	return RepoInfo{}
}

func (r RepoInfo) String() string {
	switch {
	case r.Kind == "":
		return ""
	case r.Branch != "":
		return r.Kind + " · " + r.Branch
	}
	return r.Kind
}

// DirGone reports whether a session's directory no longer exists; claude resumes by directory,
// so such a session cannot be resumed.
func (e *Engine) DirGone(s *Session) bool {
	if s.Cwd == "" {
		return false
	}
	e.mu.Lock()
	d, ok := e.dirs[s.Cwd]
	e.mu.Unlock()
	if ok && time.Since(d.at) < 10*time.Second {
		return d.gone
	}
	st, err := os.Stat(s.Cwd)
	gone := err != nil || !st.IsDir()
	e.mu.Lock()
	e.dirs[s.Cwd] = dirStat{gone, time.Now()}
	e.mu.Unlock()
	return gone
}
