package engine

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// ConvoTurn is one real user prompt and everything the assistant said in words before the next prompt.
type ConvoTurn struct {
	N     int
	When  string
	User  string
	Reply string
	Runs  []ToolRun
}

// ToolRun is a shell command (Claude's Bash, or the user's own "!" command) or a failed tool call, with what it printed.
type ToolRun struct {
	Name string
	Cmd  string
	Out  string
	Err  bool
}

const (
	askBudget    = 240000 // characters of conversation sent for one question: about 60k tokens
	turnUserMax  = 3000
	turnReplyMax = 3000
	runOutMax    = 1500 // characters of one command's output: a short head and a longer tail, where errors usually are
	runsMax      = 7000 // characters of tool output kept for one turn
)

type convoBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
	Input     struct {
		Command     string `json:"command"`
		FilePath    string `json:"file_path"`
		Pattern     string `json:"pattern"`
		URL         string `json:"url"`
		Description string `json:"description"`
	} `json:"input"`
}

type pendingRun struct{ turn, run int } // run is -1 when the call is only recorded if it fails

func convoBlocks(raw json.RawMessage) []convoBlock {
	if len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var bs []convoBlock
	_ = json.Unmarshal(raw, &bs)
	return bs
}

// resultText flattens a tool_result's content, which is a string or a list of text blocks.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var out []string
		for _, p := range parts {
			out = append(out, p.Text)
		}
		return strings.Join(out, "\n")
	}
	return ""
}

// trimOutput collapses whitespace and keeps the start and the end of long output.
func trimOutput(s string, max int) string {
	s = Clean(s, 1<<20)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	head := max * 2 / 5
	return string(r[:head]) + " […] " + string(r[len(r)-(max-head):])
}

// shellTag pulls the text between <tag> and </tag>.
func shellTag(s, tag string) (string, bool) {
	a := strings.Index(s, "<"+tag+">")
	if a < 0 {
		return "", false
	}
	a += len(tag) + 2
	b := strings.Index(s[a:], "</"+tag+">")
	if b < 0 {
		return s[a:], true
	}
	return s[a : a+b], true
}

// readConversation reads the whole transcript as turns. Unlike readExchanges it keeps every assistant
// text block, not just the last, and far more of each message, so a question can reach the middle of a session.
func readConversation(path string) []ConvoTurn {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var turns []ConvoTurn
	pending := map[string]pendingRun{}
	pendingLabel := map[string]string{}
	lastShell := -1
	rd := bufio.NewReaderSize(f, 64<<10)
	for {
		line, err := rd.ReadBytes('\n')
		if len(line) > 2 && wanted(line) {
			var d rawLine
			if json.Unmarshal(line, &d) == nil && !d.IsSidechain && !d.IsMeta {
				switch d.Type {
				case "user":
					raw := textOf(d.Message.Content)
					if len(turns) > 0 {
						cur := len(turns) - 1
						if cmd, ok := shellTag(raw, "bash-input"); ok { // the user's own "!" command
							turns[cur].Runs = append(turns[cur].Runs, ToolRun{Name: "!", Cmd: Clean(cmd, 300)})
							lastShell = len(turns[cur].Runs) - 1
						}
						if out, ok := shellTag(raw, "bash-stdout"); ok && lastShell >= 0 && lastShell < len(turns[cur].Runs) {
							errOut, _ := shellTag(raw, "bash-stderr")
							turns[cur].Runs[lastShell].Out = trimOutput(out+" "+errOut, runOutMax)
							lastShell = -1
						}
						for _, b := range convoBlocks(d.Message.Content) {
							if b.Type != "tool_result" {
								continue
							}
							p, ok := pending[b.ToolUseID]
							if !ok || p.turn >= len(turns) {
								continue
							}
							out := trimOutput(resultText(b.Content), runOutMax)
							switch {
							case p.run >= 0 && p.run < len(turns[p.turn].Runs):
								turns[p.turn].Runs[p.run].Out, turns[p.turn].Runs[p.run].Err = out, b.IsError
							case b.IsError:
								turns[p.turn].Runs = append(turns[p.turn].Runs, ToolRun{Name: "failed", Cmd: pendingLabel[b.ToolUseID], Out: out, Err: true})
							}
							delete(pending, b.ToolUseID)
							delete(pendingLabel, b.ToolUseID)
						}
					}
					if t := unwrapCmd(raw); t != "" && !hasNoise(t) {
						turns = append(turns, ConvoTurn{N: len(turns) + 1, When: day(d.Timestamp), User: Clean(t, turnUserMax)})
					}
				case "assistant":
					if len(turns) > 0 {
						cur := len(turns) - 1
						for _, b := range convoBlocks(d.Message.Content) {
							if b.Type != "tool_use" {
								continue
							}
							if b.Name == "Bash" {
								turns[cur].Runs = append(turns[cur].Runs, ToolRun{Name: "Bash", Cmd: Clean(b.Input.Command, 300)})
								pending[b.ID] = pendingRun{cur, len(turns[cur].Runs) - 1}
							} else {
								pending[b.ID] = pendingRun{cur, -1}
								pendingLabel[b.ID] = Clean(strings.TrimSpace(b.Name+" "+firstNonEmpty(b.Input.FilePath, b.Input.Pattern, b.Input.URL, b.Input.Description)), 200)
							}
						}
					}
					if t := strings.TrimSpace(textOf(d.Message.Content)); t != "" && len(turns) > 0 {
						cur := &turns[len(turns)-1]
						if cur.Reply != "" {
							cur.Reply += "\n"
						}
						if len(cur.Reply) < turnReplyMax {
							cur.Reply += Clean(t, turnReplyMax-len(cur.Reply))
						}
					}
				}
			}
		}
		if err != nil {
			break
		}
	}
	return turns
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func (t ConvoTurn) render() string {
	l := fmt.Sprintf("#%d [%s] user: %s", t.N, t.When, t.User)
	used := 0
	for _, r := range t.Runs {
		kind := "ran"
		switch {
		case r.Name == "!":
			kind = "user ran"
		case r.Err:
			kind = "error in"
		}
		line := fmt.Sprintf("\n    %s: %s", kind, r.Cmd)
		if r.Out != "" {
			line += "\n      output: " + r.Out
		}
		if used+len(line) > runsMax {
			l += "\n    [… more commands in this turn left out …]"
			break
		}
		used += len(line)
		l += line
	}
	if t.Reply != "" {
		l += "\n    claude: " + t.Reply
	}
	return l
}

// convoDigest renders the whole conversation when it fits the budget. When it does not, it keeps the first
// and last turns plus the turns that share the most words with the question, in their original order.
func convoDigest(turns []ConvoTurn, question string, budget int) string {
	rendered := make([]string, len(turns))
	total := 0
	for i, t := range turns {
		rendered[i] = t.render()
		total += len(rendered[i]) + 1
	}
	if total <= budget {
		return fmt.Sprintf("Conversation (all %d turns, oldest first):\n%s\n", len(turns), strings.Join(rendered, "\n"))
	}
	keep := map[int]bool{}
	used := 0
	take := func(i int) bool {
		if keep[i] || used+len(rendered[i])+1 > budget {
			return false
		}
		keep[i] = true
		used += len(rendered[i]) + 1
		return true
	}
	for i := 0; i < len(turns) && i < 5; i++ { // the opening sets the goal
		take(i)
	}
	for i := len(turns) - 1; i >= 0 && i >= len(turns)-15; i-- { // the end holds the outcome
		take(i)
	}
	words := questionWords(question)
	type scored struct{ i, score int }
	var sc []scored
	for i, t := range turns {
		low := strings.ToLower(t.User + " " + t.Reply)
		n := 0
		for _, w := range words {
			n += strings.Count(low, w)
		}
		if n > 0 {
			sc = append(sc, scored{i, n})
		}
	}
	sort.SliceStable(sc, func(a, b int) bool { return sc[a].score > sc[b].score })
	for _, c := range sc {
		take(c.i)
	}
	var idx []int
	for i := range keep {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	var out []string
	prev := -1
	for _, i := range idx {
		if i != prev+1 {
			out = append(out, fmt.Sprintf("[… turns %d to %d left out …]", prev+2, i))
		}
		out = append(out, rendered[i])
		prev = i
	}
	if prev != len(turns)-1 {
		out = append(out, fmt.Sprintf("[… turns %d to %d left out …]", prev+2, len(turns)))
	}
	return fmt.Sprintf("Conversation (%d of %d turns, picked for relevance to the question):\n%s\n", len(idx), len(turns), strings.Join(out, "\n"))
}

var stopWords = map[string]bool{"what": true, "which": true, "when": true, "where": true, "that": true, "this": true, "with": true, "from": true, "have": true, "about": true, "does": true, "were": true, "there": true, "their": true, "session": true, "did": true}

// questionWords returns the lowercase words of q worth matching: four letters or more, not filler.
func questionWords(q string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' || r == '/')
	}) {
		if len(w) >= 4 && !stopWords[w] && !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}
