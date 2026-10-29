package engine

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// ArtifactRow is one published page with the session it came from.
type ArtifactRow struct {
	Artifact
	SessionID string
	Label     string
	Cwd       string
	Excerpt   string // where a deep search matched inside the page
}

var frameMark = []byte(`"frame-link"`)

// readArtifacts pulls only the frame-link lines out of a transcript. It skips JSON parsing for every
// other line, so scanning every session stays cheap.
func (e *Engine) readArtifacts(path string) []Artifact {
	fh, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer fh.Close()
	var out []Artifact
	paths := map[string]string{} // artifact URL -> path it was published from
	seen := map[string]bool{}
	docs := map[string]string{}  // Claude Docs create call id -> doc title, until its result names the URL
	pages := map[string]string{} // html files the session wrote, as they stand at the end
	rd := bufio.NewReaderSize(fh, 64<<10)
	for {
		line, err := rd.ReadBytes('\n')
		if bytes.Contains(line, htmlMark) && (bytes.Contains(line, writeMark) || bytes.Contains(line, editMark)) {
			trackPage(line, pages)
		}
		if bytes.Contains(line, docMark) || (len(docs) > 0 && bytes.Contains(line, docURLMark)) {
			trackDoc(line, docs, &out, seen)
		}
		if bytes.Contains(line, frameMark) {
			var d tlLine
			if json.Unmarshal(line, &d) == nil && d.Type == "frame-link" && d.FrameURL != "" && !seen[d.FrameURL] {
				seen[d.FrameURL] = true
				title := Clean(d.FrameTitle, timelineTextCh)
				if title == "" {
					title = "Untitled artifact"
				}
				a := Artifact{Title: title, URL: d.FrameURL}
				paths[d.FrameURL] = d.FramePath
				if t, perr := time.Parse(time.RFC3339, d.Timestamp); perr == nil {
					a.At = t
				}
				if st, serr := os.Stat(d.FramePath); serr == nil && !st.IsDir() {
					a.File = d.FramePath
				}
				out = append(out, a)
			}
		}
		if err != nil {
			if err != io.EOF {
				return out
			}
			break
		}
	}
	for i := range out {
		if out[i].File == "" {
			if body, ok := pages[paths[out[i].URL]]; ok {
				out[i].File = e.recoverPage(out[i].URL, body)
			}
		}
	}
	return out
}

var (
	docMark    = []byte("Claude_Docs__batch")
	docURLMark = []byte("artifactUrl")
	docURL     = regexp.MustCompile(`artifactUrl\\?":\\?"(https://claude\.ai/artifact/[A-Za-z0-9]+)`)
	htmlMark   = []byte(".htm")
	writeMark  = []byte(`"name":"Write"`)
	editMark   = []byte(`"name":"Edit"`)
)

type pageTool struct {
	Message struct {
		Content []struct {
			Type  string `json:"type"`
			Name  string `json:"name"`
			Input struct {
				FilePath   string `json:"file_path"`
				Content    string `json:"content"`
				OldString  string `json:"old_string"`
				NewString  string `json:"new_string"`
				ReplaceAll bool   `json:"replace_all"`
			} `json:"input"`
		} `json:"content"`
	} `json:"message"`
}

// trackPage replays the Write and Edit calls on html files, so a page whose file was later deleted can
// still be rebuilt from the transcript.
func trackPage(line []byte, pages map[string]string) {
	var d pageTool
	if json.Unmarshal(line, &d) != nil {
		return
	}
	for _, b := range d.Message.Content {
		p := b.Input.FilePath
		if b.Type != "tool_use" || !(strings.HasSuffix(p, ".html") || strings.HasSuffix(p, ".htm")) {
			continue
		}
		switch b.Name {
		case "Write":
			pages[p] = b.Input.Content
		case "Edit":
			cur, ok := pages[p]
			if !ok || b.Input.OldString == "" {
				continue
			}
			if b.Input.ReplaceAll {
				pages[p] = strings.ReplaceAll(cur, b.Input.OldString, b.Input.NewString)
			} else {
				pages[p] = strings.Replace(cur, b.Input.OldString, b.Input.NewString, 1)
			}
		}
	}
}

type docLine struct {
	Timestamp string `json:"timestamp"`
	Message   struct {
		Content []struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			ToolUseID string `json:"tool_use_id"`
			Input     struct {
				Container struct {
					Create struct {
						Name string `json:"name"`
					} `json:"create"`
				} `json:"container"`
			} `json:"input"`
		} `json:"content"`
	} `json:"message"`
}

// trackDoc handles Claude Docs pages. They never get a frame-link line: the create call carries the
// title and its result carries the artifact URL, so the two are matched by tool-use id.
func trackDoc(line []byte, docs map[string]string, out *[]Artifact, seen map[string]bool) {
	var d docLine
	if json.Unmarshal(line, &d) != nil {
		return
	}
	for _, b := range d.Message.Content {
		switch b.Type {
		case "tool_use":
			if n := b.Input.Container.Create.Name; n != "" {
				docs[b.ID] = n
			}
		case "tool_result":
			name, ok := docs[b.ToolUseID]
			m := docURL.FindSubmatch(line)
			if !ok || m == nil || seen[string(m[1])] {
				continue
			}
			seen[string(m[1])] = true
			a := Artifact{Title: Clean(name, timelineTextCh), URL: string(m[1])}
			if t, err := time.Parse(time.RFC3339, d.Timestamp); err == nil {
				a.At = t
			}
			*out = append(*out, a)
		}
	}
}

// recoverPage saves a rebuilt page under the cache dir and returns its path.
func (e *Engine) recoverPage(url, body string) string {
	sum := sha1.Sum([]byte(url))
	dir := filepath.Join(e.CacheDir, "artifacts")
	p := filepath.Join(dir, hex.EncodeToString(sum[:6])+".html")
	if os.MkdirAll(dir, 0o755) != nil || os.WriteFile(p, []byte(body), 0o644) != nil {
		return ""
	}
	return p
}

func (e *Engine) sessionArtifacts(s *Session) []Artifact {
	key := BriefKey(s)
	e.factsMu.Lock()
	if a, ok := e.artCache[key]; ok {
		e.factsMu.Unlock()
		return a
	}
	e.factsMu.Unlock()
	a := e.readArtifacts(s.Path)
	e.factsMu.Lock()
	if e.artCache == nil {
		e.artCache = map[string][]Artifact{}
	}
	for k := range e.artCache {
		if strings.HasPrefix(k, s.ID+":") {
			delete(e.artCache, k)
		}
	}
	e.artCache[key] = a
	e.factsMu.Unlock()
	return a
}

// Artifacts lists every published page across the given sessions, newest first. A fork copies its
// original's transcript, so the same page can appear in several: it is listed once, under the session
// that is not a fork, else the earliest one. q filters on title, link and session name.
func (e *Engine) Artifacts(all []*Session, q string) []ArtifactRow {
	return e.SearchArtifacts(all, q, false)
}

// SearchArtifacts is Artifacts, and with deep it also searches the text inside each page's source
// file. Pages whose source file is gone can only match on title, link and session name.
func (e *Engine) SearchArtifacts(all []*Session, q string, deep bool) []ArtifactRow {
	best := map[string]ArtifactRow{}
	owner := map[string]*Session{}
	for _, s := range all {
		if s.Path == "" {
			continue
		}
		for _, a := range e.sessionArtifacts(s) {
			if o, ok := owner[a.URL]; ok && !prefer(s, o) {
				continue
			}
			owner[a.URL] = s
			best[a.URL] = ArtifactRow{Artifact: a, SessionID: s.ID, Label: s.Label(), Cwd: s.Cwd}
		}
	}
	q = strings.ToLower(strings.TrimSpace(q))
	rows := make([]ArtifactRow, 0, len(best))
	for _, r := range best {
		if q != "" && !strings.Contains(strings.ToLower(r.Title+" "+r.URL+" "+r.Label), q) {
			if !deep || r.File == "" {
				continue
			}
			ex, ok := e.bodyMatch(r.File, q)
			if !ok {
				continue
			}
			r.Excerpt = ex
		}
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].At.Equal(rows[j].At) {
			return rows[i].At.After(rows[j].At)
		}
		return rows[i].URL < rows[j].URL
	})
	return rows
}

// prefer reports whether a should own a page that b already owns.
func prefer(a, b *Session) bool {
	if a.Fork != b.Fork {
		return !a.Fork
	}
	return a.Started.Before(b.Started)
}

// artBody is a source file reduced to its visible text.
type artBody struct {
	mod  time.Time
	text string
}

var (
	scriptRe = regexp.MustCompile(`(?is)<(script|style)\b.*?</(script|style)>`)
	tagRe    = regexp.MustCompile(`(?s)<[^>]*>`)
	spaceRe  = regexp.MustCompile(`\s+`)
)

const maxArtifactRead = 4 << 20

func (e *Engine) bodyText(path string) string {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return ""
	}
	e.factsMu.Lock()
	if b, ok := e.artText[path]; ok && b.mod.Equal(st.ModTime()) {
		e.factsMu.Unlock()
		return b.text
	}
	e.factsMu.Unlock()
	fh, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer fh.Close()
	raw, _ := io.ReadAll(io.LimitReader(fh, maxArtifactRead))
	t := string(raw)
	if !utf8.ValidString(t) {
		t = strings.ToValidUTF8(t, "")
	}
	t = scriptRe.ReplaceAllString(t, " ")
	t = tagRe.ReplaceAllString(t, " ")
	t = strings.TrimSpace(spaceRe.ReplaceAllString(t, " "))
	e.factsMu.Lock()
	if e.artText == nil {
		e.artText = map[string]artBody{}
	}
	e.artText[path] = artBody{st.ModTime(), t}
	e.factsMu.Unlock()
	return t
}

// bodyMatch looks for q (already lowercase) in the page text and returns a snippet around the hit.
func (e *Engine) bodyMatch(path, q string) (string, bool) {
	t := e.bodyText(path)
	if t == "" {
		return "", false
	}
	low := strings.ToLower(t)
	i := strings.Index(low, q)
	if i < 0 || len(low) != len(t) { // lowercasing can change byte offsets; skip the snippet then
		if i < 0 {
			return "", false
		}
		return Clean(t, 120), true
	}
	from, to := max(0, i-50), min(len(t), i+len(q)+70)
	for from > 0 && !utf8.RuneStart(t[from]) {
		from--
	}
	for to < len(t) && !utf8.RuneStart(t[to]) {
		to++
	}
	ex := strings.TrimSpace(t[from:to])
	if from > 0 {
		ex = "…" + ex
	}
	if to < len(t) {
		ex += "…"
	}
	return ex, true
}
