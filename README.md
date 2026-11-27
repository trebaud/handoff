# handoff

[![Go](https://img.shields.io/badge/go-1.21%2B-00ADD8?logo=go)](https://go.dev)
[![Release](https://img.shields.io/github/v/release/trebaud/handoff)](https://github.com/trebaud/handoff/releases)

An agent skill that takes one task to reviewed code in a fresh
[mori](https://github.com/trebaud/mori) worktree, built by a background agent, plus
`handoff-watch`, a small TUI of every handoff in flight.

## How it works

```
   spec file  ·  description  ·  task link (ClickUp, Linear, Jira, GitHub issue)
                    │
                    ▼
 ┌─ SESSION (stays free) ─────────────┐       ┌─ BACKGROUND AGENT (one per task) ──────────┐
 │                                    │       │                                            │
 │ setup      task + criteria,        │       │ starts with no context; limits:            │
 │            repos + branch          │       │ no questions, no push, no PR               │
 │                                    │       │                                            │
 │            spawn the agent         ├──────▶│ setup      one mori worktree per repo      │
 │                                    │       │ implement  code, tests, local commits      │
 │    free: more handoffs can start   │       │ review     lgtm: build, security,          │
 │                                    │       │            reuse, architecture, taste      │
 │            relay the report        │◀──────┤ report     diffs, checks, open points      │
 │    STOP    approval before PR      │       │                                            │
 │                                    │       │                                            │
 └──────────────────┬─────────────────┘       └──────────────────────┬─────────────────────┘
                    │               one event per step               │
                    └────────────────────────┬───────────────────────┘
                                             ▼
                             ~/.handoff/<branch>/events.jsonl  ──▶  handoff-watch
```

The session only reads the task, picks the repos and starts the agent, so further handoffs can
start while one runs. The agent never pushes and never opens a PR.

## Install

```bash
git clone https://github.com/trebaud/handoff.git && cd handoff && ./install.sh
```

The skill relies on skills from [trebaud/skills](https://github.com/trebaud/skills): `lgtm` and
the four audits it runs (`security-audit`, `dont-reinvent-the-wheel`, `architecture-audit`,
`taste`), plus the `mori` skill from [trebaud/mori](https://github.com/trebaud/mori). The
install script downloads and installs all of them with the `handoff` skill, then builds
`handoff-watch` into `~/.local/bin`. Skills already installed are left alone.

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
| `tab`, `enter` | Move the focus between the handoffs and the events |
| `/` | Filter by name, state or repo |
| `esc` | Back to the handoffs, then clear the filter |
| `d` | Archive the selected handoff, after `y` to confirm |
| `?` `q` | Help, quit |

Archiving moves the log to `~/.handoff/.archive/` and leaves the worktrees and the branch
alone; move the folder back to restore it. A running handoff cannot be archived, since its
agent would write the log again.

`NO_COLOR` is honored. The view needs a terminal of 60x12 or more.

## Event log

The skill and the TUI share only this format: one file per handoff,
`$HANDOFF_HOME/<branch>/events.jsonl` (default `~/.handoff`), one JSON object per line.

| Field | Value |
|---|---|
| `ts` | UTC, `2006-01-02T15:04:05.000Z` |
| `feature` | The branch, also the worktree name in every repo |
| `worktree_path`, `repo`, `commit` | `null` until the worktree exists |
| `step` | `setup`, `implement`, `review`, `report` |
| `status` | `done`, `failed`, `blocked`: logged when the step ends |
| `message` | One line |

```bash
go run skill/scripts/log_event.go my-branch setup done "agent started"
```
