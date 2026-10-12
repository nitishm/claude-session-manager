package main

import (
	"fmt"
	"os"
	"strings"

	"ccsgo/engine"
)

// factsSection is the detail pane's ACTIVITY, FILES and COMMANDS blocks. Everything here comes from the transcript.
func factsSection(s *session, w int) string {
	if s.Path == "" || s.Prompts == 0 {
		return ""
	}
	f := eng.Facts(s)
	if f.Turns == 0 && len(f.Files) == 0 {
		return ""
	}
	var b strings.Builder
	head := func(name string) { b.WriteString("\n" + sHead.Render(name) + "\n") }

	head("ACTIVITY")
	parts := []string{fmt.Sprintf("%d turns", f.Turns)}
	if f.ActiveMin > 0 {
		parts = append(parts, engine.Duration(f.ActiveMin)+" active")
	}
	parts = append(parts, engine.Compact(f.In+f.CacheRead+f.CacheWrite)+" in", engine.Compact(f.Out)+" out")
	if h := f.CacheHit(); h > 0 {
		parts = append(parts, fmt.Sprintf("%d%% cached", h))
	}
	b.WriteString(strings.Join(parts, " · ") + "\n")
	if f.Model != "" {
		b.WriteString(sDim.Render(strings.TrimPrefix(f.Model, "claude-")) + "\n")
	}

	if len(f.Files) > 0 {
		head("FILES TOUCHED " + sDim.Render("· click to open"))
		for _, t := range f.Files {
			mark, n := sDim.Render("◦ read"), ""
			if t.Edits > 0 {
				mark = sGood.Render("✎ edit")
				n = fmt.Sprintf(" ×%d", t.Edits)
			}
			b.WriteString(mark + sDim.Render(n) + "  " + sLink.Render(displayPath(t.Rel, w-12-len(n))) + "\n")
		}
	}
	if len(f.Commands) > 0 {
		head(fmt.Sprintf("COMMANDS %s", sDim.Render(fmt.Sprintf("· last %d of %d", len(f.Commands), f.CommandCount))))
		for _, c := range f.Commands {
			b.WriteString(sDim.Render("$ ") + c + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// displayPath keeps the tail of a long path, which is the useful part; a leading … marks the cut.
func displayPath(p string, max int) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home+"/") {
		p = "~" + strings.TrimPrefix(p, home)
	}
	r := []rune(p)
	if max < 8 || len(r) <= max {
		return p
	}
	return "…" + string(r[len(r)-max+1:])
}
