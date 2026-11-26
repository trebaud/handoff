//go:build ignore

// Command log_event appends one normalized handoff event to a JSONL log.
//
// One log per handoff, under $HANDOFF_HOME (default ~/.handoff):
//
//	<feature>/events.jsonl
//
// The feature is the branch, which is also the name of the worktree in every repo of
// the handoff. The repo field tells the worktrees of a multi-repo handoff apart.
//
// Usage:
//
//	go run log_event.go [-worktree PATH] [-actor session|agent] [-data KEY=VALUE ...]
//	                    <feature> <step> <status> [message]
//
// It prints the path of the log file it wrote to.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

var (
	steps    = []string{"read-task", "pick-repos", "spawn-agent", "worktree", "implement", "review", "report", "relay"}
	statuses = []string{"started", "done", "failed", "blocked"}
	actors   = []string{"session", "agent"}

	unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
)

type event struct {
	TS           string            `json:"ts"`
	Feature      string            `json:"feature"`
	WorktreePath *string           `json:"worktree_path"`
	Repo         *string           `json:"repo"`
	Commit       *string           `json:"commit"`
	Actor        string            `json:"actor"`
	Step         string            `json:"step"`
	Status       string            `json:"status"`
	Message      string            `json:"message"`
	Data         map[string]string `json:"data"`
}

type dataFlag map[string]string

func (d dataFlag) String() string { return fmt.Sprint(map[string]string(d)) }

func (d dataFlag) Set(pair string) error {
	key, value, found := strings.Cut(pair, "=")
	if !found || key == "" {
		return fmt.Errorf("expects KEY=VALUE, got %q", pair)
	}
	d[key] = value
	return nil
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "log_event: "+format+"\n", args...)
	os.Exit(2)
}

func folderName(value string) string {
	name := strings.Trim(unsafeChars.ReplaceAllString(value, "-"), "-.")
	if name == "" {
		fail("%q gives an empty folder name", value)
	}
	return name
}

func git(worktree string, args ...string) *string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", worktree}, args...)...).Output()
	if err != nil {
		return nil
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return nil
	}
	return &value
}

func repoName(worktree string) *string {
	commonDir := git(worktree, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if commonDir == nil {
		return nil
	}
	name := filepath.Base(*commonDir)
	if name == ".git" {
		name = filepath.Base(filepath.Dir(*commonDir))
	} else {
		name = strings.TrimSuffix(name, ".git")
	}
	return &name
}

func handoffHome() string {
	if home := os.Getenv("HANDOFF_HOME"); home != "" {
		return home
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		fail("no home folder: %v", err)
	}
	return filepath.Join(userHome, ".handoff")
}

func main() {
	data := dataFlag{}
	worktree := flag.String("worktree", "", "absolute path of the worktree the event is about")
	actor := flag.String("actor", "agent", "who logs the event: "+strings.Join(actors, " or "))
	flag.Var(data, "data", "extra KEY=VALUE field, repeatable")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: log_event [flags] <feature> <step> <status> [message]")
		fmt.Fprintln(os.Stderr, "  step:   "+strings.Join(steps, ", "))
		fmt.Fprintln(os.Stderr, "  status: "+strings.Join(statuses, ", "))
		flag.PrintDefaults()
	}
	flag.Parse()

	args := flag.Args()
	if len(args) < 3 || len(args) > 4 {
		flag.Usage()
		os.Exit(2)
	}
	feature, step, status := args[0], args[1], args[2]
	message := ""
	if len(args) == 4 {
		message = args[3]
	}
	if !slices.Contains(steps, step) {
		fail("unknown step %q, choose from %s", step, strings.Join(steps, ", "))
	}
	if !slices.Contains(statuses, status) {
		fail("unknown status %q, choose from %s", status, strings.Join(statuses, ", "))
	}
	if !slices.Contains(actors, *actor) {
		fail("unknown actor %q, choose from %s", *actor, strings.Join(actors, ", "))
	}

	logDir := filepath.Join(handoffHome(), folderName(feature))
	entry := event{
		TS:      time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		Feature: feature,
		Actor:   *actor,
		Step:    step,
		Status:  status,
		Message: message,
		Data:    data,
	}

	if *worktree != "" {
		path, err := filepath.Abs(*worktree)
		if err != nil {
			fail("bad worktree path: %v", err)
		}
		entry.WorktreePath = &path
		entry.Repo = repoName(path)
		entry.Commit = git(path, "rev-parse", "--short", "HEAD")
	}

	line, err := json.Marshal(entry)
	if err != nil {
		fail("cannot encode the event: %v", err)
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		fail("cannot create %s: %v", logDir, err)
	}
	logFile := filepath.Join(logDir, "events.jsonl")
	file, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fail("cannot open %s: %v", logFile, err)
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		fail("cannot write %s: %v", logFile, err)
	}
	if err := file.Close(); err != nil {
		fail("cannot close %s: %v", logFile, err)
	}
	fmt.Println(logFile)
}
