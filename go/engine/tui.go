package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The terminal UI registers itself here so the Mac app can find it: a running TUI gets a
// "select this session" request (a file plus SIGUSR1) and its terminal is raised; with none
// running, the app opens a new terminal tab that runs the TUI on that session.

// TUITitle is the terminal title the TUI sets, so its Ghostty tab can be found.
const TUITitle = "ccs sessions"

func (e *Engine) tuiDir() string { return filepath.Join(e.CacheDir, "tui") }

// RegisterTUI records the current process as a running TUI. The returned func removes the record.
func (e *Engine) RegisterTUI() func() {
	os.MkdirAll(e.tuiDir(), 0o755)
	pid := os.Getpid()
	f := filepath.Join(e.tuiDir(), strconv.Itoa(pid))
	os.WriteFile(f, nil, 0o644)
	return func() { os.Remove(f); os.Remove(f + ".goto") }
}

// TakeGoto returns, and clears, the session id another process asked this TUI to select.
func (e *Engine) TakeGoto() string {
	f := filepath.Join(e.tuiDir(), strconv.Itoa(os.Getpid())+".goto")
	b, err := os.ReadFile(f)
	if err != nil {
		return ""
	}
	os.Remove(f)
	return strings.TrimSpace(string(b))
}

// RunningTUIs lists the pids of live TUIs, newest first. Records of dead processes are removed.
func (e *Engine) RunningTUIs() []int {
	ents, _ := os.ReadDir(e.tuiDir())
	type tp struct {
		pid int
		at  time.Time
	}
	var all []tp
	for _, d := range ents {
		pid, err := strconv.Atoi(d.Name())
		if err != nil {
			continue
		}
		out, _ := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
		if !strings.Contains(string(out), "ccs-go") {
			os.Remove(filepath.Join(e.tuiDir(), d.Name()))
			os.Remove(filepath.Join(e.tuiDir(), d.Name()+".goto"))
			continue
		}
		if fi, err := d.Info(); err == nil {
			all = append(all, tp{pid, fi.ModTime()})
		}
	}
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			if all[j].at.After(all[i].at) {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	pids := make([]int, len(all))
	for i, t := range all {
		pids[i] = t.pid
	}
	return pids
}

// TUIResult says what ShowInTUI did: "focused" a running TUI or "opened" a new one.
type TUIResult struct {
	Action string `json:"action"`
	Host   string `json:"host,omitempty"`
	Exact  bool   `json:"exact,omitempty"` // the TUI's own terminal tab was selected
}

// ShowInTUI selects session id in a running TUI and raises its terminal, or opens a new TUI on it.
func (e *Engine) ShowInTUI(id string) (TUIResult, error) {
	for _, pid := range e.RunningTUIs() {
		if err := os.WriteFile(filepath.Join(e.tuiDir(), strconv.Itoa(pid)+".goto"), []byte(id), 0o644); err != nil {
			return TUIResult{}, err
		}
		if err := syscall.Kill(pid, syscall.SIGUSR1); err != nil {
			continue
		}
		res := TUIResult{Action: "focused"}
		res.Host, res.Exact = raiseTUI(pid)
		return res, nil
	}
	return TUIResult{Action: "opened"}, e.openTUI(id)
}

// raiseTUI brings the terminal that holds the TUI process to the front. It reports the host app
// and whether the exact tab was picked (Ghostty only; other hosts are just raised).
func raiseTUI(pid int) (host string, exact bool) {
	// Start at the parent: when the TUI runs from the app bundle its own command line names CCS.app,
	// which would be mistaken for the host.
	var ppid int
	if out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output(); err == nil {
		ppid, _ = strconv.Atoi(strings.TrimSpace(string(out)))
	}
	path, name := hostOf(ppid)
	switch {
	case name == "Ghostty":
		out, err := exec.Command("osascript", "-e", terminalListScript).Output()
		if err == nil {
			if id := matchTerminal(strings.Split(string(out), "\n"), TUITitle); id != "" {
				script := fmt.Sprintf(`tell application "Ghostty"
	focus (first terminal whose id is %q)
	activate
end tell`, id)
				if exec.Command("osascript", "-e", script).Run() == nil {
					return name, true
				}
			}
		}
		exec.Command("open", "-a", path).Run()
		return name, false
	case name != "":
		exec.Command("open", "-a", path).Run()
		return name, false
	}
	return "", false
}

// tuiBinary finds the TUI: next to this binary (inside the app bundle), on PATH, or CCS_TUI_BIN.
func tuiBinary() (string, error) {
	if p := os.Getenv("CCS_TUI_BIN"); p != "" {
		return p, nil
	}
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), "ccs-go"); fileExists(p) {
			return p, nil
		}
	}
	if p, err := exec.LookPath("ccs-go"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("ccs-go not found: put it on PATH or set CCS_TUI_BIN")
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// openTUI starts the TUI on session id in a new terminal tab (Ghostty, else iTerm or Terminal).
func (e *Engine) openTUI(id string) error {
	bin, err := tuiBinary()
	if err != nil {
		return err
	}
	cmd := fmt.Sprintf("%s --select %s", shellQuote(bin), shellQuote(id))
	switch terminal() {
	case "ghostty":
		script := fmt.Sprintf(`tell application "Ghostty"
	set cfg to new surface configuration
	set command of cfg to %q
	if (count of windows) > 0 then
		new tab in front window with configuration cfg
	else
		new window with configuration cfg
	end if
	activate
end tell`, cmd)
		if out, err := exec.Command("osascript", "-e", script).CombinedOutput(); err != nil {
			return fmt.Errorf("ghostty: %s", strings.TrimSpace(string(out)))
		}
	case "iTerm.app":
		return exec.Command("osascript", "-e", fmt.Sprintf(`tell application "iTerm2" to tell (create window with default profile) to tell current session to write text %q`, cmd)).Run()
	case "Apple_Terminal":
		return exec.Command("osascript", "-e", fmt.Sprintf(`tell application "Terminal"
	activate
	do script %q
end tell`, cmd)).Run()
	default:
		c := exec.Command("pbcopy")
		c.Stdin = strings.NewReader(cmd)
		c.Run()
		return fmt.Errorf("unknown terminal: copied the command to the clipboard")
	}
	return nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
