package engine

import (
	"context"
	"sync"
	"time"
)

// PRInfo is one linked pull request with its live state from gh. State is nil when gh could not read it.
type PRInfo struct {
	Number int       `json:"number"`
	URL    string    `json:"url"`
	At     time.Time `json:"at"`
	PR     *PRState  `json:"pr,omitempty"`
	Err    string    `json:"err,omitempty"`
}

type prEntry struct {
	st  *PRState
	err string
	at  time.Time
}

const (
	prOpenTTL  = 45 * time.Second
	prDoneTTL  = 24 * time.Hour // a merged or closed PR does not change again
	prErrTTL   = 30 * time.Second
	prParallel = 4
)

func prFresh(c prEntry) bool {
	switch {
	case c.st == nil:
		return time.Since(c.at) < prErrTTL
	case c.st.State == "OPEN":
		return time.Since(c.at) < prOpenTTL
	}
	return time.Since(c.at) < prDoneTTL
}

// PRs returns every PR the session linked, newest first, each with its live state. gh runs once per PR, a few at a time.
func (e *Engine) PRs(s *Session) []PRInfo {
	links := e.Timeline(s).PRs
	out := make([]PRInfo, len(links))
	sem := make(chan struct{}, prParallel)
	var wg sync.WaitGroup
	for i, l := range links {
		j := len(links) - 1 - i // newest first
		out[j] = PRInfo{Number: l.Number, URL: l.URL, At: l.At}
		if l.URL == "" {
			continue
		}
		wg.Add(1)
		go func(j int, url string) {
			defer wg.Done()
			e.mu.Lock()
			c, ok := e.prCache[url]
			e.mu.Unlock()
			if !ok || !prFresh(c) {
				sem <- struct{}{}
				ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
				st, err := readPR(ctx, s.Cwd, url)
				cancel()
				<-sem
				c = prEntry{st: st, at: time.Now()}
				if err != nil {
					c.err = firstLine(err.Error())
				}
				e.mu.Lock()
				if e.prCache == nil {
					e.prCache = map[string]prEntry{}
				}
				e.prCache[url] = c
				e.mu.Unlock()
			}
			out[j].PR, out[j].Err = c.st, c.err
		}(j, l.URL)
	}
	wg.Wait()
	return out
}
