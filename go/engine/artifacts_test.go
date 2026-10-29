package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTranscript(t *testing.T, dir, id, body string) string {
	p := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const frame = `{"type":"frame-link","title":"Kargo board","frameUrl":"https://claude.ai/artifact/abc","path":"/nope","timestamp":"2026-10-20T10:00:00Z"}` + "\n"

func TestArtifactsDedupeAndSearch(t *testing.T) {
	dir := t.TempDir()
	e := New(t.TempDir())
	t0 := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	other := `{"type":"frame-link","title":"Ingress map","frameUrl":"https://claude.ai/artifact/def","timestamp":"2026-10-21T10:00:00Z"}` + "\n"
	orig := &Session{ID: "orig", Title: "Kargo work", Started: t0, MTime: t0, Path: writeTranscript(t, dir, "orig", frame+frame+other)}
	fork := &Session{ID: "fork", Title: "[fork] Kargo work", Fork: true, Started: t0.Add(-time.Hour), MTime: t0, Path: writeTranscript(t, dir, "fork", frame)}
	stub := &Session{ID: "stub", Title: "no transcript"}

	rows := e.Artifacts([]*Session{fork, stub, orig}, "")
	if len(rows) != 2 {
		t.Fatalf("want 2 rows (page listed once), got %d: %+v", len(rows), rows)
	}
	if rows[0].Title != "Ingress map" || rows[1].Title != "Kargo board" {
		t.Fatalf("want newest first, got %q then %q", rows[0].Title, rows[1].Title)
	}
	if rows[1].SessionID != "orig" {
		t.Fatalf("a fork must not own the page, got %q", rows[1].SessionID)
	}
	if got := e.Artifacts([]*Session{orig, fork}, "INGRESS"); len(got) != 1 || got[0].URL != "https://claude.ai/artifact/def" {
		t.Fatalf("search by title: %+v", got)
	}
	if got := e.Artifacts([]*Session{orig}, "kargo work"); len(got) != 2 {
		t.Fatalf("search by session name: %+v", got)
	}
}

func TestArtifactsDeepSearch(t *testing.T) {
	dir := t.TempDir()
	e := New(t.TempDir())
	page := filepath.Join(dir, "page.html")
	os.WriteFile(page, []byte("<html><head><style>.x{color:red}</style><script>var secretvar=1</script></head><body><h1>Intro</h1><p>The warehouse throttles ACR calls per process.</p></body></html>"), 0o644)
	line := `{"type":"frame-link","title":"Board","frameUrl":"https://claude.ai/artifact/q","path":"` + page + `","timestamp":"2026-10-20T10:00:00Z"}` + "\n"
	s := &Session{ID: "s1", Title: "work", Path: writeTranscript(t, dir, "s1", line), MTime: time.Now()}
	all := []*Session{s}
	if got := e.SearchArtifacts(all, "throttles", false); len(got) != 0 {
		t.Fatalf("shallow search must not read page text: %+v", got)
	}
	got := e.SearchArtifacts(all, "THROTTLES", true)
	if len(got) != 1 || !strings.Contains(got[0].Excerpt, "throttles ACR") {
		t.Fatalf("deep search: %+v", got)
	}
	if got := e.SearchArtifacts(all, "secretvar", true); len(got) != 0 {
		t.Fatalf("script bodies are not page text: %+v", got)
	}
	os.Remove(page)
	if got := e.SearchArtifacts(all, "throttles", true); len(got) != 0 {
		t.Fatalf("a missing source cannot match: %+v", got)
	}
}

func TestArtifactsRecoverFromTranscript(t *testing.T) {
	dir := t.TempDir()
	e := New(t.TempDir())
	gone := filepath.Join(dir, "gone.html")
	write := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"` + gone + `","content":"<h1>Hello old</h1>"}}]}}` + "\n"
	edit := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"` + gone + `","old_string":"old","new_string":"new"}}]}}` + "\n"
	frame := `{"type":"frame-link","title":"Gone","frameUrl":"https://claude.ai/artifact/g","path":"` + gone + `","timestamp":"2026-10-20T10:00:00Z"}` + "\n"
	s := &Session{ID: "s2", Title: "t", Path: writeTranscript(t, dir, "s2", write+frame+edit), MTime: time.Now()}
	rows := e.Artifacts([]*Session{s}, "")
	if len(rows) != 1 || rows[0].File == "" {
		t.Fatalf("want a recovered file: %+v", rows)
	}
	b, err := os.ReadFile(rows[0].File)
	if err != nil || string(b) != "<h1>Hello new</h1>" {
		t.Fatalf("recovered body %q (%v)", b, err)
	}
	if got := e.SearchArtifacts([]*Session{s}, "hello new", true); len(got) != 1 {
		t.Fatalf("deep search should read recovered pages: %+v", got)
	}
}

func TestDocsArtifacts(t *testing.T) {
	create := `{"timestamp":"2026-10-23T18:34:30Z","message":{"content":[{"type":"tool_use","id":"t1","name":"mcp__claude_ai_Claude_Docs__batch","input":{"container":{"kind":"project","create":{"name":"Interview Scorecard"}}}}]}}`
	result := `{"timestamp":"2026-10-23T18:34:37Z","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"{\"frame\":{\"url\":\"https://claude.ai/artifact/AbC123\",\"artifactUrl\":\"https://claude.ai/artifact/AbC123\"}}"}]}]}}`
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")
	os.WriteFile(p, []byte(create+"\n"+result+"\n"+result+"\n"), 0o644)
	e := &Engine{CacheDir: dir}
	got := e.readArtifacts(p)
	if len(got) != 1 || got[0].Title != "Interview Scorecard" || got[0].URL != "https://claude.ai/artifact/AbC123" {
		t.Fatalf("got %+v", got)
	}
}
