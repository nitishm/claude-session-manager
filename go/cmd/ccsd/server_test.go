package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ccsgo/engine"
)

func fixture(t *testing.T, home, id, cwd string, prompts []string, age time.Duration) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", "p")
	os.MkdirAll(dir, 0o755)
	var b strings.Builder
	ts := time.Now().Add(-age).UTC().Format(time.RFC3339)
	for i, p := range prompts {
		fmt.Fprintf(&b, `{"type":"user","uuid":"%s-%d","timestamp":%q,"cwd":%q,"message":{"content":%q}}`+"\n", id, i, ts, cwd, p)
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-age)
	os.Chtimes(path, mt, mt)
}

// fakeClaude puts a `claude` script first in PATH that prints out.
func fakeClaude(t *testing.T, out string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\ncat <<'EOF'\n" + out + "\nEOF\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func setup(t *testing.T) (*httptest.Server, *Server, string) {
	home := t.TempDir()
	fixture(t, home, "aaa", "/work/api", []string{"fix the login bug"}, time.Hour)
	fixture(t, home, "bbb", "/work/web", []string{"build the dashboard"}, 2*time.Hour)
	fixture(t, home, "ccc", "/work/api", []string{"write docs"}, 30*24*time.Hour)
	s := NewServer(engine.New(home))
	s.pre = map[string]bool{} // the boot-time guess depends on this machine, not the fixtures
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, s, home
}

func call(t *testing.T, ts *httptest.Server, method, path string, body any, out any) int {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, ts.URL+path, rd)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("%s %s: bad JSON: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

func ids(vs []SessionView) string {
	var o []string
	for _, v := range vs {
		o = append(o, v.ID)
	}
	return strings.Join(o, ",")
}

func TestListFilterSort(t *testing.T) {
	ts, _, _ := setup(t)
	var vs []SessionView
	if c := call(t, ts, "GET", "/sessions", nil, &vs); c != 200 || ids(vs) != "aaa,bbb,ccc" {
		t.Fatalf("recent: %d %s", c, ids(vs))
	}
	call(t, ts, "GET", "/sessions?sort=oldest", nil, &vs)
	if ids(vs) != "ccc,bbb,aaa" {
		t.Errorf("oldest: %s", ids(vs))
	}
	call(t, ts, "GET", "/sessions?dir=/work/api", nil, &vs)
	if ids(vs) != "aaa,ccc" {
		t.Errorf("dir: %s", ids(vs))
	}
	call(t, ts, "GET", "/sessions?q=dashboard", nil, &vs)
	if ids(vs) != "bbb" {
		t.Errorf("q: %s", ids(vs))
	}
	call(t, ts, "GET", "/sessions?stale=1", nil, &vs)
	if ids(vs) != "ccc" || !vs[0].Stale {
		t.Errorf("stale: %s", ids(vs))
	}
	var e map[string]string
	if c := call(t, ts, "GET", "/sessions?sort=bogus", nil, &e); c != 400 {
		t.Errorf("bad sort status %d", c)
	}
}

func TestGetSessionView(t *testing.T) {
	ts, _, _ := setup(t)
	var v SessionView
	if c := call(t, ts, "GET", "/sessions/aaa", nil, &v); c != 200 {
		t.Fatalf("status %d", c)
	}
	if v.Label != "fix the login bug" || v.Prompts != 1 || v.Dir != "/work/api" || v.Tags == nil || !strings.Contains(v.Resume, "--resume") {
		t.Errorf("view = %+v", v)
	}
	if c := call(t, ts, "GET", "/sessions/nope", nil, &map[string]string{}); c != 404 {
		t.Errorf("missing session status %d", c)
	}
}

func TestPinAndTags(t *testing.T) {
	ts, _, _ := setup(t)
	var p map[string]bool
	call(t, ts, "POST", "/sessions/ccc/pin", nil, &p)
	if !p["pinned"] {
		t.Fatal("not pinned")
	}
	var vs []SessionView
	call(t, ts, "GET", "/sessions", nil, &vs)
	if vs[0].ID != "ccc" || !vs[0].Pinned {
		t.Errorf("pinned should lead: %s", ids(vs))
	}
	var tg map[string][]string
	call(t, ts, "PUT", "/sessions/aaa/tags", map[string]any{"tags": []string{"Infra", "#k8s"}}, &tg)
	if strings.Join(tg["tags"], ",") != "infra,k8s" {
		t.Errorf("tags = %v", tg)
	}
	call(t, ts, "GET", "/sessions?tag=%23k8s", nil, &vs)
	if ids(vs) != "aaa" {
		t.Errorf("tag filter: %s", ids(vs))
	}
	var tl []engine.TagCount
	call(t, ts, "GET", "/tags", nil, &tl)
	if len(tl) != 2 {
		t.Errorf("taglist = %v", tl)
	}
	call(t, ts, "PUT", "/sessions/aaa/tags", map[string]any{"tags": []string{}}, &tg)
	if len(tg["tags"]) != 0 {
		t.Errorf("tags not cleared: %v", tg)
	}
}

func TestDeleteUndo(t *testing.T) {
	ts, _, home := setup(t)
	var r map[string]any
	if c := call(t, ts, "POST", "/delete", map[string]any{"ids": []string{"ccc"}}, &r); c != 200 || r["moved"].(float64) != 1 {
		t.Fatalf("delete: %d %v", c, r)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "projects", "p", "ccc.jsonl")); err == nil {
		t.Fatal("file still there")
	}
	var vs []SessionView
	call(t, ts, "GET", "/sessions", nil, &vs)
	if len(vs) != 2 {
		t.Errorf("list after delete: %s", ids(vs))
	}
	if c := call(t, ts, "POST", "/delete", map[string]any{"ids": []string{"zzz"}}, &r); c != 404 {
		t.Errorf("unknown id status %d", c)
	}
	if c := call(t, ts, "POST", "/undo", nil, &r); c != 200 || r["restored"].(float64) != 1 {
		t.Fatalf("undo: %d %v", c, r)
	}
	call(t, ts, "GET", "/sessions", nil, &vs)
	if len(vs) != 3 {
		t.Errorf("list after undo: %s", ids(vs))
	}
	if c := call(t, ts, "POST", "/undo", nil, &r); c != 404 {
		t.Errorf("second undo status %d", c)
	}
}

func TestOpenRefusesGoneDir(t *testing.T) {
	ts, _, _ := setup(t) // /work/api does not exist on disk
	var r map[string]any
	if c := call(t, ts, "POST", "/open", map[string]any{"ids": []string{"aaa"}}, &r); c != 409 {
		t.Errorf("status %d %v", c, r)
	}
}

func TestAIEndpoints(t *testing.T) {
	ts, _, _ := setup(t)
	fakeClaude(t, `[{"n":1,"why":"matches login"}]`)
	var hits []engine.Hit
	if c := call(t, ts, "POST", "/ai/ask", map[string]string{"query": "login"}, &hits); c != 200 || len(hits) != 1 {
		t.Fatalf("ask: %d %v", c, hits)
	}
	if c := call(t, ts, "POST", "/ai/ask", map[string]string{"query": " "}, &map[string]string{}); c != 400 {
		t.Errorf("empty query status %d", c)
	}
	var deep []engine.Hit
	call(t, ts, "POST", "/ai/deep", map[string]string{"query": "dashboard"}, &deep)
	if len(deep) != 1 || deep[0].ID != "bbb" {
		t.Errorf("deep = %v", deep)
	}

	fakeClaude(t, "A short summary.")
	var sm map[string]string
	call(t, ts, "POST", "/ai/summarize", map[string]string{"id": "aaa"}, &sm)
	if sm["summary"] != "A short summary." {
		t.Errorf("summary = %v", sm)
	}
	var v SessionView
	call(t, ts, "GET", "/sessions/aaa", nil, &v)
	if v.Summary != "A short summary." {
		t.Errorf("summary not stored on session: %q", v.Summary)
	}

	fakeClaude(t, "Goal\n- ship it")
	var br map[string]any
	call(t, ts, "POST", "/ai/brief", map[string]any{"id": "aaa"}, &br)
	if br["cached"] != false || !strings.Contains(br["brief"].(string), "ship it") {
		t.Errorf("brief = %v", br)
	}
	call(t, ts, "POST", "/ai/brief", map[string]any{"id": "aaa"}, &br)
	if br["cached"] != true {
		t.Errorf("second brief should be cached: %v", br)
	}

	fakeClaude(t, `{"1":["login","auth"]}`)
	var tg map[string][]string
	call(t, ts, "POST", "/ai/autotag", map[string]any{"ids": []string{"aaa"}}, &tg)
	if strings.Join(tg["aaa"], ",") != "login,auth" {
		t.Errorf("autotag = %v", tg)
	}
}

func TestAIFailureIs502(t *testing.T) {
	ts, _, _ := setup(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\nexit 1\n"), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	if c := call(t, ts, "POST", "/ai/summarize", map[string]string{"id": "aaa"}, &map[string]string{}); c != 502 {
		t.Errorf("status %d", c)
	}
}

func TestEventsOnChange(t *testing.T) {
	ts, s, home := setup(t)
	resp, err := http.Get(ts.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rd := bufio.NewReader(resp.Body)
	rd.ReadString('\n') // ": connected"
	rd.ReadString('\n')

	got := make(chan string, 1)
	go func() {
		for {
			l, err := rd.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(l, "event: ") {
				got <- strings.TrimSpace(strings.TrimPrefix(l, "event: "))
				return
			}
		}
	}()
	fixture(t, home, "ddd", "/work/new", []string{"brand new"}, time.Second)
	s.Refresh(true)
	select {
	case k := <-got:
		if k != "sessions" {
			t.Errorf("event %q", k)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event after a new transcript appeared")
	}
	var vs []SessionView
	call(t, ts, "GET", "/sessions", nil, &vs)
	if len(vs) != 4 {
		t.Errorf("new session not listed: %s", ids(vs))
	}
}

func TestListenSocketPrivateAndExclusive(t *testing.T) {
	t.Chdir(t.TempDir()) // a relative path keeps us under the unix socket length limit
	path := filepath.Join("d", "ccsd.sock")
	ln, err := listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("socket mode %v", st.Mode().Perm())
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	if _, err := listen(path); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("second listen should refuse: %v", err)
	}
	// a stale socket file (no listener) is replaced
	ln.Close()
	if c, err := net.Dial("unix", path); err == nil {
		c.Close()
	}
	ln2, err := listen(path)
	if err != nil {
		t.Fatalf("stale socket not replaced: %v", err)
	}
	ln2.Close()
}

// The JSON key names are the contract with the Mac app: this test fails if one is renamed.
func TestJSONContractKeys(t *testing.T) {
	ts, s, _ := setup(t)
	s.mu.Lock()
	s.live = map[string]engine.LiveInfo{"aaa": {PID: 1, ID: "aaa", Status: "idle", Kind: "interactive", UpdatedAt: 5}}
	s.mu.Unlock()
	var raw []map[string]any
	call(t, ts, "GET", "/sessions", nil, &raw)
	for _, k := range []string{"id", "path", "cwd", "dir", "label", "title", "aiTitle", "first", "last", "pr", "summary", "excerpt",
		"prompts", "mtime", "started", "turns", "tags", "pinned", "preBoot", "stale", "dirGone", "repo", "fork", "forks", "stub"} {
		if _, ok := raw[0][k]; !ok {
			t.Errorf("session key %q missing", k)
		}
	}
	live := raw[0]["live"].(map[string]any)
	for _, k := range []string{"pid", "id", "cwd", "name", "status", "updatedAt", "kind"} {
		if _, ok := live[k]; !ok {
			t.Errorf("live key %q missing", k)
		}
	}
	turns := raw[0]["turns"].([]any)[0].(map[string]any)
	if _, ok := turns["user"]; !ok {
		t.Error("turn key user missing")
	}
	call(t, ts, "PUT", "/sessions/aaa/tags", map[string]any{"tags": []string{"x"}}, &map[string][]string{})
	var tl []map[string]any
	call(t, ts, "GET", "/tags", nil, &tl)
	if _, ok := tl[0]["name"]; !ok || tl[0]["n"] == nil {
		t.Errorf("tag keys: %v", tl[0])
	}
}
