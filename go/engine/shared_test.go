package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Two processes (the daemon and a TUI) share state-go.json: neither may overwrite the other's change.
func TestSharedStateMerges(t *testing.T) {
	home := t.TempDir()
	a, b := New(home), New(home)
	a.TogglePin("one")
	b.RefreshShared()
	if !b.Pinned("one") {
		t.Fatal("b did not pick up a's pin")
	}
	// b saves with a stale view of a later change by a
	time.Sleep(20 * time.Millisecond)
	a.TogglePin("two")
	b.TogglePin("three")
	for _, e := range []*Engine{a, b} {
		e.RefreshShared()
		for _, id := range []string{"one", "two", "three"} {
			if !e.Pinned(id) {
				t.Fatalf("pin %q lost", id)
			}
		}
	}
	a.SetTags("one", []string{"x"})
	if st, _ := b.RefreshShared(); !st || len(b.Tags("one")) != 1 {
		t.Fatal("tags not synced")
	}
}

func TestRefreshSharedSeesBriefs(t *testing.T) {
	home := t.TempDir()
	a, b := New(home), New(home)
	b.RefreshShared() // first call only records the mtime
	a.StoreBrief("k", "text")
	if _, br := b.RefreshShared(); !br {
		t.Fatal("brief write not seen")
	}
	if _, br := b.RefreshShared(); br {
		t.Fatal("reported twice")
	}
	_ = os.Remove(filepath.Join(home, ".cache", "ccs", "briefs.json"))
}
