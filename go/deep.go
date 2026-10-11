package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// Deep search: an on-demand scan of every transcript's user and assistant text.
// It is a special mode (D), never part of the default `/` search, because it reads every file.

const deepMaxHits = 50

type deepHit struct {
	s    *session
	hits int
	snip string
}

func snippet(text string, at, w int) string {
	lo, hi := max(0, at-w/3), min(len(text), at+2*w/3)
	for lo > 0 && lo < len(text) && text[lo]&0xC0 == 0x80 { // do not cut a multi-byte rune
		lo--
	}
	for hi < len(text) && text[hi]&0xC0 == 0x80 {
		hi++
	}
	s := clean(text[lo:hi], w)
	if lo > 0 {
		s = "…" + s
	}
	if hi < len(text) {
		s += "…"
	}
	return s
}

// deepScan reports how often each word occurs in the session text and a snippet around the first hit.
func deepScan(path string, words []string) (counts []int, snip string) {
	counts = make([]int, len(words))
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	rd := bufio.NewReaderSize(f, 64<<10)
	for {
		line, err := rd.ReadBytes('\n')
		if len(line) > 2 && wanted(line) {
			var d rawLine
			if json.Unmarshal(line, &d) == nil && !d.IsSidechain && !d.IsMeta && (d.Type == "user" || d.Type == "assistant") {
				txt := textOf(d.Message.Content)
				if d.Type == "user" {
					txt = unwrapCmd(txt)
				}
				if txt != "" {
					low := strings.ToLower(txt)
					for i, w := range words {
						if n := strings.Count(low, w); n > 0 {
							counts[i] += n
							if snip == "" {
								if at := strings.Index(low, w); len(low) == len(txt) {
									snip = snippet(txt, at, 110)
								} else {
									snip = clean(txt, 110) // lowercasing changed byte lengths, offsets do not map back
								}
							}
						}
					}
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func deepSearch(query string, all []*session) tea.Cmd {
	words := strings.Fields(strings.ToLower(query))
	return func() tea.Msg {
		var ss []*session
		for _, s := range all {
			if s.Path != "" {
				ss = append(ss, s)
			}
		}
		var mu sync.Mutex
		var found []deepHit
		jobs := make(chan *session)
		var wg sync.WaitGroup
		for w := 0; w < 4; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for s := range jobs {
					counts, snip := deepScan(s.Path, words)
					total, ok := 0, true
					for _, c := range counts {
						if c == 0 {
							ok = false
						}
						total += c
					}
					if ok {
						mu.Lock()
						found = append(found, deepHit{s: s, hits: total, snip: snip})
						mu.Unlock()
					}
				}
			}()
		}
		for _, s := range ss {
			jobs <- s
		}
		close(jobs)
		wg.Wait()
		sort.Slice(found, func(i, j int) bool {
			if found[i].hits != found[j].hits {
				return found[i].hits > found[j].hits
			}
			return found[i].s.MTime.After(found[j].s.MTime)
		})
		if len(found) > deepMaxHits {
			found = found[:deepMaxHits]
		}
		var hits []askHit
		for _, h := range found {
			hits = append(hits, askHit{h.s.ID, fmt.Sprintf("%d hits · %s", h.hits, h.snip)})
		}
		return askMsg{query: query, hits: hits, kind: "deep"}
	}
}
