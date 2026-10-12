package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func shellJoin(a ...string) string {
	for i, s := range a {
		a[i] = "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return strings.Join(a, " ")
}

// ResumeCmd is the shell-quoted command that resumes (or forks and renames) a session.
func ResumeCmd(bin string, s *Session, fork bool) string {
	a := []string{bin, "--resume", s.ID}
	if fork {
		a = append(a, "--fork-session", "--name", ForkName(s))
	}
	return shellJoin(a...)
}

// ───────────────────────── open ─────────────────────────

func (e *Engine) workDir(s *Session) string {
	if st, err := os.Stat(s.Cwd); err != nil || !st.IsDir() {
		return e.Home
	}
	return s.Cwd
}

// Open starts sessions in Ghostty, iTerm2 or Terminal.app (by $TERM_PROGRAM). tab=false: one new
// window (Ghostty: a tab per session inside it); tab=true: new tab(s) in the front window, which
// is the one ccs is running in. fork=true resumes each as a named fork.
// terminal names the terminal to open sessions in. TERM_PROGRAM wins (the TUI runs inside one);
// a daemon started from Finder has none, so CCS_TERMINAL or whichever supported app is installed is used.
func terminal() string {
	if t := os.Getenv("CCS_TERMINAL"); t != "" {
		return t
	}
	if t := os.Getenv("TERM_PROGRAM"); t != "" {
		return t
	}
	for _, c := range [][2]string{{"ghostty", "Ghostty"}, {"iTerm.app", "iTerm"}, {"Apple_Terminal", "Utilities/Terminal"}} {
		for _, dir := range []string{"/Applications/", "/System/Applications/"} {
			if _, err := os.Stat(dir + c[1] + ".app"); err == nil {
				return c[0]
			}
		}
	}
	return ""
}

func (e *Engine) Open(ss []*Session, tab, fork bool) (int, error) {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return 0, fmt.Errorf("claude not found")
	}
	term := terminal()
	if term == "ghostty" {
		return e.openGhostty(ss, bin, tab, fork)
	}
	var cmds []string
	for _, s := range ss {
		cmds = append(cmds, fmt.Sprintf("cd %q && %s", e.workDir(s), ResumeCmd(bin, s, fork)))
	}
	n := 0
	for i := range ss {
		var err error
		switch term {
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
func (e *Engine) openGhostty(ss []*Session, bin string, tab, fork bool) (int, error) {
	var b strings.Builder
	b.WriteString("tell application \"Ghostty\"\n  set win to missing value\n")
	if tab {
		b.WriteString("  if (count of windows) > 0 then set win to front window\n")
	}
	for _, s := range ss {
		fmt.Fprintf(&b, "  set cfg to new surface configuration\n  set initial working directory of cfg to %q\n"+
			"  set command of cfg to %q\n  set wait after command of cfg to true\n"+
			"  if win is missing value then\n    set win to new window with configuration cfg\n"+
			"  else\n    new tab in win with configuration cfg\n  end if\n",
			e.workDir(s), ResumeCmd(bin, s, fork))
	}
	b.WriteString("end tell\n")
	if out, err := exec.Command("osascript", "-e", b.String()).CombinedOutput(); err != nil {
		return 0, fmt.Errorf("ghostty: %s", strings.TrimSpace(string(out)))
	}
	return len(ss), nil
}

// ───────────────────────── delete / undo ─────────────────────────

type move struct{ Src, Dst, CompSrc, CompDst string }

// DeleteResult says what Delete did.
type DeleteResult struct {
	Moved   int
	Skipped int             // running sessions, left alone
	Gone    map[string]bool // ids that were moved to the trash
}

// Delete moves transcripts (and their companion dir) to ~/.Trash/claude-sessions and records
// the batch so Undo can restore it. Running sessions are skipped.
func (e *Engine) Delete(ss []*Session, live map[string]LiveInfo) (DeleteResult, error) {
	res := DeleteResult{Gone: map[string]bool{}}
	var moves []move
	var err error
	for _, s := range ss {
		if _, ok := live[s.ID]; ok {
			res.Skipped++
			continue
		}
		proj := filepath.Base(filepath.Dir(s.Path))
		dst := filepath.Join(e.Home, ".Trash", "claude-sessions", proj)
		if err = os.MkdirAll(dst, 0o755); err != nil {
			break
		}
		stamp := time.Now().Format("150405")
		target := filepath.Join(dst, s.ID+".jsonl")
		if _, e := os.Stat(target); e == nil {
			target = filepath.Join(dst, s.ID+"-"+stamp+".jsonl")
		}
		if err = os.Rename(s.Path, target); err != nil {
			break
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
		res.Gone[s.ID] = true
	}
	res.Moved = len(moves)
	e.saveUndo(moves)
	return res, err
}

func (e *Engine) saveUndo(ms []move) {
	if len(ms) == 0 {
		return
	}
	os.MkdirAll(e.CacheDir, 0o755)
	b, _ := json.Marshal(ms)
	os.WriteFile(e.file("undo.json"), b, 0o644)
}

// Undo puts the last deleted batch back. It never overwrites an existing transcript.
// It returns os.ErrNotExist when there is nothing to undo.
func (e *Engine) Undo() (n int, err error) {
	b, err := os.ReadFile(e.file("undo.json"))
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
	os.Remove(e.file("undo.json"))
	return n, err
}
