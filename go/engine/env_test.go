package engine

import (
	"strings"
	"testing"
)

func TestCleanEnv(t *testing.T) {
	in := []string{"PATH=/bin", "CLAUDE_CODE_CHILD_SESSION=1", "CLAUDECODE=1", "CLAUDE_CODE_USE_BEDROCK=1", "HOME=/h"}
	got := strings.Join(CleanEnv(in), " ")
	if strings.Contains(got, "CHILD_SESSION") || strings.Contains(got, "CLAUDECODE=") {
		t.Fatal(got)
	}
	if !strings.Contains(got, "CLAUDE_CODE_USE_BEDROCK=1") || !strings.Contains(got, "PATH=/bin") {
		t.Fatalf("user config dropped: %s", got)
	}
}

func TestUnmarked(t *testing.T) {
	c := Unmarked("'claude' --resume x")
	if !strings.HasPrefix(c, "env -u CLAUDECODE ") || !strings.Contains(c, "-u CLAUDE_CODE_CHILD_SESSION ") || !strings.HasSuffix(c, " 'claude' --resume x") {
		t.Fatal(c)
	}
}
