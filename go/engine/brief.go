package engine

import (
	"encoding/json"
	"strings"
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

// BriefStep is one unit of work. State is done | doing | todo.
type BriefStep struct {
	Text  string `json:"text"`
	State string `json:"state"`
}

// BriefEvent is one point on the timeline. When is a short date such as "Oct 02".
type BriefEvent struct {
	When string `json:"when"`
	Text string `json:"text"`
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
		s.Text = Clean(s.Text, 70)
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
	var tl []BriefEvent
	for _, ev := range b.Timeline {
		ev.When, ev.Text = Clean(ev.When, 12), Clean(ev.Text, 90)
		if ev.Text != "" && len(tl) < 7 {
			tl = append(tl, ev)
		}
	}
	b.Timeline = tl
	var files []string
	for _, f := range b.Files {
		if f = Clean(f, 50); f != "" && len(files) < 6 {
			files = append(files, f)
		}
	}
	b.Files = files
	if b.Goal == "" || (len(b.Steps) == 0 && len(b.Timeline) == 0) {
		return nil, false
	}
	return &b, true
}
