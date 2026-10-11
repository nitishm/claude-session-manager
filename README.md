# claude-session-manager (ccs)

Browse, search, resume and clean up Claude Code sessions.

## Layout

| Path | What |
|---|---|
| `go/engine/` | The backend. Pure Go, no UI imports. Sessions, live status, forks, tags, query, open/delete/undo, AI calls. |
| `go/` | Go TUI (Bubble Tea), a client of the engine. Keys, layout, styles, popups. |
| `ccs`, `ccs-tui` | Original Python versions. Kept for reference. |
| `tests/pty/` | End-to-end checks: run the real binary in a pty, read the screen with `pyte`. |

## Build and run

```sh
cd go && go build -o ../ccs-go . && ../ccs-go
```

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
