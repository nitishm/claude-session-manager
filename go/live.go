package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Claude Code writes ~/.claude/sessions/<pid>.json for every running session.
// We read those to show what is running now, and snapshot them every 3 minutes so
// that after a reboot we know exactly which sessions were open when it died.

type liveInfo struct {
	PID                   int
	ID, Cwd, Name, Status string
	UpdatedAt             int64
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

var liveStateF = filepath.Join(cacheDir, "live.json")

// readPidFiles returns running claude sessions and the leftovers of dead ones.
func readPidFiles() (alive map[string]liveInfo, dead []liveInfo) {
	alive = map[string]liveInfo{}
	files, _ := filepath.Glob(filepath.Join(home, ".claude", "sessions", "*.json"))
	var all []liveInfo
	var pids []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var p pidFile
		if json.Unmarshal(b, &p) != nil || p.SessionID == "" || p.PID == 0 {
			continue
		}
		all = append(all, liveInfo{p.PID, p.SessionID, p.Cwd, p.Name, p.Status, p.UpdatedAt, p.Kind})
		pids = append(pids, strconv.Itoa(p.PID))
	}
	if len(all) == 0 {
		return
	}
	// one ps call; a pid only counts if it is still a claude process (pids get reused after reboot)
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

// readPidFilesFast is the exec-free variant for the 5s refresh tick: every fork/exec
// is scanned by endpoint security agents (Jamf Protect, Defender), so no ps here.
// A pid counts if signal 0 succeeds and its pid file was written after boot.
func readPidFilesFast(boot time.Time) map[string]liveInfo {
	alive := map[string]liveInfo{}
	files, _ := filepath.Glob(filepath.Join(home, ".claude", "sessions", "*.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var p pidFile
		if json.Unmarshal(b, &p) != nil || p.SessionID == "" || p.PID == 0 {
			continue
		}
		if !boot.IsZero() && p.UpdatedAt < boot.UnixMilli() {
			continue
		}
		if err := syscall.Kill(p.PID, 0); err != nil && err != syscall.EPERM {
			continue
		}
		alive[p.SessionID] = liveInfo{p.PID, p.SessionID, p.Cwd, p.Name, p.Status, p.UpdatedAt, p.Kind}
	}
	return alive
}

type liveState struct {
	Boot        int64             `json:"boot"`
	Live        map[string]string `json:"live"`        // session id -> cwd, as of the last snapshot
	Interrupted map[string]int64  `json:"interrupted"` // session id -> boot (unix) of the reboot that killed it
}

func loadLiveState() liveState {
	st := liveState{Live: map[string]string{}, Interrupted: map[string]int64{}}
	if b, err := os.ReadFile(liveStateF); err == nil {
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

// snapshot records what is running. Returns the state and whether history existed before this run.
func snapshot() (liveState, bool) {
	st := loadLiveState()
	hadHistory := st.Boot != 0
	alive, dead := readPidFiles()
	curBoot := bootTime().Unix()
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
	os.MkdirAll(cacheDir, 0o755)
	b, _ := json.Marshal(st)
	os.WriteFile(liveStateF, b, 0o644)
	return st, hadHistory
}

const agentLabel = "com.user.ccs-snapshot"

func agentPlist() string { return filepath.Join(home, "Library", "LaunchAgents", agentLabel+".plist") }

func installAgent() error {
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
	os.MkdirAll(filepath.Dir(agentPlist()), 0o755)
	if err := os.WriteFile(agentPlist(), []byte(plist), 0o644); err != nil {
		return err
	}
	dom := fmt.Sprintf("gui/%d", os.Getuid())
	exec.Command("launchctl", "bootout", dom+"/"+agentLabel).Run() // ignore: may not be loaded
	if out, err := exec.Command("launchctl", "bootstrap", dom, agentPlist()).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func uninstallAgent() error {
	exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), agentLabel)).Run()
	return os.Remove(agentPlist())
}
