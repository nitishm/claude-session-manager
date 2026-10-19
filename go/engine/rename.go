package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// MaxTitle is the longest title Rename accepts, in characters.
const MaxTitle = 120

// customTitleLine is the transcript line Claude Code writes for /rename; the last one in a file wins.
type customTitleLine struct {
	Type        string `json:"type"`
	CustomTitle string `json:"customTitle"`
	SessionID   string `json:"sessionId"`
}

// CleanTitle collapses whitespace so a title is one line.
func CleanTitle(t string) string { return strings.Join(strings.Fields(t), " ") }

// SetTitle changes the title shown for the session.
func (s *Session) SetTitle(t string) {
	s.Title = t
	s.hayC = "" // the search text includes the title
}

// Rename gives a session a new title by appending a custom-title line to its transcript, which is
// exactly what /rename does, so claude --resume shows the new name too. It keeps the file's
// modification time: a rename is not activity, and briefs are cached against that time.
// A running session is refused, because claude rewrites its own title when it exits.
func (e *Engine) Rename(s *Session, live map[string]LiveInfo, title string) error {
	title = CleanTitle(title)
	switch {
	case title == "":
		return fmt.Errorf("the title is empty")
	case len([]rune(title)) > MaxTitle:
		return fmt.Errorf("the title is longer than %d characters", MaxTitle)
	case s.Path == "":
		return fmt.Errorf("this session has no transcript yet")
	}
	if _, ok := live[s.ID]; ok {
		return fmt.Errorf("it is running: use /rename inside the session")
	}
	fi, err := os.Stat(s.Path)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.Path, os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	line, _ := json.Marshal(customTitleLine{"custom-title", title, s.ID})
	prefix := ""
	if fi.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, fi.Size()-1); err == nil && last[0] != '\n' {
			prefix = "\n"
		}
	}
	if _, err := f.WriteString(prefix + string(line) + "\n"); err != nil {
		return err
	}
	f.Close()
	os.Chtimes(s.Path, fi.ModTime(), fi.ModTime())
	s.SetTitle(title)
	return nil
}
