package engine

import (
	"os"
	"path/filepath"
)

// WidenPath adds the usual install dirs. Launched from Finder, or from a terminal tab that the app opened
// through a non-login shell, a process gets a bare PATH that has neither claude (~/.local/bin) nor Homebrew tools.
func WidenPath(home string) {
	have := map[string]bool{}
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		have[d] = true
	}
	p := os.Getenv("PATH")
	for _, d := range []string{filepath.Join(home, ".local", "bin"), "/opt/homebrew/bin", "/usr/local/bin"} {
		if !have[d] {
			p = d + string(os.PathListSeparator) + p
		}
	}
	os.Setenv("PATH", p)
}
