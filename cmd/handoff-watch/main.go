// Command handoff-watch shows the progress of every handoff, read from the event logs
// under $HANDOFF_HOME (default ~/.handoff). The log format in the README is its only
// link to the handoff skill.
//
// Usage:
//
//	handoff-watch [-once] [-interval 1s]
//
// Keys: j/k or the arrows move, g/G jump to the ends, tab or enter moves the focus
// between the handoffs and the events, / filters, d archives the selected handoff,
// ? shows every key, q quits.
// With -once, or when the output is not a terminal, it prints one plain snapshot.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var steps = []string{"setup", "implement", "review", "report"}

// A higher rank wins when the repos of one handoff disagree on a step: a repo that has
// not logged the step yet ("") keeps it from being done.
var statusRank = map[string]int{"done": 1, "": 2, "started": 2, "blocked": 3, "failed": 4}

// The states of a handoff, the one that needs a person first.
var stateOrder = []string{"failed", "blocked", "running", "done"}

type event struct {
	TS           time.Time `json:"ts"`
	Feature      string    `json:"feature"`
	WorktreePath string    `json:"worktree_path"`
	Repo         string    `json:"repo"`
	Commit       string    `json:"commit"`
	Step         string    `json:"step"`
	Status       string    `json:"status"`
	Message      string    `json:"message"`
}

type repoState struct {
	name, commit, path string
	steps              map[string]string
}

type feature struct {
	name    string
	dir     string
	events  []event
	steps   map[string]string
	repos   []*repoState
	state   string
	now     string // the step in progress, or the one that failed or blocked
	skipped int
}

func (f *feature) last() event { return f.events[len(f.events)-1] }

func handoffHome() string {
	if home := os.Getenv("HANDOFF_HOME"); home != "" {
		return home
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "handoff-watch: no home folder: %v\n", err)
		os.Exit(2)
	}
	return filepath.Join(userHome, ".handoff")
}

func readFeature(logFile string) *feature {
	file, err := os.Open(logFile)
	if err != nil {
		return nil
	}
	defer file.Close()

	dir := filepath.Dir(logFile)
	f := &feature{name: filepath.Base(dir), dir: dir, steps: map[string]string{}}
	repos := map[string]*repoState{}
	latest := map[string]map[string]string{}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var e event
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil || e.Step == "" {
			f.skipped++
			continue
		}
		f.events = append(f.events, e)
		if latest[e.Step] == nil {
			latest[e.Step] = map[string]string{}
		}
		latest[e.Step][e.Repo] = e.Status
		if e.Repo == "" {
			continue
		}
		repo := repos[e.Repo]
		if repo == nil {
			repo = &repoState{name: e.Repo, steps: map[string]string{}}
			repos[e.Repo] = repo
			f.repos = append(f.repos, repo)
		}
		repo.steps[e.Step] = e.Status
		if e.Commit != "" {
			repo.commit = e.Commit
		}
		if e.WorktreePath != "" {
			repo.path = e.WorktreePath
		}
	}
	if len(f.events) == 0 {
		return nil
	}

	for step, byRepo := range latest {
		status, perRepo := "done", false
		for repo, logged := range byRepo {
			perRepo = perRepo || repo != ""
			if statusRank[logged] > statusRank[status] {
				status = logged
			}
		}
		if perRepo {
			for _, repo := range f.repos {
				if statusRank[repo.steps[step]] > statusRank[status] {
					status = repo.steps[step]
				}
			}
		}
		f.steps[step] = status
	}
	f.state = "running"
	if f.steps["report"] == "done" {
		f.state = "done"
	}
	for _, status := range f.steps {
		if status == "failed" || (status == "blocked" && f.state != "failed") {
			f.state = status
		}
	}
	f.now = markCurrent(f.steps, f.state == "running")
	return f
}

// markCurrent gives the first step that is not done. The log holds only the end of each
// step, so while the handoff runs that step is the one in progress: it is marked
// started.
func markCurrent(stepStatus map[string]string, running bool) string {
	for _, step := range steps {
		if stepStatus[step] != "done" {
			if running && stepStatus[step] == "" {
				stepStatus[step] = "started"
			}
			return step
		}
	}
	return steps[len(steps)-1]
}

func readFeatures(home string) []*feature {
	logFiles, _ := filepath.Glob(filepath.Join(home, "*", "events.jsonl"))
	var features []*feature
	for _, logFile := range logFiles {
		if f := readFeature(logFile); f != nil {
			features = append(features, f)
		}
	}
	sort.SliceStable(features, func(a, b int) bool {
		if features[a].state != features[b].state {
			return slices.Index(stateOrder, features[a].state) < slices.Index(stateOrder, features[b].state)
		}
		return features[a].last().TS.After(features[b].last().TS)
	})
	return features
}

// archive moves the log of a handoff to $HANDOFF_HOME/.archive, out of the view. It
// leaves the worktrees and the branch alone. The logger trims the leading dots of its
// folder names, so no handoff can take the name .archive.
func archive(home string, f *feature) error {
	archived := filepath.Join(home, ".archive")
	if err := os.MkdirAll(archived, 0o755); err != nil {
		return err
	}
	target := filepath.Join(archived, f.name)
	if _, err := os.Stat(target); err == nil {
		target += "-" + time.Now().UTC().Format("20060102-150405")
	}
	return os.Rename(f.dir, target)
}

func stty(args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func terminalSize() (rows, cols int) {
	rows, cols = 24, 80
	out, err := stty("size")
	if err != nil {
		return
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return
	}
	if r, err := strconv.Atoi(fields[0]); err == nil && r > 0 {
		rows = r
	}
	if c, err := strconv.Atoi(fields[1]); err == nil && c > 0 {
		cols = c
	}
	return
}

func readKeys(keys chan<- string) {
	buf := make([]byte, 16)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			close(keys)
			return
		}
		keys <- string(buf[:n])
	}
}

// A terminal is the alternate screen in cbreak mode. It remembers the last frame and
// writes only the lines that changed, in one write, so the screen never flickers.
type terminal struct {
	saved string
	prev  []string
}

func (t *terminal) enter() error {
	if _, err := stty("cbreak", "-echo"); err != nil {
		return err
	}
	t.prev = nil
	fmt.Print("\x1b[?1049h\x1b[?25l\x1b[?7l")
	return nil
}

func (t *terminal) leave() {
	fmt.Print("\x1b[?7h\x1b[?25h\x1b[?1049l")
	stty(t.saved)
}

func (t *terminal) draw(lines []string) {
	var out strings.Builder
	// Synchronized output: a terminal that knows it shows the frame at once.
	out.WriteString("\x1b[?2026h")
	if t.prev == nil {
		out.WriteString("\x1b[2J")
	}
	for i, line := range lines {
		if i < len(t.prev) && t.prev[i] == line {
			continue
		}
		fmt.Fprintf(&out, "\x1b[%d;1H\x1b[2K%s", i+1, line)
	}
	if len(lines) < len(t.prev) {
		fmt.Fprintf(&out, "\x1b[%d;1H\x1b[J", len(lines)+1)
	}
	out.WriteString("\x1b[?2026l")
	os.Stdout.WriteString(out.String())
	t.prev = lines
}

func main() {
	once := flag.Bool("once", false, "print one plain snapshot and exit")
	interval := flag.Duration("interval", time.Second, "time between two reads of the logs")
	flag.Parse()

	home := handoffHome()
	p := painter{style: true, color: os.Getenv("NO_COLOR") == ""}

	saved, err := stty("-g")
	stdout, _ := os.Stdout.Stat()
	interactive := err == nil && stdout != nil && stdout.Mode()&os.ModeCharDevice != 0
	if *once || !interactive {
		p.style = interactive
		fmt.Println(strings.Join(snapshot(p, home, readFeatures(home), time.Now()), "\n"))
		return
	}

	term := &terminal{saved: saved}
	if err := term.enter(); err != nil {
		fmt.Fprintf(os.Stderr, "handoff-watch: cannot set the terminal mode: %v\n", err)
		os.Exit(1)
	}
	defer term.leave()

	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGWINCH, syscall.SIGTSTP)
	keys := make(chan string)
	go readKeys(keys)
	reads := time.NewTicker(*interval)
	defer reads.Stop()
	spins := time.NewTicker(100 * time.Millisecond)
	defer spins.Stop()

	v := &view{home: home}
	v.rows, v.cols = terminalSize()
	v.load(readFeatures(home))
	spin := 0
	for {
		p.running = spinner[spin%len(spinner)]
		term.draw(v.frame(p, time.Now()))

	wait:
		select {
		case sig := <-signals:
			switch sig {
			case syscall.SIGWINCH:
			case syscall.SIGTSTP:
				// Give the shell its screen back, stop, and take the screen again on resume.
				term.leave()
				syscall.Kill(syscall.Getpid(), syscall.SIGSTOP)
				if err := term.enter(); err != nil {
					return
				}
			default:
				return
			}
			v.rows, v.cols = terminalSize()
			term.prev = nil
		case <-reads.C:
			v.load(readFeatures(home))
		case <-spins.C:
			if !v.animated() {
				goto wait
			}
			spin++
		case chunk, open := <-keys:
			if !open {
				return
			}
			for _, key := range splitKeys(chunk) {
				if v.key(key) {
					return
				}
			}
		}
	}
}
