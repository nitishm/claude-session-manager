package main

import (
	"strings"
	"testing"

	"ccsgo/engine"
)

func TestRepoLine(t *testing.T) {
	if repoLine(repoView{}) != "" {
		t.Fatal("empty before the first fetch")
	}
	l := repoLine(repoView{ok: true, st: engine.RepoState{Kind: "jj", Branch: "main", Dirty: 3, PR: &engine.PRState{State: "OPEN", Checks: "failing", Failed: 2, Review: "APPROVED"}}})
	for _, w := range []string{"jj main", "3 changed", "PR open", "2 checks failing", "approved"} {
		if !strings.Contains(l, w) {
			t.Fatalf("missing %q in %q", w, l)
		}
	}
}
