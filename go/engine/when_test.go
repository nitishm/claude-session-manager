package engine

import (
	"testing"
	"time"
)

func TestWhenPrefersLastMessage(t *testing.T) {
	m := time.Date(2026, 10, 21, 15, 46, 50, 0, time.UTC)
	a := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	if got := (&Session{MTime: m}).When(); !got.Equal(m) {
		t.Fatalf("no activity: want mtime, got %v", got)
	}
	if got := (&Session{MTime: m, Active: a}).When(); !got.Equal(a) {
		t.Fatalf("want last message, got %v", got)
	}
}
