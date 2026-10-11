package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Claude Code writes ~/.claude/sessions/<pid>.json for every running session.
// We read those to show what is running now, and snapshot them every 3 minutes so
// that after a reboot we know exactly which sessions were open when it died.

// LiveInfo describes one running claude process.
type LiveInfo struct {
	PID                   int
	ID, Cwd, Name, Status string
	UpdatedAt             int64 // unix millis
	Kind                  string
}

type pidFile struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	UpdatedAt int64  `json:"updatedAt"`
	Kind      string `json:"kind"`
}

func (e *Engine) readPidFileList() (all []LiveInfo) {
	files, _ := filepath.Glob(e.sessionsGlob())
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var p pidFile
		if json.Unmarshal(b, &p) != nil || p.SessionID == "" || p.PID == 0 {
			continue
		}
		all = append(all, LiveInfo{p.PID, p.SessionID, p.Cwd, p.Name, p.Status, p.UpdatedAt, p.Kind})
	}
	return
}

// ReadPids returns running claude sessions and the leftovers of dead ones. It runs ps once, so
// use it at startup and in the snapshot agent only, never on a timer.
func (e *Engine) ReadPids() (alive map[string]LiveInfo, dead []LiveInfo) {
	alive = map[string]LiveInfo{}
	all := e.readPidFileList()
	if len(all) == 0 {
		return
	}
	var pids []string
	for _, li := range all {
		pids = append(pids, strconv.Itoa(li.PID))
	}
	// a pid only counts if it is still a claude process (pids get reused after reboot)
	out, _ := exec.Command("ps", "-p", strings.Join(pids, ","), "-o", "pid=,command=").Output()
	running := map[int]bool{}
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && strings.Contains(l, "claude") {
			n, _ := strconv.Atoi(f[0])
			running[n] = true
		}
	}
	for _, li := range all {
		if running[li.PID] {
			alive[li.ID] = li
		} else {
			dead = append(dead, li)
		}
	}
	return
}

// LivePids is the exec-free variant for the 5s refresh tick: every fork/exec is scanned by
// endpoint security agents, so no ps here. A pid counts if signal 0 succeeds and its pid file
// was written after boot.
func (e *Engine) LivePids(boot time.Time) map[string]LiveInfo {
	alive := map[string]LiveInfo{}
	for _, li := range e.readPidFileList() {
		if !boot.IsZero() && li.UpdatedAt < boot.UnixMilli() {
			continue
		}
		if err := syscall.Kill(li.PID, 0); err != nil && err != syscall.EPERM {
			continue
		}
		alive[li.ID] = li
	}
	return alive
}

var bootRe = regexp.MustCompile(`sec = (\d+)`)

// BootTime is when the machine last booted (zero if unknown). One sysctl call.
func BootTime() time.Time {
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

// LiveState is what the snapshot agent persists.
type LiveState struct {
	Boot        int64             `json:"boot"`
	Live        map[string]string `json:"live"`        // session id -> cwd, as of the last snapshot
	Interrupted map[string]int64  `json:"interrupted"` // session id -> boot (unix) of the reboot that killed it
}

func (e *Engine) loadLiveState() LiveState {
	st := LiveState{Live: map[string]string{}, Interrupted: map[string]int64{}}
	if b, err := os.ReadFile(e.file("live.json")); err == nil {
		json.Unmarshal(b, &st)
	}
	if st.Live == nil {
		st.Live = map[string]string{}
	}
	if st.Interrupted == nil {
		st.Interrupted = map[string]int64{}
	}
	return st
}

// Snapshot records what is running. Returns the state and whether history existed before this run.
func (e *Engine) Snapshot() (LiveState, bool) {
	st := e.loadLiveState()
	hadHistory := st.Boot != 0
	alive, dead := e.ReadPids()
	curBoot := BootTime().Unix()
	if st.Boot != 0 && st.Boot != curBoot { // machine rebooted since the last snapshot
		for id := range st.Live {
			if _, ok := alive[id]; !ok {
				st.Interrupted[id] = curBoot
			}
		}
	}
	for _, d := range dead { // pid files left behind by a crash/reboot
		if d.UpdatedAt/1000 < curBoot {
			if _, ok := alive[d.ID]; !ok {
				st.Interrupted[d.ID] = curBoot
				hadHistory = true
			}
		}
	}
	for id := range alive {
		delete(st.Interrupted, id)
	}
	for id, b := range st.Interrupted {
		if time.Since(time.Unix(b, 0)) > 14*24*time.Hour {
			delete(st.Interrupted, id)
		}
	}
	st.Live = map[string]string{}
	for id, li := range alive {
		st.Live[id] = li.Cwd
	}
	st.Boot = curBoot
	os.MkdirAll(e.CacheDir, 0o755)
	b, _ := json.Marshal(st)
	os.WriteFile(e.file("live.json"), b, 0o644)
	return st, hadHistory
}

// PreBoot finds the sessions that were in use when the machine last went down: the ones the
// snapshot agent saw running, else everything active within 8h of the last activity before boot.
func (e *Engine) PreBoot(all []*Session, boot time.Time) map[string]bool {
	pre := map[string]bool{}
	st, hadHistory := e.Snapshot()
	alive, _ := e.ReadPids()
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
	// no snapshot history yet: guess from file times
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

const agentLabel = "com.user.ccs-snapshot"

// AgentPlist is where the launchd agent definition lives.
func (e *Engine) AgentPlist() string {
	return filepath.Join(e.Home, "Library", "LaunchAgents", agentLabel+".plist")
}

// InstallAgent installs a launchd agent that runs `<this binary> --snapshot` every 3 minutes.
func (e *Engine) InstallAgent() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string><string>--snapshot</string></array>
<key>StartInterval</key><integer>180</integer>
<key>RunAtLoad</key><true/>
<key>StandardErrorPath</key><string>/tmp/ccs-snapshot.log</string>
</dict></plist>
`, agentLabel, exe)
	os.MkdirAll(filepath.Dir(e.AgentPlist()), 0o755)
	if err := os.WriteFile(e.AgentPlist(), []byte(plist), 0o644); err != nil {
		return err
	}
	dom := fmt.Sprintf("gui/%d", os.Getuid())
	exec.Command("launchctl", "bootout", dom+"/"+agentLabel).Run() // ignore: may not be loaded
	if out, err := exec.Command("launchctl", "bootstrap", dom, e.AgentPlist()).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// UninstallAgent removes the launchd agent.
func (e *Engine) UninstallAgent() error {
	exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), agentLabel)).Run()
	return os.Remove(e.AgentPlist())
}
