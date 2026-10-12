package engine

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// PRState is what gh reports for the PR link on a session.
type PRState struct {
	State   string `json:"state"` // OPEN, MERGED, CLOSED
	Draft   bool   `json:"draft"`
	Review  string `json:"review"` // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, or ""
	Checks  string `json:"checks"` // passing, failing, pending, or "" when there are none
	Passed  int    `json:"passed"`
	Failed  int    `json:"failed"`
	Pending int    `json:"pending"`
	Title   string `json:"title"`
}

// RepoState is the live state of the session's working copy. It runs jj, git and gh, so it is slow.
type RepoState struct {
	Kind   string   `json:"kind"` // jj, git, or "" when the folder is not a repo
	Branch string   `json:"branch"`
	Dirty  int      `json:"dirty"`
	Ahead  int      `json:"ahead"`
	Behind int      `json:"behind"`
	PR     *PRState `json:"pr,omitempty"`
	Err    string   `json:"err,omitempty"`
}

type repoStateEntry struct {
	st RepoState
	at time.Time
}

const repoStateTTL = 45 * time.Second

// tool finds a CLI even when the daemon was launched from the app with a bare PATH.
func tool(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, d := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"} {
		p := filepath.Join(d, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func run(ctx context.Context, dir, bin string, args ...string) (string, error) {
	if bin == "" {
		return "", exec.ErrNotFound
	}
	c := exec.CommandContext(ctx, bin, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "NO_COLOR=1")
	out, err := c.Output()
	return string(out), err
}

// RepoState returns the working copy and PR state for s, cached for a short while.
func (e *Engine) RepoState(s *Session) RepoState {
	key := s.ID + "|" + s.Cwd + "|" + s.PR
	e.mu.Lock()
	if c, ok := e.repoStates[key]; ok && time.Since(c.at) < repoStateTTL {
		e.mu.Unlock()
		return c.st
	}
	e.mu.Unlock()
	st := readRepoState(s.Cwd, s.PR)
	e.mu.Lock()
	if e.repoStates == nil {
		e.repoStates = map[string]repoStateEntry{}
	}
	e.repoStates[key] = repoStateEntry{st, time.Now()}
	e.mu.Unlock()
	return st
}

func readRepoState(cwd, pr string) RepoState {
	var st RepoState
	if cwd == "" {
		return st
	}
	if fi, err := os.Stat(cwd); err != nil || !fi.IsDir() {
		st.Err = "directory is gone"
		return st
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	root := findRepo(cwd)
	switch {
	case root.Root == "":
	case isJJ(root.Root):
		st.Kind = "jj"
		readJJ(ctx, &st, root.Root)
	default:
		st.Kind = "git"
		readGit(ctx, &st, root.Root)
	}
	if strings.HasPrefix(pr, "https://") {
		if p, err := readPR(ctx, cwd, pr); err == nil {
			st.PR = p
		} else if st.Err == "" {
			st.Err = "PR: " + err.Error()
		}
	}
	return st
}

func isJJ(root string) bool {
	fi, err := os.Stat(filepath.Join(root, ".jj"))
	return err == nil && fi.IsDir()
}

func readJJ(ctx context.Context, st *RepoState, root string) {
	jj := tool("jj")
	// --ignore-working-copy: a status check must not snapshot or touch the repo.
	out, err := run(ctx, root, jj, "--ignore-working-copy", "--no-pager", "log", "--no-graph", "-r", "latest(::@ & bookmarks())", "-T", `bookmarks.map(|b| b.name()).join(",")`)
	if err != nil {
		st.Err = "jj: " + firstLine(err.Error())
		return
	}
	st.Branch = strings.TrimSpace(out)
	if d, err := run(ctx, root, jj, "--ignore-working-copy", "--no-pager", "diff", "-r", "@", "--summary"); err == nil {
		st.Dirty = countLines(d)
	}
}

func readGit(ctx context.Context, st *RepoState, root string) {
	out, err := run(ctx, root, tool("git"), "status", "--porcelain=v1", "-b")
	if err != nil {
		st.Err = "git: " + firstLine(err.Error())
		return
	}
	parseGitStatus(st, out)
}

// parseGitStatus reads "git status --porcelain=v1 -b": a "## branch...origin/branch [ahead 1, behind 2]" line, then one line per change.
func parseGitStatus(st *RepoState, out string) {
	for i, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if i > 0 {
			if l != "" {
				st.Dirty++
			}
			continue
		}
		l = strings.TrimPrefix(l, "## ")
		if j := strings.Index(l, " ["); j >= 0 {
			for _, p := range strings.Split(strings.Trim(l[j+2:], "]"), ", ") {
				switch {
				case strings.HasPrefix(p, "ahead "):
					st.Ahead, _ = strconv.Atoi(strings.TrimPrefix(p, "ahead "))
				case strings.HasPrefix(p, "behind "):
					st.Behind, _ = strconv.Atoi(strings.TrimPrefix(p, "behind "))
				}
			}
			l = l[:j]
		}
		l = strings.TrimPrefix(l, "No commits yet on ")
		if j := strings.Index(l, "..."); j >= 0 {
			l = l[:j]
		}
		st.Branch = l
	}
}

func readPR(ctx context.Context, cwd, url string) (*PRState, error) {
	out, err := run(ctx, cwd, tool("gh"), "pr", "view", url, "--json", "state,isDraft,reviewDecision,statusCheckRollup,title")
	if err != nil {
		return nil, err
	}
	return parsePR([]byte(out))
}

func parsePR(b []byte) (*PRState, error) {
	var raw struct {
		State          string `json:"state"`
		IsDraft        bool   `json:"isDraft"`
		ReviewDecision string `json:"reviewDecision"`
		Title          string `json:"title"`
		Checks         []struct {
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			State      string `json:"state"` // status contexts use state instead
		} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	p := &PRState{State: raw.State, Draft: raw.IsDraft, Review: raw.ReviewDecision, Title: raw.Title}
	for _, c := range raw.Checks {
		switch strings.ToUpper(c.Conclusion + c.State) {
		case "SUCCESS", "NEUTRAL", "SKIPPED":
			p.Passed++
		case "FAILURE", "ERROR", "TIMED_OUT", "CANCELLED", "STARTUP_FAILURE", "ACTION_REQUIRED":
			p.Failed++
		default:
			p.Pending++
		}
	}
	switch {
	case p.Failed > 0:
		p.Checks = "failing"
	case p.Pending > 0:
		p.Checks = "pending"
	case p.Passed > 0:
		p.Checks = "passing"
	}
	return p, nil
}

func countLines(s string) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
