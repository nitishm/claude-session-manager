// ccsd serves the ccs engine over a unix socket (HTTP+JSON) for clients such as the Mac app.
//
//	ccsd [--home DIR] [--sock PATH]
//
// The socket is mode 0600 in ~/.cache/ccs, so only the current user can reach it.
package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"ccsgo/engine"
)

// widenPath adds the usual install dirs. Launched from Finder, the daemon gets launchd's bare PATH,
// which has neither claude (~/.local/bin) nor Homebrew tools.
func widenPath(home string) {
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

func main() {
	home := flag.String("home", os.Getenv("HOME"), "home directory holding .claude and .cache/ccs")
	sock := flag.String("sock", "", "socket path (default HOME/.cache/ccs/ccsd.sock)")
	flag.Parse()
	widenPath(*home)
	if *home == "" {
		fmt.Fprintln(os.Stderr, "ccsd: no home directory")
		os.Exit(1)
	}
	e := engine.New(*home)
	path := *sock
	if path == "" {
		path = filepath.Join(e.CacheDir, "ccsd.sock")
	}
	ln, err := listen(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccsd:", err)
		os.Exit(1)
	}
	srv := NewServer(e)
	stop := make(chan struct{})
	go srv.Run(stop)

	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		close(stop)
		hs.Close()
	}()
	fmt.Fprintln(os.Stderr, "ccsd: listening on", path)
	if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, "ccsd:", err)
	}
	os.Remove(path)
}

// listen binds the socket. A leftover socket file from a crash is removed, but a live daemon is not displaced.
func listen(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
		c.Close()
		return nil, fmt.Errorf("already running on %s", path)
	}
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}
