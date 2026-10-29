package main

import (
	"fmt"
	"os/exec"
	"strings"

	"ccsgo/engine"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// artTab is the Artifacts mode (P): every page Claude published, across all sessions.
type artTab struct {
	on     bool
	rows   []engine.ArtifactRow
	cur    int
	top    int
	in     textinput.Model
	loaded bool
	deep   bool // also search inside the pages
}

func (m model) artSearch() []engine.ArtifactRow {
	return eng.SearchArtifacts(m.all, m.art.in.Value(), m.art.deep)
}

// artMsg carries the freshly scanned artifacts.
type artMsg struct{ rows []engine.ArtifactRow }

func newArtInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = " ⌕ "
	ti.PromptStyle = sAcc
	ti.Placeholder = "press / to search titles, links, session names"
	ti.PlaceholderStyle = sDim
	return ti
}

// loadArtifacts scans off the UI goroutine: the first scan reads every transcript.
func loadArtifacts(all []*session, q string) tea.Cmd {
	return func() tea.Msg { return artMsg{eng.Artifacts(all, q)} }
}

func (m model) artSelected() *engine.ArtifactRow {
	if m.art.cur < 0 || m.art.cur >= len(m.art.rows) {
		return nil
	}
	return &m.art.rows[m.art.cur]
}

// artKey handles every key while the Artifacts mode is on.
func (m model) artKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	a := &m.art
	if a.in.Focused() {
		switch msg.String() {
		case "enter":
			a.in.Blur()
			return m, nil
		case "esc":
			a.in.SetValue("")
			a.in.Blur()
			a.rows = m.artSearch()
			a.cur, a.top = 0, 0
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		}
		var c tea.Cmd
		a.in, c = a.in.Update(msg)
		a.rows = m.artSearch() // warm after the first scan: cheap
		a.cur, a.top = 0, 0
		return m, c
	}
	step := max(1, (m.h-5)/2)
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "P":
		if a.in.Value() != "" {
			a.in.SetValue("")
			a.rows = m.artSearch()
			a.cur, a.top = 0, 0
			return m, nil
		}
		a.on = false
	case "/":
		a.in.Focus()
	case "D": // search inside the pages too
		a.deep = !a.deep
		a.rows = m.artSearch()
		a.cur, a.top = 0, 0
		if a.deep {
			return m, m.flash("deep: searching inside the pages")
		}
		return m, m.flash("deep off: titles, links and session names")
	case "up", "k", "ctrl+p":
		a.cur = max(0, a.cur-1)
	case "down", "j", "ctrl+n":
		a.cur = min(max(0, len(a.rows)-1), a.cur+1)
	case "pgup", "ctrl+u":
		a.cur = max(0, a.cur-step)
	case "pgdown", "ctrl+d":
		a.cur = min(max(0, len(a.rows)-1), a.cur+step)
	case "home", "g":
		a.cur = 0
	case "end", "G":
		a.cur = max(0, len(a.rows)-1)
	case "enter", "o":
		if r := m.artSelected(); r != nil {
			if err := exec.Command("open", r.URL).Start(); err != nil {
				return m, m.flash("could not open: " + err.Error())
			}
			return m, m.flash("opened in the browser")
		}
	case "y":
		if r := m.artSelected(); r != nil {
			c := exec.Command("pbcopy")
			c.Stdin = strings.NewReader(r.URL)
			if c.Run() == nil {
				return m, m.flash("copied the artifact link")
			}
		}
	case "f":
		if r := m.artSelected(); r != nil {
			if r.File == "" {
				return m, m.flash("the source file is gone")
			}
			exec.Command("open", "-R", r.File).Start()
		}
	case "s": // jump to the owning session in the list
		if r := m.artSelected(); r != nil {
			id := r.SessionID
			if !m.selectID(id) {
				return m, m.flash("that session is not in the list: press r to reload")
			}
			a.on = false
		}
	}
	return m, nil
}

// artView draws the whole screen for the Artifacts mode.
func (m model) artView() string {
	a := m.art
	head := sDim.Render("◆ Claude sessions  ") + sTitle.Render("▸ Artifacts") + sDim.Render(fmt.Sprintf("  %d", len(a.rows)))
	if !a.loaded {
		head += sAcc.Render("  reading transcripts…")
	}
	if a.deep {
		head += sAcc.Render("  ⌕ deep: inside pages")
	}
	box := a.in.View()
	listH := max(2, m.h-5)
	per := 2 // each artifact takes two lines
	fit := max(1, listH/per)
	if a.cur < a.top {
		a.top = a.cur
	}
	if a.cur >= a.top+fit {
		a.top = a.cur - fit + 1
	}
	titleW := max(10, m.w-10)
	var lines []string
	for i := a.top; i < min(len(a.rows), a.top+fit); i++ {
		r := a.rows[i]
		file := ""
		if r.File != "" {
			file = " · ▤ source"
		}
		l1 := fmt.Sprintf(" %s  %s", sDim.Render(pad(engine.Ago(r.At), 4)), pad(r.Title, titleW))
		l2 := "       " + sDim.Render(pad("from "+r.Label+"  ·  "+eng.ShortDir(r.Cwd)+file, titleW))
		if r.Excerpt != "" {
			l2 = "       " + sWarn.Render(pad("⌕ "+r.Excerpt, titleW))
		}
		if i == a.cur {
			l1, l2 = sSel.Render(pad(l1, m.w)), sSel.Render(pad(l2, m.w))
		}
		lines = append(lines, l1, l2)
	}
	if len(a.rows) == 0 && a.loaded {
		lines = append(lines, sDim.Render("  no artifacts match"))
	}
	for len(lines) < listH {
		lines = append(lines, "")
	}
	bar := ""
	if m.msg != "" {
		bar = " " + sWarn.Render(m.msg)
	}
	k := func(key, desc string) string { return sAcc.Render(key) + sDim.Render(" "+desc) }
	help := " " + strings.Join([]string{k("enter", "open"), k("y", "copy link"), k("f", "reveal source"), k("s", "go to session"), k("/", "search"), k("D", "deep"), k("esc", "back")}, "  ")
	return strings.Join([]string{head, box, strings.Join(lines[:listH], "\n"), bar, truncLines(help, m.w)}, "\n")
}
