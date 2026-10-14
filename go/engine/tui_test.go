package engine

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestTUIRegistry(t *testing.T) {
	e := &Engine{CacheDir: t.TempDir()}
	unregister := e.RegisterTUI()
	me := filepath.Join(e.tuiDir(), strconv.Itoa(os.Getpid()))
	if _, err := os.Stat(me); err != nil {
		t.Fatal("not registered:", err)
	}
	// the test binary is not ccs-go, so the record counts as stale and is pruned
	if got := e.RunningTUIs(); len(got) != 0 {
		t.Fatalf("running = %v, want none", got)
	}
	if _, err := os.Stat(me); err == nil {
		t.Fatal("stale record was not removed")
	}
	os.MkdirAll(e.tuiDir(), 0o755)
	os.WriteFile(me+".goto", []byte("abc\n"), 0o644)
	if got := e.TakeGoto(); got != "abc" {
		t.Fatalf("goto = %q", got)
	}
	if got := e.TakeGoto(); got != "" {
		t.Fatalf("goto not cleared: %q", got)
	}
	unregister()
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("a'b"); got != `'a'\''b'` {
		t.Fatal(got)
	}
}
