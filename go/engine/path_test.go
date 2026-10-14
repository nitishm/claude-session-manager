package engine

import (
	"os"
	"strings"
	"testing"
)

func TestWidenPath(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	WidenPath("/home/me")
	p := os.Getenv("PATH")
	if !strings.Contains(p, "/home/me/.local/bin") || !strings.HasSuffix(p, "/usr/bin:/bin") {
		t.Fatal(p)
	}
	WidenPath("/home/me") // second call adds nothing
	if strings.Count(os.Getenv("PATH"), "/home/me/.local/bin") != 1 {
		t.Fatal(os.Getenv("PATH"))
	}
}
