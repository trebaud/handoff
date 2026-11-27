---
name: handoff
description: >-
  Takes one task from input to reviewed code in a fresh mori worktree, built by a background
  agent while the session stays free, then stops for approval before any PR. Input is a spec
  file, a short description or a link to a task in a task management app (ClickUp, Linear,
  Jira, GitHub issue). Use when the user says "handoff", "implement this task", "take this
  ticket" or gives a task link and wants it built.
argument-hint: "<spec-file | description | task-url>"
---

# Handoff

Hand `$ARGUMENTS` to a background agent that implements it in a new worktree and reviews it with
`lgtm`. Relay its report, then wait.

Steps 1 to 3 run in this session and stay short. Steps 4 to 6 run in the background agent.

## Event log

Log one event at each point below with `scripts/log_event.go`, next to this file. Flags come
before the branch:

```bash
go run <skill-dir>/scripts/log_event.go [-worktree <absolute path>] <branch> <step> <status> "<one line>"
```

Every event of one handoff goes to `~/.handoff/<branch>/events.jsonl`; the branch is also the
worktree name. `HANDOFF_HOME` moves the root. Log a step once it ends, with `done`, `failed` or
`blocked`: the step after the last done one counts as running. The agent passes `-worktree` from
the moment the worktree exists, once per worktree: it adds the repo and the commit to the event.

| Step | Logged by | When it ends |
|---|---|---|
| `setup` | session | once the agent runs |
| `setup` | agent | once the worktree exists, `failed` when it cannot be made |
| `implement` | agent | code committed and checks pass, or `failed`, or `blocked` on a question |
| `review` | agent | `lgtm` ran and its fixes are committed, or `failed` |
| `report` | agent | just before the final message |
| `report` | session | `failed` when the agent died or stopped before its report |

Put the reason in the message of every `failed` and `blocked` event. A logging error never stops
the work: note it under **Open points** and go on.

The log is the whole interface to anything that shows progress. This skill starts no viewer.

## 1. Read the task

| Input | How to read it |
|---|---|
| Path to a file | Read the file |
| URL | The matching MCP tool or CLI (ClickUp, Linear, Jira, `gh issue view`): title, description, comments, subtasks, linked docs |
| Anything else | It is the description |

State the task in one or two sentences with its acceptance criteria. Ask only when the task
cannot be built as written. A link that cannot be read is a blocker: say so and stop.

The agent cannot ask the user anything. Settle every question here, before step 3.

## 2. Pick the repos

Find the repos the task changes: the current repo, or, when the current directory holds several
repos, the ones the task names or whose code it touches. Search before choosing. Take the
fewest repos that complete the task. Ask when two candidates are equally likely.

Pick the branch name: `<task-id>-<short-slug>` when the task has an id, else `<short-slug>`.
Follow the repo's own branch convention when it documents one.

## 3. Start the background agent

Spawn one `general-purpose` agent with the Agent tool, in the background. One agent per task,
also when the task spans several repos. Do not use `isolation: "worktree"`: the agent makes its
own worktrees with mori.

The agent starts with no context. Its prompt holds:

- the full task text as read in step 1 (not a summary), the acceptance criteria and the answers
  the user gave
- the absolute path of each chosen repo and the branch name
- what the search in step 2 already found: the files and modules the change touches
- the instruction to read this file (give its absolute path) and follow **Event log** and
  steps 4 to 6
- these limits: no questions to the user, no push, no PR, no edits outside the worktrees

Then tell the user the agent's name, the repos, the branch and the path of the log file, and end
the turn. Do not wait, poll or do the agent's work in this session. Further handoffs can start while this one runs.

When the agent's notification arrives, relay its step 6 report in full, then stop. If the agent
failed or stopped early, say what it finished and where the worktrees are.

## 4. Create the worktrees and implement

In each chosen repo, fetch `origin`, then use the `mori` skill to create a new worktree on the
branch. Use the same branch name in every repo. Do all further work inside the worktrees. Never
edit the original checkouts.

- Read the repo's rules (CLAUDE.md, AGENTS.md) and the code around the change first.
- Build the task and nothing else. Add or update tests for the new behaviour.
- Run the repo's typecheck, lint and the affected tests until they pass.
- Commit in small logical commits, in the repo's commit style. Do not push.
- When the task cannot be built as written, do not guess: stop and put the question under
  **Open points** in the report.

## 5. Review

Run the `lgtm` skill in each worktree, with no arguments. Commit the fixes it applies. When this
agent cannot spawn the agents `lgtm` asks for, run each of its checks in turn in this agent.

## 6. Report

Return as the final message, for each repo:

- **Worktree**: full absolute path
- **Branch** and its commits (`git log --oneline <default-branch>..HEAD`)
- **Main diffs**: `git diff --stat <default-branch>...HEAD`, then the few hunks that carry the
  change, each as `file:line` with one line on what it does
- **Checks**: build and test result, what `lgtm` fixed, what it left and why
- **Open points**: assumptions made, criteria not met, questions that blocked the work

Do not push and do not open a PR.
