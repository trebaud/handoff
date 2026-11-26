# handoff

[![Go](https://img.shields.io/badge/go-1.21%2B-00ADD8?logo=go)](https://go.dev)
[![Release](https://img.shields.io/github/v/release/trebaud/handoff)](https://github.com/trebaud/handoff/releases)

An agent skill that takes one task to reviewed code in a fresh
[mori](https://github.com/trebaud/mori) worktree, built by a background agent, plus
`handoff-watch`, a small TUI of every handoff in flight.

## Install

```bash
git clone https://github.com/trebaud/handoff.git && cd handoff && ./install.sh
```

This installs the skill, the skills it calls (`mori`, `lgtm` and the four audits `lgtm` runs)
and builds `handoff-watch` into `~/.local/bin`. Skills already installed are left alone.

```bash
./install.sh --check    # report what is missing, change nothing
./install.sh --no-tui   # skills only
```

The TUI alone:

```bash
go install github.com/trebaud/handoff/cmd/handoff-watch@latest
```

The skill needs `go`, `git` and `mori` on `PATH`. `SKILLS_HOME`, `BIN_DIR`, `SKILLS_REPO` and
`SKILLS_REF` override the installer defaults.

## Usage

In the agent, give the skill a spec file, a short description or a task link:

```
/handoff https://app.clickup.com/t/abc123
```

It builds and reviews the task in the background, then stops for approval before any PR.

```bash
handoff-watch                 # live view
handoff-watch -once           # one plain snapshot
handoff-watch -interval 5s    # time between two reads of the logs
```

Failed and blocked handoffs come first, then the running ones, then the done ones.

| Key | Action |
|---|---|
| `j` `k` `g` `G`, arrows, `PgUp` `PgDn` | Move |
| `tab`, `enter` | Focus the events of the selected handoff |
| `/` | Filter by name, state or repo |
| `esc` | Leave the events, then clear the filter |
| `?` `q` | Help, quit |

`NO_COLOR` is honored. The view needs a terminal of 60x12 or more.

## Event log

The skill and the TUI share only this format: one file per handoff,
`$HANDOFF_HOME/<branch>/events.jsonl` (default `~/.handoff`), one JSON object per line.

| Field | Value |
|---|---|
| `ts` | UTC, `2006-01-02T15:04:05.000Z` |
| `feature` | The branch, also the worktree name in every repo |
| `worktree_path`, `repo`, `commit` | `null` until the worktree exists |
| `actor` | `session`, `agent` |
| `step` | `read-task`, `pick-repos`, `spawn-agent`, `worktree`, `implement`, `review`, `report`, `relay` |
| `status` | `started`, `done`, `failed`, `blocked` |
| `message` | One line |
| `data` | Extra string pairs |

```bash
go run skill/scripts/log_event.go -actor session my-branch read-task done "read the spec"
```
