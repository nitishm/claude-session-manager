package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixture writes a transcript under a temp home and returns its path.
func fixture(t *testing.T, home, proj, id, cwd string, title string, prompts []string, uuids []string, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", proj)
	os.MkdirAll(dir, 0o755)
	var b strings.Builder
	if title != "" {
		fmt.Fprintf(&b, `{"type":"custom-title","customTitle":%q}`+"\n", title)
	}
	ts := time.Now().Add(-age).UTC().Format(time.RFC3339)
	for i, p := range prompts {
		u := fmt.Sprintf("%s-u%d", id, i)
		if i < len(uuids) {
			u = uuids[i]
		}
		fmt.Fprintf(&b, `{"type":"user","uuid":%q,"timestamp":%q,"cwd":%q,"message":{"content":%q}}`+"\n", u, ts, cwd, p)
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-age)
	os.Chtimes(path, mt, mt)
	return path
}

func newEng(t *testing.T) (*Engine, string) {
	home := t.TempDir()
	return New(home), home
}

func TestLoadAndLabel(t *testing.T) {
	e, home := newEng(t)
	fixture(t, home, "p", "aaa", "/work/api", "", []string{"fix the login bug in api"}, nil, time.Hour)
	fixture(t, home, "p", "bbb", "/work/web", "My Title", []string{"build the dashboard page"}, nil, 2*time.Hour)
	fixture(t, home, "p", "empty", "/work/x", "", nil, nil, time.Hour) // no prompts: skipped
	all := e.Load()
	if len(all) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(all))
	}
	byID := map[string]*Session{}
	for _, s := range all {
		byID[s.ID] = s
	}
	if got := byID["aaa"].Label(); got != "fix the login bug in api" {
		t.Errorf("label = %q", got)
	}
	if got := byID["bbb"].Label(); got != "My Title" {
		t.Errorf("label = %q", got)
	}
}

func TestQueryFilterAndSort(t *testing.T) {
	e, home := newEng(t)
	fixture(t, home, "p", "a", "/work/api", "", []string{"fix login bug"}, nil, 3*time.Hour)
	fixture(t, home, "p", "b", "/work/web", "", []string{"dashboard layout"}, nil, time.Hour)
	fixture(t, home, "p", "c", "/work/api", "", []string{"write docs", "more docs", "even more docs"}, nil, 2*time.Hour)
	all := e.Load()

	ids := func(rows []*Session) string {
		var o []string
		for _, s := range rows {
			o = append(o, s.ID)
		}
		return strings.Join(o, "")
	}
	if got := ids(e.Query(all, Opts{})); got != "bca" {
		t.Errorf("recent = %s", got)
	}
	if got := ids(e.Query(all, Opts{Sort: "oldest"})); got != "acb" {
		t.Errorf("oldest = %s", got)
	}
	if got := ids(e.Query(all, Opts{Sort: "prompts"})); got != "c" && got[0] != 'c' {
		t.Errorf("prompts = %s", got)
	}
	if got := ids(e.Query(all, Opts{Sort: "recent", Reverse: true})); got != "acb" {
		t.Errorf("reverse = %s", got)
	}
	if got := ids(e.Query(all, Opts{Dir: "/work/api"})); got != "ca" {
		t.Errorf("dir = %s", got)
	}
	if got := ids(e.Query(all, Opts{Text: "login"})); got != "a" {
		t.Errorf("text = %s", got)
	}
	if got := ids(e.Query(all, Opts{Text: "dshbrd"})); got != "b" { // dropped vowels: fuzzy match on title
		t.Errorf("fuzzy = %q", got)
	}
	e.TogglePin("a")
	if got := ids(e.Query(all, Opts{})); got != "abc" {
		t.Errorf("pinned first = %s", got)
	}
	rank := map[string]int{"c": 1, "a": 2}
	if got := ids(e.Query(all, Opts{AskRank: rank})); got != "ca" {
		t.Errorf("ask rank = %s", got)
	}
}

func TestTagsPinsPersistAndSearch(t *testing.T) {
	e, home := newEng(t)
	fixture(t, home, "p", "a", "/w", "", []string{"alpha work"}, nil, time.Hour)
	fixture(t, home, "p", "b", "/w", "", []string{"beta work"}, nil, 2*time.Hour)
	all := e.Load()
	e.SetTags("a", ParseTags("Infra, #k8s infra extra1 extra2 extra3 extra4"))
	if got := e.Tags("a"); len(got) != 5 || got[0] != "infra" || got[1] != "k8s" {
		t.Errorf("tags = %v", got)
	}
	if rows := e.Query(all, Opts{Tag: "k8s"}); len(rows) != 1 || rows[0].ID != "a" {
		t.Errorf("tag filter wrong: %d rows", len(rows))
	}
	if rows := e.Query(all, Opts{Text: "#infra"}); len(rows) != 1 { // tags are searchable, cache rebuilt on change
		t.Errorf("tag search = %d rows", len(rows))
	}
	e.AddTags("b", []string{"k8s", "x"})
	if tl := e.TagList(); tl[0].Name != "k8s" || tl[0].N != 2 {
		t.Errorf("taglist = %v", tl)
	}
	e.TogglePin("b")
	e.SetSort("dir")

	e2 := New(home) // a fresh engine reads the saved state
	if !e2.Pinned("b") || e2.Pinned("a") || e2.Sort() != "dir" || len(e2.Tags("a")) != 5 {
		t.Errorf("state not persisted: pinned b=%v sort=%s", e2.Pinned("b"), e2.Sort())
	}
}

func TestForkLinking(t *testing.T) {
	e, home := newEng(t)
	orig := []string{"o1", "o2", "o3", "o4"}
	fixture(t, home, "p", "orig", "/w", "", []string{"p1", "p2", "p3", "p4"}, orig, 3*time.Hour)
	// a fork copies the original's message uuids and was named by ccs
	fixture(t, home, "p", "fk", "/w", ForkMark+"orig · 10:00", []string{"p1", "p2", "p3", "p4", "new"}, append(orig, "n5"), time.Hour)
	all := e.Load()
	var o, f *Session
	for _, s := range all {
		if s.ID == "orig" {
			o = s
		} else {
			f = s
		}
	}
	if !f.Fork || f.ForkOf != o || o.Forks != 1 || o.Fork {
		t.Fatalf("fork link wrong: fork=%v of=%v forks=%d", f.Fork, f.ForkOf == o, o.Forks)
	}
	rows := e.Query(all, Opts{ForkOnly: true})
	if len(rows) != 2 || rows[0].ID != "orig" { // original directly above its fork
		t.Errorf("fork view order wrong")
	}
}

func TestSyncStubs(t *testing.T) {
	e, home := newEng(t)
	fixture(t, home, "p", "real", "/w", "", []string{"hello there"}, nil, time.Hour)
	all := e.Load()
	live := map[string]LiveInfo{
		"new1": {PID: 1, ID: "new1", Cwd: "/w", Kind: "interactive", UpdatedAt: time.Now().UnixMilli(), Name: ForkMark + "x · 09:00"},
		"real": {PID: 2, ID: "real", Cwd: "/w", Kind: "interactive", UpdatedAt: time.Now().UnixMilli()},
	}
	all, changed := e.SyncStubs(all, live)
	if !changed || len(all) != 2 {
		t.Fatalf("stub not added: changed=%v n=%d", changed, len(all))
	}
	stub := all[1]
	if stub.Path != "" || !stub.Fork {
		t.Errorf("stub wrong: path=%q fork=%v", stub.Path, stub.Fork)
	}
	all, _ = e.SyncStubs(all, map[string]LiveInfo{}) // session ended without a prompt: stub goes
	if len(all) != 1 {
		t.Errorf("stub not removed, n=%d", len(all))
	}
}

func TestReloadIncremental(t *testing.T) {
	e, home := newEng(t)
	fixture(t, home, "p", "a", "/w", "", []string{"first one"}, nil, time.Hour)
	all := e.Load()
	same, changed := e.Reload(all)
	if changed || len(same) != 1 || same[0] != all[0] {
		t.Fatalf("unchanged reload should reuse sessions")
	}
	fixture(t, home, "p", "b", "/w", "", []string{"second one"}, nil, time.Minute)
	more, changed := e.Reload(all)
	if !changed || len(more) != 2 {
		t.Fatalf("new transcript not picked up: changed=%v n=%d", changed, len(more))
	}
}

func TestDeleteAndUndo(t *testing.T) {
	e, home := newEng(t)
	p := fixture(t, home, "p", "a", "/w", "", []string{"to delete"}, nil, time.Hour)
	fixture(t, home, "p", "b", "/w", "", []string{"running one"}, nil, time.Hour)
	os.MkdirAll(strings.TrimSuffix(p, ".jsonl"), 0o755) // companion dir
	all := e.Load()
	live := map[string]LiveInfo{"b": {ID: "b"}}
	res, err := e.Delete(all, live)
	if err != nil || res.Moved != 1 || res.Skipped != 1 || !res.Gone["a"] {
		t.Fatalf("delete: %+v err=%v", res, err)
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("transcript still in place")
	}
	n, err := e.Undo()
	if err != nil || n != 1 {
		t.Fatalf("undo n=%d err=%v", n, err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("transcript not restored")
	}
	if _, err := os.Stat(strings.TrimSuffix(p, ".jsonl")); err != nil {
		t.Fatal("companion dir not restored")
	}
	if _, err := e.Undo(); err != os.ErrNotExist {
		t.Errorf("second undo should report nothing: %v", err)
	}
}

func TestStale(t *testing.T) {
	e, home := newEng(t)
	fixture(t, home, "p", "old", "/w", "", []string{"one prompt"}, nil, 30*24*time.Hour)
	fixture(t, home, "p", "fresh", "/w", "", []string{"one prompt"}, nil, time.Hour)
	fixture(t, home, "p", "tagged", "/w", "", []string{"one prompt"}, nil, 30*24*time.Hour)
	all := e.Load()
	e.SetTags("tagged", []string{"keep"})
	rows := e.Query(all, Opts{Stale: true})
	if len(rows) != 1 || rows[0].ID != "old" {
		t.Errorf("stale rows wrong: %d", len(rows))
	}
	rows = e.Query(all, Opts{Stale: true, Pre: map[string]bool{"old": true}})
	if len(rows) != 0 {
		t.Errorf("pre-reboot session must not be stale")
	}
}

func TestParsers(t *testing.T) {
	ss := []*Session{{ID: "s1"}, {ID: "s2"}}
	hits, err := parseAsk("noise [{\"n\":2,\"why\":\"fits\"},{\"n\":9,\"why\":\"bad\"},{\"n\":2,\"why\":\"dup\"}] tail", ss)
	if err != nil || len(hits) != 1 || hits[0].ID != "s2" {
		t.Errorf("parseAsk = %v %v", hits, err)
	}
	if _, err := parseAsk("no json", ss); err == nil {
		t.Error("expected error without JSON")
	}
	res, err := parseTagReply(`{"1":["Go","#K8s!!","averyveryverylongtagname"],"7":["bad"],"x":["bad"]}`, ss)
	if err != nil || len(res) != 1 {
		t.Fatalf("parseTagReply = %v %v", res, err)
	}
	if got := res["s1"]; got[0] != "go" || got[1] != "k8s" || len(got[2]) != 16 {
		t.Errorf("tags not sanitized: %v", got)
	}
}

func TestDeepSearch(t *testing.T) {
	e, home := newEng(t)
	fixture(t, home, "p", "a", "/w", "", []string{"investigating the zebra crash"}, nil, time.Hour)
	fixture(t, home, "p", "b", "/w", "", []string{"unrelated"}, nil, time.Hour)
	all := e.Load()
	hits := e.DeepSearch("ZEBRA crash", all)
	if len(hits) != 1 || hits[0].ID != "a" || !strings.Contains(hits[0].Why, "zebra") {
		t.Errorf("deep = %+v", hits)
	}
	if hits := e.DeepSearch("zebra giraffe", all); len(hits) != 0 { // every word must appear
		t.Errorf("deep should need all words: %+v", hits)
	}
}

func TestRepoOf(t *testing.T) {
	e, home := newEng(t)
	repo := filepath.Join(home, "proj")
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("ref: refs/heads/feature-x\n"), 0o644)
	os.MkdirAll(filepath.Join(repo, "sub", "dir"), 0o755)
	r := e.RepoOf(filepath.Join(repo, "sub", "dir"))
	if r.Kind != "git" || r.Branch != "feature-x" || r.String() != "git · feature-x" {
		t.Errorf("repo = %+v", r)
	}
	if r := e.RepoOf(home); r.Kind != "" {
		t.Errorf("non-repo = %+v", r)
	}
}

func TestMatchPos(t *testing.T) {
	if p := MatchPos([]rune("hello world"), []rune("wor"), false); len(p) != 3 || p[0] != 6 {
		t.Errorf("substring = %v", p)
	}
	if p := MatchPos([]rune("dashboard"), []rune("dshbrd"), true); p == nil {
		t.Error("fuzzy should match a tight subsequence")
	}
	if p := MatchPos([]rune("hello"), []rune("xyz"), true); p != nil {
		t.Errorf("no match = %v", p)
	}
}

func TestResumeCmd(t *testing.T) {
	s := &Session{ID: "abc", First: "do a thing"}
	if got := ResumeCmd("/bin/claude", s, false); got != "'/bin/claude' '--resume' 'abc'" {
		t.Errorf("resume = %s", got)
	}
	if got := ResumeCmd("/bin/claude", s, true); !strings.Contains(got, "'--fork-session'") || !strings.Contains(got, ForkMark+"do a thing") {
		t.Errorf("fork = %s", got)
	}
}

func TestSummaryAndBriefCache(t *testing.T) {
	e, home := newEng(t)
	fixture(t, home, "p", "a", "/w", "", []string{"some work"}, nil, time.Hour)
	all := e.Load()
	e.SetSummary(all[0], "a nice summary")
	if all[0].Summary != "a nice summary" {
		t.Fatal("summary not set on session")
	}
	if got := e.Load()[0].Summary; got != "a nice summary" { // reloaded from the cache file
		t.Errorf("summary not persisted: %q", got)
	}
	if _, ok := e.BriefCached(all[0]); ok {
		t.Error("no brief yet")
	}
	e.StoreBrief(BriefKey(all[0]), "Goal\n- x")
	if got, ok := e.BriefCached(all[0]); !ok || got != "Goal\n- x" {
		t.Errorf("brief cache = %q %v", got, ok)
	}
}

func TestParseBrief(t *testing.T) {
	good := "sure!\n{\"goal\":\"ship it\",\"status\":\"weird\",\"steps\":[{\"text\":\"a\",\"state\":\"done\"},{\"text\":\"b\",\"state\":\"nope\"},{\"text\":\"\",\"state\":\"done\"}],\"files\":[\"x\"]}"
	b, ok := ParseBrief(good)
	if !ok {
		t.Fatal("not parsed")
	}
	if b.Status != "active" || len(b.Steps) != 2 || b.Steps[1].State != "todo" {
		t.Fatalf("not normalized: %+v", b)
	}
	if d, n := b.Progress(); d != 1 || n != 2 {
		t.Fatalf("progress %d/%d", d, n)
	}
	for _, bad := range []string{"", "plain text brief\n- a", "{\"goal\":\"\"}", "{\"goal\":\"g\"}", "{broken"} {
		if _, ok := ParseBrief(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestBriefKindsAndFiles(t *testing.T) {
	b, ok := ParseBrief(`{"goal":"g","steps":[{"text":"a","state":"done","kind":"fix","outcome":"3 of 4 pass"},{"text":"b","state":"todo","kind":"nonsense"}],
	  "timeline":[{"when":"Oct 01","text":"t","kind":"research","outcome":"found it"}],
	  "files":["~/.aerospace.toml","src/main.go","Karabiner config JSON (hyper+c)","make suites ENV=dev","Makefile"]}`)
	if !ok {
		t.Fatal("not parsed")
	}
	if b.Steps[0].Kind != "fix" || b.Steps[0].Outcome != "3 of 4 pass" || b.Steps[1].Kind != "" {
		t.Errorf("kinds not normalized: %+v", b.Steps)
	}
	if b.Timeline[0].Kind != "research" || b.Timeline[0].Outcome != "found it" {
		t.Errorf("timeline = %+v", b.Timeline[0])
	}
	if len(b.Files) != 3 || b.Files[0] != "~/.aerospace.toml" || b.Files[2] != "Makefile" {
		t.Errorf("files = %v", b.Files)
	}
}

func TestReadExchanges(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, "s.jsonl")
	lines := `{"type":"user","timestamp":"2026-10-01T10:00:00Z","message":{"content":"first ask"}}
{"type":"assistant","timestamp":"2026-10-01T10:00:05Z","message":{"content":[{"type":"text","text":"working on it, this is a long enough reply"}]}}
{"type":"assistant","timestamp":"2026-10-01T10:00:09Z","message":{"content":[{"type":"text","text":"final answer: all done and verified here, thanks"}]}}
{"type":"user","timestamp":"2026-10-02T10:00:00Z","message":{"content":"second ask"}}
`
	os.WriteFile(p, []byte(lines), 0o644)
	ex := readExchanges(p)
	if len(ex) != 2 || ex[0].N != 1 || ex[1].N != 2 {
		t.Fatalf("ex = %+v", ex)
	}
	if ex[0].User != "first ask" || !strings.HasPrefix(ex[0].Reply, "final answer") || ex[1].Reply != "" {
		t.Errorf("pairing wrong: %+v", ex)
	}
}

func TestBriefLatestStale(t *testing.T) {
	e := &Engine{CacheDir: t.TempDir()}
	s := &Session{ID: "abc", MTime: time.Unix(1000, 0)}
	if _, ok := e.BriefLatest(s); ok {
		t.Fatal("nothing cached yet")
	}
	e.StoreBrief(BriefKey(s), "old")
	if bi, ok := e.BriefLatest(s); !ok || bi.Stale || bi.Text != "old" {
		t.Errorf("exact = %+v %v", bi, ok)
	}
	s.MTime = time.Unix(2000, 0)
	bi, ok := e.BriefLatest(s)
	if !ok || !bi.Stale || bi.Text != "old" || bi.At.Unix() != 1000 {
		t.Errorf("after the session changed = %+v %v", bi, ok)
	}
	other := &Session{ID: "abcd", MTime: time.Unix(1000, 0)}
	if _, ok := e.BriefLatest(other); ok {
		t.Error("a different session id must not match")
	}
}
