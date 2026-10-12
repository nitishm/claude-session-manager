package engine

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// FileTouch is a file Claude edited or read in a session.
type FileTouch struct {
	Path  string `json:"path"` // absolute
	Rel   string `json:"rel"`  // relative to the session's directory when inside it
	Edits int    `json:"edits"`
	Reads int    `json:"reads"`
}

// Facts are numbers and lists read straight from a transcript. No model call is involved.
type Facts struct {
	Model        string         `json:"model"`
	Turns        int            `json:"turns"` // assistant messages
	In           int64          `json:"in"`
	Out          int64          `json:"out"`
	CacheRead    int64          `json:"cacheRead"`
	CacheWrite   int64          `json:"cacheWrite"`
	ActiveMin    int            `json:"activeMin"` // time between messages, gaps over 5 minutes not counted
	Files        []FileTouch    `json:"files"`
	Commands     []string       `json:"commands"` // the last few shell commands
	CommandCount int            `json:"commandCount"`
	Tools        map[string]int `json:"tools"`
}

// CacheHit is the share of input tokens served from the prompt cache, 0 to 100.
func (f *Facts) CacheHit() int {
	if tot := f.In + f.CacheRead + f.CacheWrite; tot > 0 {
		return int(f.CacheRead * 100 / tot)
	}
	return 0
}

type factLine struct {
	Type        string `json:"type"`
	Timestamp   string `json:"timestamp"`
	IsSidechain bool   `json:"isSidechain"`
	Message     struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   struct {
			In         int64 `json:"input_tokens"`
			Out        int64 `json:"output_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type toolUse struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Input struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
		Command      string `json:"command"`
	} `json:"input"`
}

// Facts returns the transcript facts for s, cached until the session changes.
func (e *Engine) Facts(s *Session) *Facts {
	key := BriefKey(s)
	e.factsMu.Lock()
	if f, ok := e.facts[key]; ok {
		e.factsMu.Unlock()
		return f
	}
	e.factsMu.Unlock()
	f := ReadFacts(s.Path, s.Cwd)
	e.factsMu.Lock()
	if e.facts == nil {
		e.facts = map[string]*Facts{}
	}
	for k := range e.facts { // keep one entry per session
		if strings.HasPrefix(k, s.ID+":") {
			delete(e.facts, k)
		}
	}
	e.facts[key] = f
	e.factsMu.Unlock()
	return f
}

// ReadFacts scans a transcript. Claude Code writes one line per content block, so usage is taken
// once per message id.
func ReadFacts(path, cwd string) *Facts {
	f := &Facts{Tools: map[string]int{}}
	fh, err := os.Open(path)
	if err != nil {
		return f
	}
	defer fh.Close()
	type usage struct{ in, out, cr, cw int64 }
	perMsg := map[string]usage{}
	touched := map[string]*FileTouch{}
	var stamps []time.Time
	rd := bufio.NewReaderSize(fh, 64<<10)
	for {
		line, err := rd.ReadBytes('\n')
		if len(line) > 2 && (bytes.Contains(line, []byte(`"type":"assistant"`)) || bytes.Contains(line, []byte(`"type":"user"`))) {
			var d factLine
			if json.Unmarshal(line, &d) == nil {
				if t, err := time.Parse(time.RFC3339Nano, d.Timestamp); err == nil && !d.IsSidechain {
					stamps = append(stamps, t)
				}
				if d.Type == "assistant" {
					if d.Message.Model != "" && d.Message.Model != "<synthetic>" {
						f.Model = d.Message.Model
					}
					if id := d.Message.ID; id != "" {
						u := d.Message.Usage
						perMsg[id] = usage{u.In, u.Out, u.CacheRead, u.CacheWrite} // the last line of a message has the final counts
					}
					if !d.IsSidechain {
						f.scanTools(d.Message.Content, cwd, touched)
					}
				}
			}
		}
		if err != nil {
			break
		}
	}
	f.Turns = len(perMsg)
	for _, u := range perMsg {
		f.In += u.in
		f.Out += u.out
		f.CacheRead += u.cr
		f.CacheWrite += u.cw
	}
	var active time.Duration
	for i := 1; i < len(stamps); i++ {
		if gap := stamps[i].Sub(stamps[i-1]); gap > 0 && gap <= 5*time.Minute {
			active += gap
		}
	}
	f.ActiveMin = int(active / time.Minute)
	for _, t := range touched {
		f.Files = append(f.Files, *t)
	}
	sort.Slice(f.Files, func(i, j int) bool {
		a, b := f.Files[i], f.Files[j]
		if a.Edits != b.Edits {
			return a.Edits > b.Edits
		}
		if a.Reads != b.Reads {
			return a.Reads > b.Reads
		}
		return a.Path < b.Path
	})
	if len(f.Files) > 12 {
		f.Files = f.Files[:12]
	}
	if len(f.Commands) > 5 {
		f.Commands = f.Commands[len(f.Commands)-5:]
	}
	if f.Commands == nil {
		f.Commands = []string{}
	}
	if f.Files == nil {
		f.Files = []FileTouch{}
	}
	return f
}

func (f *Facts) scanTools(raw json.RawMessage, cwd string, touched map[string]*FileTouch) {
	var blocks []toolUse
	if json.Unmarshal(raw, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		f.Tools[b.Name]++
		path := b.Input.FilePath
		if path == "" {
			path = b.Input.NotebookPath
		}
		switch b.Name {
		case "Edit", "Write", "MultiEdit", "NotebookEdit", "Read":
			if path == "" {
				continue
			}
			t := touched[path]
			if t == nil {
				rel := path
				if cwd != "" && strings.HasPrefix(path, cwd+"/") {
					rel = strings.TrimPrefix(path, cwd+"/")
				}
				t = &FileTouch{Path: path, Rel: rel}
				touched[path] = t
			}
			if b.Name == "Read" {
				t.Reads++
			} else {
				t.Edits++
			}
		case "Bash":
			f.CommandCount++
			cmd := Clean(strings.SplitN(strings.TrimSpace(b.Input.Command), "\n", 2)[0], 110)
			if cmd != "" && (len(f.Commands) == 0 || f.Commands[len(f.Commands)-1] != cmd) {
				f.Commands = append(f.Commands, cmd)
			}
		}
	}
}

// Compact renders a token count such as 1.2k or 3.4M.
func Compact(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

// Duration renders minutes as 45m or 3h 20m.
func Duration(min int) string {
	if min < 60 {
		return fmt.Sprintf("%dm", min)
	}
	if min%60 == 0 {
		return fmt.Sprintf("%dh", min/60)
	}
	return fmt.Sprintf("%dh %dm", min/60, min%60)
}
