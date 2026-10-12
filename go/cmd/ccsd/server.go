package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"ccsgo/engine"
)

// Server holds the session list and serves it over HTTP+JSON. The list is refreshed in the
// background (live status every 5s, transcripts every 30s) and clients are told through /events.
type Server struct {
	eng  *engine.Engine
	boot time.Time

	mu   sync.Mutex // guards everything below; engine queries mutate cached search text
	all  []*engine.Session
	live map[string]engine.LiveInfo
	pre  map[string]bool

	subMu sync.Mutex
	subs  map[chan string]struct{}
}

// NewServer loads every session once and returns a ready server. Call Run to start refreshing.
func NewServer(e *engine.Engine) *Server {
	s := &Server{eng: e, boot: engine.BootTime(), subs: map[chan string]struct{}{}}
	s.all = e.Load()
	s.pre = e.PreBoot(s.all, s.boot)
	s.live, _ = e.ReadPids()
	s.all, _ = e.SyncStubs(s.all, s.live)
	return s
}

// Run refreshes until stop is closed. It starts no processes: file reads and syscalls only.
func (s *Server) Run(stop <-chan struct{}) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for n := 1; ; n++ {
		select {
		case <-stop:
			return
		case <-t.C:
			s.Refresh(n%6 == 0)
		}
	}
}

// Refresh updates live status, and rescans transcripts when full is true. It reports changes to subscribers.
func (s *Server) Refresh(full bool) {
	live := s.eng.LivePids(s.boot)
	s.mu.Lock()
	prev := s.all
	liveChanged := !sameLive(s.live, live)
	s.live = live
	s.mu.Unlock()

	all, changed := prev, false
	if full {
		all, changed = s.eng.Reload(prev) // off the lock: it reads files
	}
	s.mu.Lock()
	all, stubs := s.eng.SyncStubs(all, live)
	if changed || stubs {
		s.eng.LinkForks(all)
	}
	s.all = all
	s.mu.Unlock()

	if changed || stubs {
		s.publish("sessions")
	} else if liveChanged {
		s.publish("live")
	}
}

func sameLive(a, b map[string]engine.LiveInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func (s *Server) publish(kind string) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- kind:
		default: // a slow client misses one nudge; the next one still tells it to refetch
		}
	}
}

// ───────────────────────── JSON view ─────────────────────────

// SessionView is the API shape of a session. It is the contract with the Mac app, so it is
// decoupled from engine.Session: add fields freely, rename or remove none.
type SessionView struct {
	ID      string           `json:"id"`
	Path    string           `json:"path"`
	Cwd     string           `json:"cwd"`
	Dir     string           `json:"dir"`   // cwd with ~ for home
	Label   string           `json:"label"` // best display name
	Title   string           `json:"title"`
	AITitle string           `json:"aiTitle"`
	First   string           `json:"first"`
	Last    string           `json:"last"`
	PR      string           `json:"pr"`
	Summary string           `json:"summary"`
	Excerpt string           `json:"excerpt"`
	Prompts int              `json:"prompts"`
	MTime   time.Time        `json:"mtime"`
	Started time.Time        `json:"started"`
	Turns   []engine.Turn    `json:"turns"`
	Tags    []string         `json:"tags"`
	Pinned  bool             `json:"pinned"`
	Live    *engine.LiveInfo `json:"live,omitempty"`
	PreBoot bool             `json:"preBoot"`
	Stale   bool             `json:"stale"`
	DirGone bool             `json:"dirGone"`
	Repo    string           `json:"repo"`
	Fork    bool             `json:"fork"`
	ForkOf  string           `json:"forkOf,omitempty"`
	Forks   int              `json:"forks"`
	Stub    bool             `json:"stub"`             // running but no transcript yet
	Resume  string           `json:"resume,omitempty"` // only on GET /sessions/{id}
}

// view must be called with s.mu held.
func (s *Server) view(x *engine.Session) SessionView {
	v := SessionView{
		ID: x.ID, Path: x.Path, Cwd: x.Cwd, Dir: s.eng.Short(x.Cwd), Label: x.Label(), Title: x.Title,
		AITitle: x.AI, First: x.First, Last: x.Last, PR: x.PR, Summary: x.Summary, Excerpt: x.Excerpt(),
		Prompts: x.Prompts, MTime: x.MTime, Started: x.Started, Turns: x.Turns,
		Tags: s.eng.Tags(x.ID), Pinned: s.eng.Pinned(x.ID), PreBoot: s.pre[x.ID],
		Stale: s.eng.IsStale(x, s.live, s.pre), DirGone: s.eng.DirGone(x),
		Repo: s.eng.RepoOf(x.Cwd).String(), Fork: x.Fork, Forks: x.Forks, Stub: x.Path == "",
	}
	if v.Tags == nil {
		v.Tags = []string{}
	}
	if li, ok := s.live[x.ID]; ok {
		v.Live = &li
	}
	if x.ForkOf != nil {
		v.ForkOf = x.ForkOf.ID
	}
	return v
}

func (s *Server) byID(id string) *engine.Session {
	for _, x := range s.all {
		if x.ID == id {
			return x
		}
	}
	return nil
}

// ───────────────────────── HTTP ─────────────────────────

// Handler returns the API. Every route is JSON; errors are {"error": "..."} with a status code.
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		reply(w, 200, map[string]any{"ok": true, "sessions": len(s.all), "live": len(s.live)})
	})
	m.HandleFunc("GET /sessions", s.listSessions)
	m.HandleFunc("GET /sessions/{id}", s.getSession)
	m.HandleFunc("GET /live", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		reply(w, 200, s.live)
	})
	m.HandleFunc("GET /tags", func(w http.ResponseWriter, r *http.Request) {
		tl := s.eng.TagList()
		if tl == nil {
			tl = []engine.TagCount{}
		}
		reply(w, 200, tl)
	})
	m.HandleFunc("POST /sessions/{id}/pin", s.pin)
	m.HandleFunc("PUT /sessions/{id}/tags", s.setTags)
	m.HandleFunc("POST /delete", s.del)
	m.HandleFunc("POST /undo", s.undo)
	m.HandleFunc("POST /open", s.open)
	m.HandleFunc("POST /ai/summarize", s.aiSummarize)
	m.HandleFunc("POST /ai/brief", s.aiBrief)
	m.HandleFunc("POST /ai/ask", s.aiAsk)
	m.HandleFunc("POST /ai/deep", s.aiDeep)
	m.HandleFunc("POST /ai/autotag", s.aiAutoTag)
	m.HandleFunc("GET /events", s.events)
	return m
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	reply(w, code, map[string]string{"error": msg})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		fail(w, 400, "bad JSON: "+err.Error())
		return false
	}
	return true
}

func on(r *http.Request, k string) bool {
	v := r.URL.Query().Get(k)
	return v == "1" || v == "true"
}

// GET /sessions?q=&dir=&tag=&sort=&rev=1&stale=1&pre=1&live=1&forks=1
func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.mu.Lock()
	defer s.mu.Unlock()
	o := engine.Opts{
		Text: q.Get("q"), Dir: q.Get("dir"), Tag: strings.TrimPrefix(q.Get("tag"), "#"),
		Stale: on(r, "stale"), PreOnly: on(r, "pre"), Pre: s.pre,
		LiveOnly: on(r, "live"), Live: s.live, ForkOnly: on(r, "forks"),
		Sort: q.Get("sort"), Reverse: on(r, "rev"),
	}
	if o.Sort == "" {
		o.Sort = s.eng.Sort()
	}
	if o.Sort != "" && !validSort(o.Sort) {
		fail(w, 400, "unknown sort "+o.Sort)
		return
	}
	rows := s.eng.Query(s.all, o)
	out := make([]SessionView, 0, len(rows))
	for _, x := range rows {
		out = append(out, s.view(x))
	}
	reply(w, 200, out)
}

func validSort(n string) bool {
	for _, v := range engine.Sorts {
		if v == n {
			return true
		}
	}
	return false
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x := s.byID(r.PathValue("id"))
	if x == nil {
		fail(w, 404, "no such session")
		return
	}
	v := s.view(x)
	v.Resume = engine.ResumeCmd("claude", x, false)
	reply(w, 200, v)
}

func (s *Server) pin(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := r.PathValue("id")
	if s.byID(id) == nil {
		fail(w, 404, "no such session")
		return
	}
	reply(w, 200, map[string]bool{"pinned": s.eng.TogglePin(id)})
}

func (s *Server) setTags(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Tags []string `json:"tags"`
	}
	if !decode(w, r, &b) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := r.PathValue("id")
	if s.byID(id) == nil {
		fail(w, 404, "no such session")
		return
	}
	tags := engine.ParseTags(strings.Join(b.Tags, " "))
	s.eng.SetTags(id, tags)
	if tags == nil {
		tags = []string{}
	}
	reply(w, 200, map[string][]string{"tags": tags})
}

// pick returns the sessions for ids, or an error message naming the first unknown id.
// Callers hold s.mu.
func (s *Server) pick(ids []string) ([]*engine.Session, string) {
	var out []*engine.Session
	for _, id := range ids {
		x := s.byID(id)
		if x == nil {
			return nil, "no such session: " + id
		}
		out = append(out, x)
	}
	if len(out) == 0 {
		return nil, "no ids given"
	}
	return out, ""
}

func (s *Server) del(w http.ResponseWriter, r *http.Request) {
	var b struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &b) {
		return
	}
	s.mu.Lock()
	ss, msg := s.pick(b.IDs)
	if msg != "" {
		s.mu.Unlock()
		fail(w, 404, msg)
		return
	}
	res, err := s.eng.Delete(ss, s.live)
	kept := s.all[:0:0]
	for _, x := range s.all {
		if !res.Gone[x.ID] {
			kept = append(kept, x)
		}
	}
	s.all = kept
	s.eng.LinkForks(s.all)
	s.mu.Unlock()
	s.publish("sessions")
	out := map[string]any{"moved": res.Moved, "skipped": res.Skipped}
	if err != nil {
		out["error"] = err.Error()
	}
	reply(w, 200, out)
}

func (s *Server) undo(w http.ResponseWriter, r *http.Request) {
	n, err := s.eng.Undo()
	if n == 0 {
		fail(w, 404, "nothing to undo")
		return
	}
	s.mu.Lock()
	s.all = s.eng.Load()
	s.all, _ = s.eng.SyncStubs(s.all, s.live)
	s.mu.Unlock()
	s.publish("sessions")
	out := map[string]any{"restored": n}
	if err != nil {
		out["error"] = err.Error()
	}
	reply(w, 200, out)
}

func (s *Server) open(w http.ResponseWriter, r *http.Request) {
	var b struct {
		IDs  []string `json:"ids"`
		Tab  bool     `json:"tab"`
		Fork bool     `json:"fork"`
	}
	if !decode(w, r, &b) {
		return
	}
	s.mu.Lock()
	ss, msg := s.pick(b.IDs)
	var ok []*engine.Session
	skipped := 0
	for _, x := range ss {
		if s.eng.DirGone(x) || x.Path == "" {
			skipped++
		} else {
			ok = append(ok, x)
		}
	}
	s.mu.Unlock()
	if msg != "" {
		fail(w, 404, msg)
		return
	}
	if len(ok) == 0 {
		fail(w, 409, "nothing to open: directory gone or no transcript yet")
		return
	}
	n, err := s.eng.Open(ok, b.Tab, b.Fork)
	out := map[string]any{"opened": n, "skipped": skipped}
	if err != nil {
		out["error"] = err.Error()
	}
	reply(w, 200, out)
}

// ───────────────────────── AI ─────────────────────────

func (s *Server) aiSummarize(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &b) {
		return
	}
	s.mu.Lock()
	x := s.byID(b.ID)
	s.mu.Unlock()
	if x == nil || x.Path == "" {
		fail(w, 404, "no such session")
		return
	}
	txt, err := s.eng.Summarize(x)
	if err != nil {
		fail(w, 502, "summary failed: "+err.Error())
		return
	}
	s.mu.Lock()
	s.eng.SetSummary(x, txt)
	s.mu.Unlock()
	reply(w, 200, map[string]string{"summary": txt})
}

func (s *Server) aiBrief(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ID    string `json:"id"`
		Force bool   `json:"force"`
	}
	if !decode(w, r, &b) {
		return
	}
	s.mu.Lock()
	x := s.byID(b.ID)
	s.mu.Unlock()
	if x == nil || x.Path == "" {
		fail(w, 404, "no such session")
		return
	}
	if !b.Force {
		if t, ok := s.eng.BriefCached(x); ok {
			reply(w, 200, map[string]any{"brief": t, "cached": true})
			return
		}
	}
	txt, err := s.eng.Brief(x)
	if err != nil {
		fail(w, 502, "brief failed: "+err.Error())
		return
	}
	reply(w, 200, map[string]any{"brief": txt, "cached": false})
}

func (s *Server) aiAsk(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Query string `json:"query"`
	}
	if !decode(w, r, &b) || !needQuery(w, b.Query) {
		return
	}
	s.mu.Lock()
	snap := append([]*engine.Session(nil), s.all...)
	s.mu.Unlock()
	hits, err := s.eng.Ask(b.Query, snap)
	if err != nil {
		fail(w, 502, "ask failed: "+err.Error())
		return
	}
	reply(w, 200, nonNil(hits))
}

func (s *Server) aiDeep(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Query string `json:"query"`
	}
	if !decode(w, r, &b) || !needQuery(w, b.Query) {
		return
	}
	s.mu.Lock()
	snap := append([]*engine.Session(nil), s.all...)
	s.mu.Unlock()
	reply(w, 200, nonNil(s.eng.DeepSearch(b.Query, snap)))
}

func (s *Server) aiAutoTag(w http.ResponseWriter, r *http.Request) {
	var b struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &b) {
		return
	}
	s.mu.Lock()
	ss, msg := s.pick(b.IDs)
	s.mu.Unlock()
	if msg != "" {
		fail(w, 404, msg)
		return
	}
	if len(ss) > engine.AutoTagMax {
		fail(w, 400, fmt.Sprintf("at most %d sessions per call", engine.AutoTagMax))
		return
	}
	res, err := s.eng.AutoTag(ss)
	if err != nil {
		fail(w, 502, "auto-tag failed: "+err.Error())
		return
	}
	out := map[string][]string{}
	for id, tags := range res {
		out[id] = s.eng.AddTags(id, tags)
	}
	reply(w, 200, out)
}

func needQuery(w http.ResponseWriter, q string) bool {
	if strings.TrimSpace(q) == "" {
		fail(w, 400, "empty query")
		return false
	}
	return true
}

func nonNil(h []engine.Hit) []engine.Hit {
	if h == nil {
		return []engine.Hit{}
	}
	return h
}

// ───────────────────────── events ─────────────────────────

// GET /events is a server-sent event stream. "sessions" means the list changed, "live" means only
// running status changed. Clients refetch; the event carries no data.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		fail(w, 500, "streaming unsupported")
		return
	}
	ch := make(chan string, 8)
	s.subMu.Lock()
	s.subs[ch] = struct{}{}
	s.subMu.Unlock()
	defer func() {
		s.subMu.Lock()
		delete(s.subs, ch)
		s.subMu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprint(w, ": connected\n\n")
	fl.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case k := <-ch:
			fmt.Fprintf(w, "event: %s\ndata: {}\n\n", k)
			fl.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}
