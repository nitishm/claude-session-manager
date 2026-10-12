package main

import (
	"fmt"
	"strings"

	"ccsgo/engine"
)

const histRecent = 12

var histGlyph = map[string]string{
	engine.EvPrompt: "›", engine.EvCommit: "✓", engine.EvPush: "↑", engine.EvPR: "⇄", engine.EvCompact: "≈",
	engine.EvAway: "☾", engine.EvTitle: "✎", engine.EvAgent: "◇", engine.EvAsk: "?", engine.EvResume: "↻",
	engine.EvGap: "…", engine.EvSchedule: "⏲", engine.EvArtifact: "▣", engine.EvAPIError: "!", engine.EvInterrupt: "✗",
}

// historySection is the detail pane's HISTORY block: every prompt, commit, push, PR, compaction and pause, newest last.
// It shows the last few events and h reveals the rest.
func (m model) historySection(s *session, w int) string {
	if s.Path == "" || s.Prompts == 0 {
		return ""
	}
	ev := eng.Timeline(s).Events
	if len(ev) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n" + sHead.Render(fmt.Sprintf("TIMELINE · %d events", len(ev))) + "\n")
	shown := ev
	if !m.histAll && len(ev) > histRecent {
		shown = ev[len(ev)-histRecent:]
		b.WriteString(sDim.Render(fmt.Sprintf("… %d earlier · h shows all", len(ev)-histRecent)) + "\n")
	} else if m.histAll && len(ev) > histRecent {
		b.WriteString(sDim.Render("h shows only the latest") + "\n")
	}
	prevDay := ""
	for _, e := range shown {
		day := e.At.Format("Mon 02 Jan")
		if day != prevDay {
			b.WriteString(sDim.Render("── "+day) + "\n")
			prevDay = day
		}
		b.WriteString(eventLine(e, w) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func eventLine(e engine.Event, w int) string {
	g := histGlyph[e.Kind]
	if g == "" {
		g = "·"
	}
	head := sDim.Render(e.At.Format("15:04") + " ")
	text := e.Text
	switch e.Kind {
	case engine.EvPrompt:
		var st []string
		if e.Tools > 0 {
			st = append(st, fmt.Sprintf("%d tools", e.Tools))
		}
		if e.Edits > 0 {
			st = append(st, fmt.Sprintf("%d edits", e.Edits))
		}
		if e.Secs > 0 {
			st = append(st, secsText(e.Secs))
		}
		line := sAcc.Render(g) + " " + text
		out := head + line
		if len(st) > 0 {
			out += sDim.Render("  " + strings.Join(st, " · "))
		}
		if e.Errors > 0 {
			out += sWarn.Render(fmt.Sprintf("  %d failed", e.Errors))
		}
		return out
	case engine.EvCommit, engine.EvPush, engine.EvPR:
		if e.Detail != "" {
			text += " " + e.Detail
		}
		return head + sGood.Render(g) + " " + text
	case engine.EvInterrupt, engine.EvAPIError:
		if e.Detail != "" {
			text += " · " + e.Detail
		}
		return head + sWarn.Render(g+" "+text)
	}
	if e.Detail != "" {
		text += " · " + e.Detail
	}
	return head + sDim.Render(g+" "+text)
}

func secsText(n int) string {
	if n < 60 {
		return fmt.Sprintf("%ds", n)
	}
	return engine.Duration(n / 60)
}
