package engine

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

// BriefData is the structured form of a brief. Clients draw it: header card, kanban, timeline.
// The JSON keys are part of the ccsd contract.
type BriefData struct {
	Goal       string       `json:"goal"`
	Status     string       `json:"status"` // active | blocked | done | idle
	StatusNote string       `json:"statusNote"`
	Next       string       `json:"next"`
	Steps      []BriefStep  `json:"steps"`
	Timeline   []BriefEvent `json:"timeline"`
	Files      []string     `json:"files"`
}

// Exchange is one real prompt and the assistant's closing reply, read from the transcript to
// build the digest the model sees.
type Exchange struct {
	N     int
	When  string
	User  string
	Reply string
}

// BriefKinds are the kinds of work a step or event can be, drawn as an icon by the clients.
var BriefKinds = map[string]bool{
	"research": true, "diagnose": true, "build": true, "fix": true, "test": true,
	"decide": true, "blocked": true, "ship": true, "setup": true,
}

// BriefStep is one unit of work. State is done | doing | todo. Outcome is the result in a few words.
type BriefStep struct {
	Text    string `json:"text"`
	State   string `json:"state"`
	Kind    string `json:"kind"`
	Outcome string `json:"outcome"`
}

// BriefEvent is one point on the timeline. When is a short date such as "Oct 02".
type BriefEvent struct {
	When    string `json:"when"`
	Text    string `json:"text"`
	Kind    string `json:"kind"`
	Outcome string `json:"outcome"`
}

// looksLikePath keeps real file paths and drops the descriptions a model sometimes puts in a files list.
func looksLikePath(f string) bool {
	if f == "" || strings.ContainsAny(f, " \t|;&()$") {
		return false
	}
	r, _ := utf8.DecodeRuneInString(f)
	return strings.ContainsAny(f, "/.") || strings.HasPrefix(f, "~") || unicode.IsUpper(r) // Makefile, Dockerfile
}

// Progress counts finished steps.
func (b *BriefData) Progress() (done, total int) {
	for _, s := range b.Steps {
		if s.State == "done" {
			done++
		}
	}
	return done, len(b.Steps)
}

// ParseBrief reads a brief from the model's reply. ok is false for old plain-text briefs and for
// replies that are not usable, so callers can fall back to showing the text.
func ParseBrief(txt string) (*BriefData, bool) {
	i, j := strings.Index(txt, "{"), strings.LastIndex(txt, "}")
	if i < 0 || j < i {
		return nil, false
	}
	var b BriefData
	if json.Unmarshal([]byte(txt[i:j+1]), &b) != nil {
		return nil, false
	}
	b.Goal = Clean(b.Goal, 140)
	b.StatusNote = Clean(b.StatusNote, 120)
	b.Next = Clean(b.Next, 140)
	switch b.Status {
	case "active", "blocked", "done", "idle":
	default:
		b.Status = "active"
	}
	var steps []BriefStep
	for _, s := range b.Steps {
		s.Text, s.Outcome = Clean(s.Text, 70), Clean(s.Outcome, 90)
		if !BriefKinds[s.Kind] {
			s.Kind = ""
		}
		if s.Text == "" || len(steps) == 10 {
			continue
		}
		switch s.State {
		case "done", "doing", "todo":
		default:
			s.State = "todo"
		}
		steps = append(steps, s)
	}
	b.Steps = steps
	// A brief cannot be done while steps remain open.
	for _, s := range steps {
		if b.Status == "done" && s.State != "done" {
			b.Status = "active"
		}
	}
	var tl []BriefEvent
	for _, ev := range b.Timeline {
		ev.When, ev.Text, ev.Outcome = Clean(ev.When, 12), Clean(ev.Text, 90), Clean(ev.Outcome, 90)
		if !BriefKinds[ev.Kind] {
			ev.Kind = ""
		}
		if ev.Text != "" && len(tl) < 7 {
			tl = append(tl, ev)
		}
	}
	b.Timeline = tl
	var files []string
	for _, f := range b.Files {
		if f = Clean(f, 80); looksLikePath(f) && len(files) < 6 {
			files = append(files, f)
		}
	}
	b.Files = files
	if b.Goal == "" || (len(b.Steps) == 0 && len(b.Timeline) == 0) {
		return nil, false
	}
	return &b, true
}
