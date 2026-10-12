package engine

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// Event kinds in a Timeline.
const (
	EvPrompt    = "prompt"    // a user message; carries the turn's duration and counts
	EvCommit    = "commit"    // git commit, jj describe or jj commit
	EvPush      = "push"      // git push, jj git push
	EvPR        = "pr"        // a PR was linked, opened or merged
	EvCompact   = "compact"   // the conversation was compacted
	EvAway      = "away"      // Claude's own recap, written when the user was away
	EvTitle     = "title"     // the user renamed the session
	EvAgent     = "agent"     // a subagent was started
	EvAsk       = "ask"       // Claude asked the user a question
	EvResume    = "resume"    // the session was resumed
	EvGap       = "gap"       // the session sat idle for a while
	EvSchedule  = "schedule"  // a loop or cron woke the session
	EvArtifact  = "artifact"  // an artifact was published
	EvAPIError  = "api-error" // the API dropped or errored; Detail counts retries
	EvInterrupt = "interrupt" // the user cut a response short
)

// Event is one thing that happened in a session. Prompt events carry the stats of the turn that followed.
type Event struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Text   string    `json:"text"`
	Detail string    `json:"detail,omitempty"`
	N      int       `json:"n,omitempty"`      // prompt number, for prompt events
	Secs   int       `json:"secs,omitempty"`   // how long the turn took
	Tools  int       `json:"tools,omitempty"`  // tool calls in the turn
	Edits  int       `json:"edits,omitempty"`  // file edits in the turn
	Errors int       `json:"errors,omitempty"` // failed tool calls in the turn
}

// Timeline is the whole history of a session read from its transcript. No model call is involved.
type Timeline struct {
	Events []Event `json:"events"`
}

const (
	idleGap        = 30 * time.Minute
	maxTimeline    = 1500
	timelineTextCh = 140
)

type tlLine struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	Timestamp        string          `json:"timestamp"`
	Content          json.RawMessage `json:"content"` // system lines carry a plain string here
	IsSidechain      bool            `json:"isSidechain"`
	IsMeta           bool            `json:"isMeta"`
	IsCompactSummary bool            `json:"isCompactSummary"`
	CustomTitle      string          `json:"customTitle"`
	PRURL            string          `json:"prUrl"`
	PRNumber         int             `json:"prNumber"`
	FrameTitle       string          `json:"title"`
	DurationMs       int64           `json:"durationMs"`
	RetryAttempt     int             `json:"retryAttempt"`
	Compact          struct {
		Trigger    string `json:"trigger"`
		PreTokens  int64  `json:"preTokens"`
		PostTokens int64  `json:"postTokens"`
	} `json:"compactMetadata"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type tlBlock struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	IsError bool   `json:"is_error"`
	Input   struct {
		Command     string `json:"command"`
		FilePath    string `json:"file_path"`
		Description string `json:"description"`
		Prompt      string `json:"prompt"`
		Questions   []struct {
			Question string `json:"question"`
		} `json:"questions"`
	} `json:"input"`
}

var (
	commitRe = regexp.MustCompile(`(^|[;&|] *)(git (-C \S+ )?commit|jj (describe|commit))\b`)
	pushRe   = regexp.MustCompile(`(^|[;&|] *)(git (-C \S+ )?push|jj git push)\b`)
	prRe     = regexp.MustCompile(`(^|[;&|] *)gh pr (create|merge|ready|close)\b`)
	editTool = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}
)

// ReadTimeline scans a transcript once and returns everything notable in it, oldest first.
func ReadTimeline(path string) *Timeline {
	tl := &Timeline{Events: []Event{}}
	fh, err := os.Open(path)
	if err != nil {
		return tl
	}
	defer fh.Close()
	seenPR := map[int]bool{}
	var (
		cur      = -1 // index of the latest prompt event
		last     time.Time
		title    string
		apiErrs  int
		apiAt    time.Time
		flushAPI = func() {
			if apiErrs > 0 {
				tl.Events = append(tl.Events, Event{At: apiAt, Kind: EvAPIError, Text: "API connection errors", Detail: fmt.Sprintf("%d retries", apiErrs)})
				apiErrs = 0
			}
		}
		add = func(e Event) {
			if len(tl.Events) < maxTimeline {
				tl.Events = append(tl.Events, e)
			}
		}
	)
	rd := bufio.NewReaderSize(fh, 64<<10)
	for {
		line, err := rd.ReadBytes('\n')
		if len(line) > 2 {
			var d tlLine
			if json.Unmarshal(line, &d) == nil {
				at, _ := time.Parse(time.RFC3339Nano, d.Timestamp)
				at = at.Local()
				main := !d.IsSidechain && !at.IsZero()
				if main && (d.Type == "user" || d.Type == "assistant") {
					if !last.IsZero() && at.Sub(last) > idleGap {
						add(Event{At: at, Kind: EvGap, Text: "idle " + gapText(at.Sub(last))})
					}
					last = at
				}
				switch d.Type {
				case "user":
					if d.IsSidechain {
						break
					}
					if d.IsCompactSummary {
						break
					}
					if tr := userBlocks(d.Message.Content); tr > 0 && cur >= 0 {
						tl.Events[cur].Errors += tr
					}
					if d.IsMeta {
						break
					}
					raw := textOf(d.Message.Content)
					t := unwrapCmd(raw)
					switch {
					case strings.Contains(raw, "[Request interrupted by user"):
						add(Event{At: at, Kind: EvInterrupt, Text: "interrupted"})
					case t == "" || hasNoise(t):
					default:
						n := 1
						for _, e := range tl.Events {
							if e.Kind == EvPrompt {
								n++
							}
						}
						add(Event{At: at, Kind: EvPrompt, Text: Clean(t, timelineTextCh), N: n})
						cur = len(tl.Events) - 1
					}
				case "assistant":
					if d.IsSidechain {
						break
					}
					for _, b := range blocks(d.Message.Content) {
						if b.Type != "tool_use" {
							continue
						}
						if cur >= 0 && cur < len(tl.Events) && tl.Events[cur].Kind == EvPrompt {
							tl.Events[cur].Tools++
							if editTool[b.Name] {
								tl.Events[cur].Edits++
							}
						}
						switch {
						case b.Name == "Bash":
							c := strings.TrimSpace(b.Input.Command)
							first := Clean(strings.SplitN(c, "\n", 2)[0], timelineTextCh)
							switch {
							case commitRe.MatchString(c):
								add(Event{At: at, Kind: EvCommit, Text: commitText(c, first)})
							case pushRe.MatchString(c):
								add(Event{At: at, Kind: EvPush, Text: first})
							case prRe.MatchString(c):
								add(Event{At: at, Kind: EvPR, Text: first})
							}
						case b.Name == "Agent" || b.Name == "Task":
							x := b.Input.Description
							if x == "" {
								x = b.Input.Prompt
							}
							add(Event{At: at, Kind: EvAgent, Text: Clean(x, timelineTextCh)})
						case b.Name == "AskUserQuestion" && len(b.Input.Questions) > 0:
							add(Event{At: at, Kind: EvAsk, Text: Clean(b.Input.Questions[0].Question, timelineTextCh)})
						}
					}
				case "system":
					switch d.Subtype {
					case "turn_duration":
						if cur >= 0 && tl.Events[cur].Kind == EvPrompt && tl.Events[cur].Secs == 0 {
							tl.Events[cur].Secs = int(d.DurationMs / 1000)
						}
					case "compact_boundary":
						det := d.Compact.Trigger
						if d.Compact.PreTokens > 0 {
							det += fmt.Sprintf(", %s to %s tokens", Compact(d.Compact.PreTokens), Compact(d.Compact.PostTokens))
						}
						add(Event{At: at, Kind: EvCompact, Text: "conversation compacted", Detail: det})
					case "away_summary":
						add(Event{At: at, Kind: EvAway, Text: Clean(jsonString(d.Content), 400)})
					case "local_command":
						if c := jsonString(d.Content); strings.Contains(c, "<command-name>/resume</command-name>") {
							add(Event{At: at, Kind: EvResume, Text: "resumed"})
						}
					case "scheduled_task_fire":
						add(Event{At: at, Kind: EvSchedule, Text: Clean(jsonString(d.Content), timelineTextCh)})
					case "api_error":
						if apiErrs == 0 {
							apiAt = at
						}
						apiErrs++
					}
				case "custom-title":
					if d.CustomTitle != "" && d.CustomTitle != title {
						title = d.CustomTitle
						add(Event{At: nonZero(at, last), Kind: EvTitle, Text: "renamed to " + d.CustomTitle})
					}
				case "pr-link":
					if seenPR[d.PRNumber] {
						break // the transcript repeats the link on every turn
					}
					seenPR[d.PRNumber] = true
					add(Event{At: nonZero(at, last), Kind: EvPR, Text: fmt.Sprintf("PR #%d linked", d.PRNumber), Detail: d.PRURL})
				case "frame-link":
					add(Event{At: nonZero(at, last), Kind: EvArtifact, Text: Clean(d.FrameTitle, timelineTextCh)})
				}
				if d.Type != "system" || d.Subtype != "api_error" {
					flushAPI()
				}
			}
		}
		if err != nil {
			break
		}
	}
	flushAPI()
	return tl
}

func nonZero(a, b time.Time) time.Time {
	if a.IsZero() {
		return b
	}
	return a
}

func blocks(raw json.RawMessage) []tlBlock {
	if len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var bs []tlBlock
	_ = json.Unmarshal(raw, &bs)
	return bs
}

// userBlocks counts failed tool results in a user message.
func userBlocks(raw json.RawMessage) int {
	n := 0
	for _, b := range blocks(raw) {
		if b.Type == "tool_result" && b.IsError {
			n++
		}
	}
	return n
}

func jsonString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

// commitText prefers the -m message of a commit command, which says what the commit was.
// Heredoc messages ("$(cat <<'EOF' ...) put the subject on the next line.
func commitText(cmd, first string) string {
	i := strings.Index(cmd, "-m ")
	if i < 0 {
		return first
	}
	rest := cmd[i+3:]
	lines := strings.Split(rest, "\n")
	head := strings.TrimLeft(lines[0], " \"'")
	if strings.HasPrefix(head, "$(cat <<") {
		for _, l := range lines[1:] {
			if l = strings.TrimSpace(l); l != "" && l != "EOF" {
				return Clean(l, timelineTextCh)
			}
		}
		return first
	}
	if head = strings.TrimRight(head, "\"'"); head != "" {
		return Clean(head, timelineTextCh)
	}
	return first
}

func gapText(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// Timeline returns the history of s, cached until the session changes.
func (e *Engine) Timeline(s *Session) *Timeline {
	key := BriefKey(s)
	e.factsMu.Lock()
	if t, ok := e.timelines[key]; ok {
		e.factsMu.Unlock()
		return t
	}
	e.factsMu.Unlock()
	t := ReadTimeline(s.Path)
	e.factsMu.Lock()
	if e.timelines == nil {
		e.timelines = map[string]*Timeline{}
	}
	for k := range e.timelines {
		if strings.HasPrefix(k, s.ID+":") {
			delete(e.timelines, k)
		}
	}
	e.timelines[key] = t
	e.factsMu.Unlock()
	return t
}
