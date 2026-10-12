# claude-session-manager (ccs)

Browse, search, resume and clean up Claude Code sessions.

## Layout

| Path | What |
|---|---|
| `go/engine/` | The backend. Pure Go, no UI imports. Sessions, live status, forks, tags, query, open/delete/undo, AI calls. |
| `go/cmd/ccsd/` | Daemon: the engine over a unix socket (HTTP+JSON) for the Mac app. |
| `go/` | Go TUI (Bubble Tea), a client of the engine. Keys, layout, styles, popups. |
| `ccs`, `ccs-tui` | Original Python versions. Kept for reference. |
| `tests/pty/` | End-to-end checks: run the real binary in a pty, read the screen with `pyte`. |

## Build and run

```sh
cd go && go build -o ../ccs-go . && ../ccs-go
```

## ccsd

```sh
cd go && go build -o ../ccsd ./cmd/ccsd && ../ccsd          # socket: ~/.cache/ccs/ccsd.sock (mode 0600)
curl -s --unix-socket ~/.cache/ccs/ccsd.sock 'http://x/sessions?q=login&sort=recent'
```

| Route | What |
|---|---|
| `GET /sessions` | `q dir tag sort rev stale pre live forks` filters; pinned first |
| `GET /sessions/{id}`, `/live`, `/tags`, `/health` | detail, running sessions, tag counts |
| `POST /sessions/{id}/pin`, `PUT /sessions/{id}/tags` | pin toggle, replace tags |
| `POST /delete`, `/undo`, `/open` | trash, restore, open in terminal |
| `POST /ai/{summarize,brief,ask,deep,autotag}` | Claude-backed; `deep` is a plain transcript scan |
| `GET /events` | server-sent events: `sessions` or `live` means refetch |

The JSON keys are the contract with the Mac app (`TestJSONContractKeys` pins them). Add fields, never rename.

## Tests

```sh
cd go && go test ./engine   # engine unit tests, temp home
cd .. && CCS_BIN=$PWD/ccs-go uv run --with pyte python tests/pty/new_test.py
```

- Most tests use a temp `$HOME` with fake transcripts and a fake `claude`.
- `fork_view.py`, `forkfilter_view.py`, `legend_view.py` and `ask_test.py` read your real sessions.

## Design rules

- **No process launches in hot paths.** Endpoint security tools scan every exec.
- **Read-only on Claude's files.** Delete moves files to `~/.Trash/claude-sessions`, and `Z` undoes it.
- **`claude -p` calls strip `ANTHROPIC_API_KEY`.** They use the login instead.

## Keys

Press `?` in the app for the full list and legend.

## Mac app (`app/`)

SwiftUI front end over `ccsd`. macOS 14+, Mac only.

```sh
cd app && ./build.sh        # build/CCS.app, ccsd bundled inside
./build.sh run              # build, then open
```

- Plain `swiftc`, no Xcode. SwiftPM and the `@State` macro need Xcode, so UI state lives in an `@Observable` `Store`.
- The app starts `ccsd` if none is running and stops it on quit only if it started it.
- Talks to `~/.cache/ccs/ccsd.sock` with a small POSIX-socket HTTP client (`URLSession` has no unix sockets).
