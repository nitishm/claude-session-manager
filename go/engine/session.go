package engine

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Session is one Claude Code transcript.
type Session struct {
	ID, Path, Cwd, Title, AI, First, Last, PR, AText, Summary string
	Prompts                                                   int
	MTime, Started                                            time.Time
	PList                                                     []string
	Turns                                                     []Turn
	UH                                                        []uint64 // hashed message uuids; a fork copies its original's
	Fork                                                      bool     // created by a fork (named with a leading [fork])
	ForkOf                                                    *Session // best guess at the original
	Forks                                                     int      // forks made from this session

	hayC   string // lowercased search text, rebuilt when tags or summary change
	hayGen int
}

// Turn is one message of the conversation preview.
type Turn struct {
	User bool
	Text string
}

func (s *Session) addTurn(user bool, text string) {
	s.Turns = append(s.Turns, Turn{user, text})
	if len(s.Turns) > 4 {
		s.Turns = s.Turns[1:]
	}
}

// Label is the best display name: the user's title, else the AI title, else the first prompt.
func (s *Session) Label() string {
	for _, v := range []string{s.Title, s.AI, s.First} {
		if v != "" {
			return v
		}
	}
	return "(untitled)"
}

var pasteRe = regexp.MustCompile(`<pasted_content[^>]*>.*`)

// Excerpt is the LLM summary when cached, else first ask ⟶ last ask.
func (s *Session) Excerpt() string {
	if s.Summary != "" {
		return s.Summary
	}
	var good []string
	for _, p := range s.PList {
		if len(strings.Fields(p)) >= 5 {
			good = append(good, pasteRe.ReplaceAllString(p, "[paste]"))
		}
	}
	switch {
	case len(good) == 0:
		return s.Last
	case len(good) == 1 || good[0] == good[len(good)-1]:
		return good[0]
	}
	return Clean(good[0], 55) + "  ⟶  " + Clean(good[len(good)-1], 55)
}

var (
	wsRe = regexp.MustCompile(`\s+`)
	mdRe = strings.NewReplacer("```", "", "**", "", "###", "", "##", "")
)

// Clean strips markdown noise, collapses whitespace and cuts to n runes.
func Clean(s string, n int) string {
	s = mdRe.Replace(s)
	s = strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

// Ago is a short age: now, 5m, 3h, 12d.
func Ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return "now"
}

// AgoLong is Ago for sentences: "5m ago", "just now".
func AgoLong(t time.Time) string {
	if a := Ago(t); a != "now" {
		return a + " ago"
	}
	return "just now"
}
