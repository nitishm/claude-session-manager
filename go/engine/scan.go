package engine

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

type rawLine struct {
	Timestamp   string `json:"timestamp"`
	UUID        string `json:"uuid"`
	Type        string `json:"type"`
	CustomTitle string `json:"customTitle"`
	AITitle     string `json:"aiTitle"`
	LastPrompt  string `json:"lastPrompt"`
	PRURL       string `json:"prUrl"`
	URL         string `json:"url"`
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	Cwd         string `json:"cwd"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

func textOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

var cmdRe = regexp.MustCompile(`(?s)<command-message>(.*?)</command-message>\s*<command-name>(.*?)</command-name>(?:\s*<command-args>(.*?)</command-args>)?`)

// slash-command prompts arrive as XML wrappers; show them as "/name args".
func unwrapCmd(t string) string {
	if m := cmdRe.FindStringSubmatch(t); m != nil {
		return strings.TrimSpace("/" + strings.TrimPrefix(m[2], "/") + " " + m[3])
	}
	return t
}

var wantedTypes = [][]byte{[]byte(`"type":"user"`), []byte(`"type":"assistant"`), []byte(`"type":"custom-title"`),
	[]byte(`"type":"ai-title"`), []byte(`"type":"pr-link"`), []byte(`"type":"last-prompt"`)}

// wanted is a cheap byte check so big attachment/snapshot lines are never JSON-decoded.
func wanted(line []byte) bool {
	// "type" sits in the first 400 bytes for most lines and in the last 700 for assistant lines
	// (their big message body comes first), so the huge middle never needs scanning.
	head, tail := line, []byte(nil)
	if len(line) > 1100 {
		head, tail = line[:400], line[len(line)-700:]
	}
	for _, w := range wantedTypes {
		if bytes.Contains(head, w) || (tail != nil && bytes.Contains(tail, w)) {
			return true
		}
	}
	return false
}

var noisePfx = []string{"<local-command", "<command-name", "<command-message", "<system-reminder", "Caveat:", "[Request interrupted", "<task-notification", "<pasted_content", "<bash-", "<ide_"}

func hasNoise(t string) bool {
	t = strings.TrimLeft(t, " \n\t")
	if t == "/clear" || strings.HasPrefix(t, "/clear ") {
		return true
	}
	for _, p := range noisePfx {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// userText returns the real prompt of a user line, or "" for tool results and injected noise.
func userText(d *rawLine) string {
	t := unwrapCmd(textOf(d.Message.Content))
	if t == "" || hasNoise(t) {
		return ""
	}
	return t
}

func (e *Engine) scan(path string, cache map[string]string) *Session {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, _ := f.Stat()
	s := &Session{ID: strings.TrimSuffix(filepath.Base(path), ".jsonl"), Path: path, MTime: st.ModTime()}
	rd := bufio.NewReaderSize(f, 64<<10)
	for {
		line, err := rd.ReadBytes('\n')
		if len(line) > 2 && wanted(line) {
			var d rawLine
			if json.Unmarshal(line, &d) == nil {
				if d.UUID != "" && (d.Type == "user" || d.Type == "assistant") {
					s.UH = append(s.UH, h64(d.UUID))
				}
				switch d.Type {
				case "custom-title":
					if d.CustomTitle != "" {
						s.Title = d.CustomTitle
					}
				case "ai-title":
					if d.AITitle != "" {
						s.AI = d.AITitle
					}
				case "pr-link":
					if d.PRURL != "" {
						s.PR = d.PRURL
					} else if d.URL != "" {
						s.PR = d.URL
					}
				case "last-prompt":
					if c := Clean(d.LastPrompt, 200); c != "" {
						s.Last = c
					}
				case "user":
					if d.IsMeta || d.IsSidechain {
						break
					}
					if s.Cwd == "" {
						s.Cwd = d.Cwd
					}
					if s.Started.IsZero() {
						if ts, err := time.Parse(time.RFC3339, d.Timestamp); err == nil {
							s.Started = ts
						}
					}
					if txt := userText(&d); txt != "" {
						s.Prompts++
						c := Clean(txt, 300)
						if s.First == "" {
							s.First = c
						}
						s.Last = c
						s.PList = append(s.PList, c)
						s.addTurn(true, c)
					}
				case "assistant":
					if d.IsSidechain {
						break
					}
					if at := textOf(d.Message.Content); len(at) > 40 {
						s.AText = Clean(at, 500)
						s.addTurn(false, Clean(at, 400))
					}
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				return nil
			}
			break
		}
	}
	if s.Prompts == 0 {
		return nil
	}
	s.Summary = cache[summaryKey(s)]
	return s
}

func summaryKey(s *Session) string { return fmt.Sprintf("%s:%d", s.ID, s.MTime.Unix()) }

// Load scans every transcript and links forks to their originals.
func (e *Engine) Load() []*Session {
	files, _ := filepath.Glob(e.projectsGlob())
	cache := e.loadSummaries()
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out []*Session
		sem = make(chan struct{}, 6)
	)
	for _, p := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(p string) {
			defer wg.Done()
			defer func() { <-sem }()
			if s := e.scan(p, cache); s != nil {
				mu.Lock()
				out = append(out, s)
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()
	e.LinkForks(out)
	return out
}

// Reload rescans only transcripts whose mtime changed; unchanged sessions keep their parsed
// data. It never touches the sessions in prev, so it can run off the UI goroutine. Placeholder
// sessions (no transcript yet) in prev are ignored. The result is not fork-linked: call LinkForks.
func (e *Engine) Reload(prev []*Session) (all []*Session, changed bool) {
	files, _ := filepath.Glob(e.projectsGlob())
	old := make(map[string]*Session, len(prev))
	real := 0
	for _, s := range prev {
		if s.Path != "" {
			old[s.Path] = s
			real++
		}
	}
	var cache map[string]string
	for _, p := range files {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if o, ok := old[p]; ok && o.MTime.Equal(st.ModTime()) {
			all = append(all, o)
			continue
		}
		e.mu.Lock()
		t, skip := e.emptyAt[p]
		e.mu.Unlock()
		if skip && t.Equal(st.ModTime()) {
			continue
		}
		if cache == nil {
			cache = e.loadSummaries()
		}
		s := e.scan(p, cache)
		if s == nil {
			e.mu.Lock()
			e.emptyAt[p] = st.ModTime() // no real prompts yet: do not rescan until the file changes
			e.mu.Unlock()
			if _, was := old[p]; was {
				changed = true
			}
			continue
		}
		changed = true
		all = append(all, s)
	}
	if len(all) != real {
		changed = true
	}
	if changed {
		debug.FreeOSMemory()
	}
	return all, changed
}

// ───────────────────────── summary cache ─────────────────────────

func (e *Engine) loadSummaries() map[string]string {
	m := map[string]string{}
	if b, err := os.ReadFile(e.file("summaries.json")); err == nil {
		json.Unmarshal(b, &m)
	}
	return m
}

// StoreSummary caches a one-line summary on disk under key (see SummaryKey).
func (e *Engine) StoreSummary(key, text string) {
	m := e.loadSummaries() // read-modify-write so the python tool's entries survive
	m[key] = text
	os.MkdirAll(e.CacheDir, 0o755)
	b, _ := json.Marshal(m)
	os.WriteFile(e.file("summaries.json"), b, 0o644)
}

// SetSummary stores a summary on disk and on the session. Call it from the goroutine that owns s.
func (e *Engine) SetSummary(s *Session, text string) {
	e.StoreSummary(summaryKey(s), text)
	s.Summary = text
	s.hayC = ""
}
