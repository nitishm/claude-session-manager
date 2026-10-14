package engine

import "strings"

// sessionEnv are the variables a running Claude Code session sets for its children. A claude started with
// them still set believes it is a child of that session: CLAUDE_CODE_CHILD_SESSION, for one, turns transcript
// saving off. The daemon or a TUI started from inside a session inherits them, so every claude that ccs
// launches for the user starts without them. (ccs's own claude -p calls keep them on purpose: no transcripts.)
var sessionEnv = []string{
	"CLAUDECODE", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SESSION_ID",
	"CLAUDE_CODE_SESSION_ATTENDED", "CLAUDE_CODE_EXECPATH", "CLAUDE_CODE_MESSAGING_SOCKET",
	"CLAUDE_CODE_MESSAGING_TOKEN", "CLAUDE_PID",
}

// Unmarked prefixes a shell command so it runs without the session variables: "env -u A -u B cmd".
func Unmarked(cmd string) string {
	return "env -u " + strings.Join(sessionEnv, " -u ") + " " + cmd
}

// CleanEnv returns env without the session variables, for syscall.Exec.
func CleanEnv(env []string) []string {
	drop := map[string]bool{}
	for _, k := range sessionEnv {
		drop[k] = true
	}
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[k] {
			out = append(out, kv)
		}
	}
	return out
}
