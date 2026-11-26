# handoff

An agent skill that takes one task (spec file, short description or task link) to reviewed code
in a fresh [mori](https://github.com/trebaud/mori) worktree, built by a background agent, and a
terminal view of every handoff in flight.

| Part | Path | Role |
|---|---|---|
| Skill | `skill/` | `SKILL.md` and `scripts/log_event.go`, the event logger the skill calls |
| TUI | `cmd/handoff-watch/` | Standalone viewer of the event logs |
| Installer | `install.sh` | Installs the skill, its supporting skills and the TUI |

The skill and the TUI share one thing: the event log format below. The skill never calls the
TUI and the TUI needs no skill installed.

## Install

```bash
./install.sh            # install what is missing
./install.sh --check    # report only, exit 1 when a skill is missing
./install.sh --no-tui   # skip the handoff-watch build
```

The installer:

1. Checks each supporting skill in `~/.agents/skills`, `~/.claude/skills` and
   `~/.opencode/skills`. A skill found in any of them is left alone.
2. Downloads the missing ones from GitHub into `~/.agents/skills` and links them into the agent
   folders that exist.
3. Copies `skill/` to `~/.agents/skills/handoff`, replacing an earlier copy, and links it.
4. Builds `handoff-watch` into `~/.local/bin`.

| Supporting skill | Source | Called by |
|---|---|---|
| `mori` | `trebaud/mori`, `skill/` | handoff |
| `lgtm` | `trebaud/skills` | handoff |
| `security-audit`, `dont-reinvent-the-wheel`, `architecture-audit`, `taste` | `trebaud/skills` | lgtm |

`lgtm` also calls `code-review`, which ships with Claude Code. The skill needs `go`, `git` and
the `mori` binary on `PATH`; the installer warns when one is absent.

`SKILLS_HOME`, `BIN_DIR`, `SKILLS_REPO` and `SKILLS_REF` override the defaults.

## Watch

```bash
handoff-watch                 # live view
handoff-watch -once           # one plain snapshot
handoff-watch -interval 5s    # time between two reads of the logs
```

One row per handoff with its steps, state and last event, then the events of the selected one.
Failed and blocked handoffs come first, then the running ones, then the done ones.

| Key | Action |
|---|---|
| `j` `k`, arrows | Next and previous handoff, or newer and older events |
| `g` `G` | First and last handoff, or oldest and newest events |
| `PgUp` `PgDn` | One page |
| `tab` | Move the focus between the handoffs and the events |
| `enter` | Focus the events of the selected handoff |
| `/` | Filter the handoffs by name, state or repo |
| `esc` | Leave the events, then clear the filter |
| `?` | Help |
| `q` | Quit |

The view needs a terminal of 60x12 or more and uses the 16 ANSI colors only. `NO_COLOR` removes
the colors: every status keeps a mark of its own (`✓` `!` `✗` `·`, a spinner for a started step).

## Event log

One file per handoff: `$HANDOFF_HOME/<branch>/events.jsonl` (default `~/.handoff`), one JSON
object per line.

| Field | Value |
|---|---|
| `ts` | UTC, `2006-01-02T15:04:05.000Z` |
| `feature` | The branch, also the worktree name in every repo |
| `worktree_path`, `repo`, `commit` | Set once the worktree exists, else `null` |
| `actor` | `session` or `agent` |
| `step` | `read-task`, `pick-repos`, `spawn-agent`, `worktree`, `implement`, `review`, `report`, `relay` |
| `status` | `started`, `done`, `failed`, `blocked` |
| `message` | One line |
| `data` | Extra string pairs |

Write an event by hand:

```bash
go run skill/scripts/log_event.go -actor session my-branch read-task done "read the spec"
```
