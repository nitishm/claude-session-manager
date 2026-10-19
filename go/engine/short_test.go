package engine

import (
	"testing"
	"time"
)

func TestIsShortPresets(t *testing.T) {
	e := New(t.TempDir())
	mk := func(prompts int, age time.Duration, pr string) *Session {
		return &Session{ID: "s", Path: "/x", Prompts: prompts, MTime: time.Now().Add(-age), PR: pr}
	}
	day := 24 * time.Hour
	cases := []struct {
		name string
		s    *Session
		max  int
		days int
		want bool
	}{
		{"default catches 2 prompts, 8d", mk(2, 8*day, ""), 2, 7, true},
		{"default skips 5 prompts", mk(5, 8*day, ""), 2, 7, false},
		{"any age catches 5 prompts, 2d", mk(5, 2*day, ""), 5, 0, true},
		{"any age catches today", mk(5, time.Hour, ""), 5, 0, true},
		{"any age catches 10", mk(10, 2*day, ""), 10, 0, true},
		{"any age skips 11", mk(11, 2*day, ""), 10, 0, false},
		{"a day gate skips today", mk(5, time.Hour, ""), 5, 1, false},
		{"a PR keeps it", mk(1, 30*day, "https://x/pull/1"), 10, 0, false},
	}
	for _, c := range cases {
		if got := e.IsShort(c.s, nil, nil, c.max, c.days); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
	// pinned and tagged sessions are kept
	s := mk(1, 30*day, "")
	e.TogglePin(s.ID)
	if e.IsShort(s, nil, nil, 10, 1) {
		t.Error("pinned session counted as short")
	}
	e.TogglePin(s.ID)
	e.SetTags(s.ID, []string{"keep"})
	if e.IsShort(s, nil, nil, 10, 1) {
		t.Error("tagged session counted as short")
	}
	// running sessions are kept
	e.SetTags(s.ID, nil)
	if e.IsShort(s, map[string]LiveInfo{"s": {PID: 1}}, nil, 10, 1) {
		t.Error("running session counted as short")
	}
}
