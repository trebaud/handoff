// Command handoff-watch shows the progress of every handoff, read from the event logs
// under $HANDOFF_HOME (default ~/.handoff). The log format in the README is its only
// link to the handoff skill.
//
// Usage:
//
//	handoff-watch [-once] [-interval 1s]
//
// Keys: j/k or the arrows move, g/G jump to the first and last, q quits.
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
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

var steps = []string{"read-task", "pick-repos", "spawn-agent", "worktree", "implement", "review", "report", "relay"}

var stepShort = map[string]string{
	"read-task": "read", "pick-repos": "pick", "spawn-agent": "spawn", "worktree": "wt",
	"implement": "impl", "review": "review", "report": "report", "relay": "relay",
}

// A higher rank wins when the repos of one handoff disagree on a step.
var statusRank = map[string]int{"done": 1, "started": 2, "blocked": 3, "failed": 4}

type event struct {
	TS           time.Time         `json:"ts"`
	Feature      string            `json:"feature"`
	WorktreePath string            `json:"worktree_path"`
	Repo         string            `json:"repo"`
	Commit       string            `json:"commit"`
	Actor        string            `json:"actor"`
	Step         string            `json:"step"`
	Status       string            `json:"status"`
	Message      string            `json:"message"`
	Data         map[string]string `json:"data"`
}

type repoState struct {
	name, commit, path string
	steps              map[string]string
}

type feature struct {
	name    string
	events  []event
	steps   map[string]string
	repos   []*repoState
	state   string
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

	f := &feature{name: filepath.Base(filepath.Dir(logFile)), steps: map[string]string{}}
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
		for _, status := range byRepo {
			if statusRank[status] > statusRank[f.steps[step]] {
				f.steps[step] = status
			}
		}
	}
	f.state = "running"
	if f.steps["relay"] == "done" {
		f.state = "done"
	}
	for _, status := range f.steps {
		if status == "failed" || (status == "blocked" && f.state != "failed") {
			f.state = status
		}
	}
	return f
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
		return features[a].last().TS.After(features[b].last().TS)
	})
	return features
}

type painter struct{ color bool }

func (p painter) paint(code, text string) string {
	if !p.color {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (p painter) status(status, text string) string {
	switch status {
	case "done":
		return p.paint("32", text)
	case "started", "running":
		return p.paint("33", text)
	case "blocked":
		return p.paint("35", text)
	case "failed":
		return p.paint("31", text)
	}
	return p.paint("2", text)
}

func (p painter) pipeline(stepStatus map[string]string) string {
	marks := map[string]string{"done": "✓", "started": "●", "blocked": "!", "failed": "✗"}
	var out strings.Builder
	for _, step := range steps {
		status := stepStatus[step]
		mark := marks[status]
		if mark == "" {
			mark = "·"
		}
		out.WriteString(p.status(status, mark))
	}
	return out.String()
}

func pad(text string, width int) string {
	length := utf8.RuneCountInString(text)
	if length > width {
		runes := []rune(text)
		return string(runes[:width-1]) + "…"
	}
	return text + strings.Repeat(" ", width-length)
}

func age(since time.Duration) string {
	switch {
	case since < time.Minute:
		return fmt.Sprintf("%ds", max(0, int(since.Seconds())))
	case since < time.Hour:
		return fmt.Sprintf("%dm", int(since.Minutes()))
	case since < 48*time.Hour:
		return fmt.Sprintf("%dh", int(since.Hours()))
	}
	return fmt.Sprintf("%dd", int(since.Hours()/24))
}

func repoNames(f *feature) string {
	names := make([]string, len(f.repos))
	for i, repo := range f.repos {
		names[i] = repo.name
	}
	if len(names) == 0 {
		return f.last().Data["repos"]
	}
	return strings.Join(names, ", ")
}

func featureWidth(features []*feature) int {
	width := len("FEATURE")
	for _, f := range features {
		width = max(width, utf8.RuneCountInString(f.name))
	}
	return min(width, 40)
}

func listLines(p painter, features []*feature, selected int, now time.Time) []string {
	width := featureWidth(features)
	lines := []string{p.paint("2", "  "+pad("FEATURE", width)+"  STEPS     STATE    NOW          LAST  REPOS")}
	for i, f := range features {
		cursor := "  "
		name := pad(f.name, width)
		if i == selected {
			cursor = p.paint("1", "> ")
			name = p.paint("1", name)
		}
		last := f.last()
		lines = append(lines, cursor+name+"  "+p.pipeline(f.steps)+"  "+
			p.status(f.state, pad(f.state, 7))+"  "+pad(last.Step, 11)+"  "+
			pad(age(now.Sub(last.TS)), 4)+"  "+repoNames(f))
	}
	return lines
}

func detailLines(p painter, f *feature, room int) []string {
	lines := []string{p.paint("1", f.name)}
	for _, repo := range f.repos {
		// The session steps have no repo: a repo row shows them as the handoff has them.
		repoSteps := map[string]string{}
		for _, step := range steps {
			repoSteps[step] = repo.steps[step]
			if repoSteps[step] == "" && latestHasNoRepo(f, step) {
				repoSteps[step] = f.steps[step]
			}
		}
		lines = append(lines, "  "+p.pipeline(repoSteps)+"  "+repo.name+"  "+
			p.paint("2", strings.TrimSpace(repo.commit+"  "+repo.path)))
	}
	if f.skipped > 0 {
		lines = append(lines, p.paint("31", fmt.Sprintf("  %d unreadable lines skipped", f.skipped)))
	}
	lines = append(lines, "")

	shown := f.events
	if room = room - len(lines); room > 0 && len(shown) > room {
		shown = shown[len(shown)-room:]
	}
	for _, e := range shown {
		lines = append(lines, "  "+p.paint("2", e.TS.Local().Format("Jan 02 15:04:05"))+"  "+
			pad(e.Actor, 7)+"  "+pad(e.Repo, 14)+"  "+pad(e.Step, 11)+"  "+
			p.status(e.Status, pad(e.Status, 7))+"  "+e.Message)
	}
	return lines
}

func latestHasNoRepo(f *feature, step string) bool {
	for _, e := range f.events {
		if e.Step == step && e.Repo != "" {
			return false
		}
	}
	return true
}

func legend(p painter) string {
	names := make([]string, len(steps))
	for i, step := range steps {
		names[i] = stepShort[step]
	}
	return p.paint("2", "steps: "+strings.Join(names, " ")+"   ✓ done  ● started  ! blocked  ✗ failed  · pending")
}

func snapshot(p painter, home string, features []*feature, now time.Time) []string {
	if len(features) == 0 {
		return []string{"no handoff logs in " + home}
	}
	lines := listLines(p, features, -1, now)
	for _, f := range features {
		lines = append(lines, "")
		lines = append(lines, detailLines(p, f, 0)...)
	}
	return append(lines, "", legend(p))
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

func frame(p painter, home string, features []*feature, selected int, now time.Time) []string {
	rows, _ := terminalSize()
	title := p.paint("1", "handoff") + p.paint("2", fmt.Sprintf("  %s  %d features  %s", home, len(features), now.Format("15:04:05")))
	footer := []string{legend(p), p.paint("2", "j/k move   g/G first/last   q quit")}
	if len(features) == 0 {
		return append([]string{title, "", "no handoff logs yet"}, footer...)
	}

	list := listLines(p, features, selected, now)
	listRoom := max(4, (rows-len(footer)-3)/2)
	if len(list) > listRoom {
		first := min(max(1, selected+2-listRoom/2), len(list)-listRoom+1)
		list = append(list[:1:1], list[first:first+listRoom-1]...)
	}

	lines := append([]string{title, ""}, list...)
	lines = append(lines, "")
	room := rows - len(lines) - len(footer) - 1
	lines = append(lines, detailLines(p, features[selected], max(room, 4))...)
	for len(lines) < rows-len(footer) {
		lines = append(lines, "")
	}
	return append(lines[:max(0, rows-len(footer))], footer...)
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

func main() {
	once := flag.Bool("once", false, "print one plain snapshot and exit")
	interval := flag.Duration("interval", time.Second, "time between two reads of the logs")
	flag.Parse()

	home := handoffHome()
	p := painter{color: os.Getenv("NO_COLOR") == ""}

	saved, err := stty("-g")
	stdout, _ := os.Stdout.Stat()
	interactive := err == nil && stdout != nil && stdout.Mode()&os.ModeCharDevice != 0
	if *once || !interactive {
		p.color = p.color && interactive
		fmt.Println(strings.Join(snapshot(p, home, readFeatures(home), time.Now()), "\n"))
		return
	}

	if _, err := stty("cbreak", "-echo"); err != nil {
		fmt.Fprintf(os.Stderr, "handoff-watch: cannot set the terminal mode: %v\n", err)
		os.Exit(1)
	}
	fmt.Print("\x1b[?1049h\x1b[?25l\x1b[?7l")
	restore := func() {
		fmt.Print("\x1b[?7h\x1b[?25h\x1b[?1049l")
		stty(saved)
	}
	defer restore()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	keys := make(chan string)
	go readKeys(keys)
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	selectedName := ""
	for {
		features := readFeatures(home)
		selected := 0
		for i, f := range features {
			if f.name == selectedName {
				selected = i
			}
		}
		draw := func() {
			if len(features) > 0 {
				selectedName = features[selected].name
			}
			lines := frame(p, home, features, selected, time.Now())
			fmt.Print("\x1b[H" + strings.Join(lines, "\x1b[K\r\n") + "\x1b[K\x1b[J")
		}
		draw()

	wait:
		for {
			select {
			case <-signals:
				return
			case <-ticker.C:
				break wait
			case key, open := <-keys:
				if !open {
					return
				}
				switch key {
				case "q", "Q", "\x03", "\x04":
					return
				case "j", "\x1b[B", "\x1bOB":
					selected = min(selected+1, len(features)-1)
				case "k", "\x1b[A", "\x1bOA":
					selected--
				case "g":
					selected = 0
				case "G":
					selected = len(features) - 1
				}
				selected = max(selected, 0)
				draw()
			}
		}
	}
}
