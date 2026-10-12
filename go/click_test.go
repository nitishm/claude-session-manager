package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTokenAt(t *testing.T) {
	line := "\x1b[34mFILES\x1b[0m  go/engine/brief.go, https://github.com/x/y/pull/9)  ~/a.txt"
	cases := map[int]string{
		0:  "FILES",
		8:  "go/engine/brief.go",
		30: "https://github.com/x/y/pull/9",
		64: "~/a.txt",
		6:  "", // the gap between words
	}
	for col, want := range cases {
		if got := tokenAt(line, col); got != want {
			t.Errorf("col %d = %q, want %q", col, got, want)
		}
	}
}

func TestResolvePath(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), nil, 0o644)
	if got := resolvePath("a.go", dir); got != filepath.Join(dir, "a.go") {
		t.Errorf("relative = %q", got)
	}
	if got := resolvePath("missing.go", dir); got != "" {
		t.Errorf("missing = %q", got)
	}
	if got := resolvePath(filepath.Join(dir, "a.go"), "/nowhere"); got == "" {
		t.Error("absolute path should resolve")
	}
}
