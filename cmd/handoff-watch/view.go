package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// The 16 ANSI colors only: the theme of the terminal decides how they look, so the view
// reads the same on a dark and on a light background.
const (
	sgrBold    = "1"
	sgrDim     = "2"
	sgrReverse = "7"
	sgrError   = "31"
	sgrSuccess = "32"
	sgrActive  = "33"
	sgrBlocked = "35"
	sgrAccent  = "36"
)

const (
	minCols = 60
	minRows = 12
	// Below this width an event row shows the time without the date.
	wideCols = 100
	// A worktree path with less room than this is left out.
	minPath = 12
)

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Every status has a mark of its own, so no meaning rests on color alone.
type painter struct {
	style   bool   // bold, dim and reverse: off when the output is not a terminal
	color   bool   // off with NO_COLOR
	running string // mark of a started step, "●" when empty
}

func (p painter) attr(code, text string) string {
	if !p.style || text == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (p painter) hue(code, text string) string {
	if !p.color {
		return text
	}
	return p.attr(code, text)
}

func (p painter) bold(text string) string { return p.attr(sgrBold, text) }
func (p painter) dim(text string) string  { return p.attr(sgrDim, text) }

func (p painter) status(status, text string) string {
	switch status {
	case "done":
		return p.hue(sgrSuccess, text)
	case "started", "running":
		return p.hue(sgrActive, text)
	case "blocked":
		return p.hue(sgrBlocked, text)
	case "failed":
		return p.hue(sgrError, text)
	}
	return p.dim(text)
}

func (p painter) mark(status string) string {
	switch status {
	case "done":
		return "✓"
	case "started", "running":
		if p.running == "" {
			return "●"
		}
		return p.running
	case "blocked":
		return "!"
	case "failed":
		return "✗"
	}
	return "·"
}

func (p painter) pipeline(stepStatus map[string]string) string {
	var out strings.Builder
	for _, step := range steps {
		out.WriteString(p.status(stepStatus[step], p.mark(stepStatus[step])))
	}
	return out.String()
}

func (p painter) namedPipeline(stepStatus map[string]string) string {
	cells := make([]string, len(steps))
	for i, step := range steps {
		status := stepStatus[step]
		cells[i] = p.status(status, p.mark(status)+" "+stepShort[step])
	}
	return strings.Join(cells, " ")
}

func (p painter) hints(pairs ...string) string {
	cells := make([]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		cells = append(cells, p.bold(pairs[i])+" "+p.dim(pairs[i+1]))
	}
	return strings.Join(cells, "  ")
}

func (p painter) section(title string, focused bool) string {
	if focused {
		return p.bold(p.hue(sgrAccent, title))
	}
	return p.dim(title)
}

func pad(text string, width int) string {
	if width <= 0 {
		return ""
	}
	length := utf8.RuneCountInString(text)
	if length > width {
		runes := []rune(text)
		return string(runes[:width-1]) + "…"
	}
	return text + strings.Repeat(" ", width-length)
}

// visibleWidth counts the runes outside the SGR sequences.
func visibleWidth(text string) int {
	width, escape := 0, false
	for _, r := range text {
		switch {
		case escape:
			escape = r == '[' || r < '@' || r > '~'
		case r == '\x1b':
			escape = true
		default:
			width++
		}
	}
	return width
}

// clip cuts text to width visible runes. It keeps every SGR sequence, so a cut never
// leaves a style open.
func clip(text string, width int) string {
	if visibleWidth(text) <= width {
		return text
	}
	var out strings.Builder
	seen, escape := 0, false
	for _, r := range text {
		switch {
		case escape:
			out.WriteRune(r)
			escape = r == '[' || r < '@' || r > '~'
		case r == '\x1b':
			out.WriteRune(r)
			escape = true
		case seen < width-1:
			out.WriteRune(r)
			seen++
		case seen == width-1:
			out.WriteString("…")
			seen++
		}
	}
	return out.String()
}

func tilde(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + path[len(home):]
	}
	return path
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

func repoWidth(f *feature) int {
	width := 0
	for _, repo := range f.repos {
		width = max(width, utf8.RuneCountInString(repo.name))
	}
	return min(width, 14)
}

func featureWidth(features []*feature) int {
	width := len("FEATURE")
	for _, f := range features {
		width = max(width, utf8.RuneCountInString(f.name))
	}
	return min(width, 40)
}

func listRow(p painter, f *feature, width int, now time.Time) string {
	last := f.last()
	return pad(f.name, width) + "  " + p.pipeline(f.steps) + "  " +
		p.status(f.state, p.mark(f.state)+" "+pad(f.state, 7)) + "  " + pad(last.Step, 11) + "  " +
		fmt.Sprintf("%4s", age(now.Sub(last.TS))) + "  " + repoNames(f)
}

// listLines gives the column titles, then one row per feature. With focused, the
// selected row is a reverse bar of cols cells.
func listLines(p painter, features []*feature, selected int, focused bool, cols int, now time.Time) []string {
	width := featureWidth(features)
	lines := []string{p.dim("  " + pad("FEATURE", width) + "  " + pad("STEPS", len(steps)) + "  " +
		pad("STATE", 9) + "  " + pad("NOW", 11) + "  LAST  REPOS")}
	for i, f := range features {
		switch {
		case i != selected:
			lines = append(lines, "  "+listRow(p, f, width, now))
		case focused:
			plain := painter{running: p.running}
			lines = append(lines, p.attr(sgrReverse, pad("> "+listRow(plain, f, width, now), cols)))
		default:
			lines = append(lines, p.bold(">")+" "+listRow(p, f, width, now))
		}
	}
	return lines
}

func hasRepoEvent(f *feature, step string) bool {
	for _, e := range f.events {
		if e.Step == step && e.Repo != "" {
			return true
		}
	}
	return false
}

// clipStart cuts the start of text, since the end of a path says the most.
func clipStart(text string, width int) string {
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width < minPath {
		return ""
	}
	return "…" + string(runes[len(runes)-width+1:])
}

// detailHead gives one named pipeline per repo, or one for the handoff before its
// first worktree exists. With cols, a worktree path is cut to fit the row.
func detailHead(p painter, f *feature, cols int) []string {
	var lines []string
	if len(f.repos) == 0 {
		lines = append(lines, "  "+p.namedPipeline(f.steps))
	}
	width := repoWidth(f)
	for _, repo := range f.repos {
		// The session steps have no repo: a repo row shows them as the handoff has them.
		repoSteps := map[string]string{}
		for _, step := range steps {
			repoSteps[step] = repo.steps[step]
			if repoSteps[step] == "" && !hasRepoEvent(f, step) {
				repoSteps[step] = f.steps[step]
			}
		}
		row := "  " + pad(repo.name, width) + "  " + p.namedPipeline(repoSteps) + "  "
		path := repo.path
		if cols > 0 {
			path = clipStart(tilde(path), cols-visibleWidth(row)-utf8.RuneCountInString(repo.commit)-2)
		}
		lines = append(lines, row+p.dim(strings.TrimSpace(repo.commit+"  "+path)))
	}
	if f.skipped > 0 {
		lines = append(lines, p.hue(sgrError, fmt.Sprintf("  ! %d unreadable lines skipped", f.skipped)))
	}
	return lines
}

func eventLines(p painter, f *feature, events []event, timeFormat string) []string {
	width := repoWidth(f)
	lines := make([]string, len(events))
	for i, e := range events {
		repo := ""
		if width > 0 {
			repo = pad(e.Repo, width) + "  "
		}
		lines[i] = "  " + p.dim(e.TS.Local().Format(timeFormat)) + "  " + pad(e.Actor, 7) + "  " + repo +
			pad(e.Step, 11) + "  " + p.status(e.Status, pad(e.Status, 7)) + "  " + e.Message
	}
	return lines
}

func legend(p painter) string {
	names := make([]string, len(steps))
	for i, step := range steps {
		names[i] = stepShort[step]
	}
	return p.dim("steps: " + strings.Join(names, " ") + "   ✓ done  ● started  ! blocked  ✗ failed  · pending")
}

func snapshot(p painter, home string, features []*feature, now time.Time) []string {
	if len(features) == 0 {
		return []string{"no handoff logs in " + home}
	}
	lines := listLines(p, features, -1, false, 0, now)
	for _, f := range features {
		lines = append(lines, "", p.bold(f.name))
		lines = append(lines, detailHead(p, f, 0)...)
		lines = append(lines, "")
		lines = append(lines, eventLines(p, f, f.events, "Jan 02 15:04:05")...)
	}
	return append(lines, "", legend(p))
}

// A view is the state of the live screen: what is selected, which pane has the focus,
// how far the events are scrolled and the filter on the list.
type view struct {
	home         string
	rows, cols   int
	all, shown   []*feature
	selected     int
	selectedName string
	focusEvents  bool
	eventEnd     int // one past the last event shown; 0 follows the newest
	eventRoom    int // event rows of the last frame
	filter       string
	typing       bool
	help         bool
}

func (v *view) load(features []*feature) {
	v.all = features
	v.refresh()
}

// refresh keeps the selection on the same handoff when the rows move.
func (v *view) refresh() {
	v.shown = v.shown[:0:0]
	needle := strings.ToLower(v.filter)
	for _, f := range v.all {
		if strings.Contains(strings.ToLower(f.name+" "+f.state+" "+repoNames(f)), needle) {
			v.shown = append(v.shown, f)
		}
	}
	index := 0
	for i, f := range v.shown {
		if f.name == v.selectedName {
			index = i
		}
	}
	v.pick(index)
}

func (v *view) current() *feature {
	if len(v.shown) == 0 {
		return nil
	}
	return v.shown[v.selected]
}

func (v *view) pick(index int) {
	v.selected = max(0, min(index, len(v.shown)-1))
	name := ""
	if f := v.current(); f != nil {
		name = f.name
	}
	if name != v.selectedName {
		v.selectedName = name
		v.eventEnd = 0
	}
}

func (v *view) scroll(delta int) {
	f := v.current()
	if f == nil {
		return
	}
	total := len(f.events)
	end := v.eventEnd
	if end == 0 || end > total {
		end = total
	}
	end = max(min(total, v.eventRoom), min(end+delta, total))
	if end >= total {
		end = 0
	}
	v.eventEnd = end
}

func (v *view) move(delta int) {
	if v.focusEvents {
		v.scroll(delta)
		return
	}
	v.pick(v.selected + delta)
}

func (v *view) animated() bool {
	for _, f := range v.all {
		if f.state == "running" {
			return true
		}
		for _, status := range f.steps {
			if status == "started" {
				return true
			}
		}
	}
	return false
}

const farAway = 1 << 20

// key applies one key and reports whether to quit.
func (v *view) key(key string) bool {
	if v.help {
		v.help = false
		return false
	}
	if v.typing {
		v.typeKey(key)
		return false
	}
	switch key {
	case "q", "Q", "\x03", "\x04":
		return true
	case "?":
		v.help = true
	case "/":
		v.typing = true
	case "\t", "\x1b[Z":
		v.focusEvents = !v.focusEvents
	case "\r", "\n":
		v.focusEvents = true
	case "\x1b":
		if v.focusEvents {
			v.focusEvents = false
		} else if v.filter != "" {
			v.filter = ""
			v.refresh()
		}
	case "j", "\x1b[B", "\x1bOB":
		v.move(1)
	case "k", "\x1b[A", "\x1bOA":
		v.move(-1)
	case "g", "\x1b[H", "\x1bOH", "\x1b[1~":
		v.move(-farAway)
	case "G", "\x1b[F", "\x1bOF", "\x1b[4~":
		v.move(farAway)
	case "\x1b[5~":
		v.move(-v.page())
	case "\x1b[6~":
		v.move(v.page())
	}
	return false
}

func (v *view) page() int { return max(1, v.eventRoom-1) }

func (v *view) typeKey(key string) {
	switch key {
	case "\r", "\n":
		v.typing = false
		return
	case "\x1b":
		v.typing = false
		v.filter = ""
	case "\x7f", "\x08":
		if _, size := utf8.DecodeLastRuneInString(v.filter); size > 0 {
			v.filter = v.filter[:len(v.filter)-size]
		}
	default:
		if r, _ := utf8.DecodeRuneInString(key); r < ' ' || r == utf8.RuneError {
			return
		}
		v.filter += key
	}
	v.refresh()
}

// splitKeys cuts one read of the terminal into keys: an escape sequence stays whole.
func splitKeys(chunk string) []string {
	var keys []string
	for len(chunk) > 0 {
		_, size := utf8.DecodeRuneInString(chunk)
		if chunk[0] == '\x1b' && len(chunk) > 1 && (chunk[1] == '[' || chunk[1] == 'O') {
			size = 2
			for size < len(chunk) && (chunk[size] < '@' || chunk[size] > '~') {
				size++
			}
			size = min(size+1, len(chunk))
		}
		keys = append(keys, chunk[:size])
		chunk = chunk[size:]
	}
	return keys
}

// frame gives exactly v.rows lines, none wider than v.cols.
func (v *view) frame(p painter, now time.Time) []string {
	if v.cols < minCols || v.rows < minRows {
		return v.fill([]string{fmt.Sprintf("terminal too small: %dx%d, needs %dx%d", v.cols, v.rows, minCols, minRows)}, "")
	}
	lines := []string{v.header(p, now), ""}
	if v.help {
		return v.fill(append(lines, helpLines(p)...), p.hints("any key", "close"))
	}
	return v.fill(append(lines, v.body(p, now)...), v.footer(p))
}

func (v *view) fill(lines []string, footer string) []string {
	body := max(0, v.rows-1)
	for len(lines) < body {
		lines = append(lines, "")
	}
	lines = append(lines[:body:body], footer)
	for i := range lines {
		lines[i] = clip(lines[i], v.cols)
	}
	return lines
}

func (v *view) header(p painter, now time.Time) string {
	counts := map[string]int{}
	for _, f := range v.all {
		counts[f.state]++
	}
	var cells []string
	for _, state := range stateOrder {
		if counts[state] > 0 {
			cells = append(cells, p.status(state, fmt.Sprintf("%s %d %s", p.mark(state), counts[state], state)))
		}
	}
	// A narrow terminal loses the folder first, then the clock.
	left := p.bold("handoff")
	right := strings.Join(cells, "  ")
	fits := func(left, right string) bool { return visibleWidth(left)+2+visibleWidth(right) <= v.cols }
	if clock := strings.Join(append(cells, p.dim(now.Format("15:04:05"))), "  "); fits(left, clock) {
		right = clock
	}
	if folder := left + p.dim("  "+tilde(v.home)); fits(folder, right) {
		left = folder
	}
	gap := max(2, v.cols-visibleWidth(left)-visibleWidth(right))
	return left + strings.Repeat(" ", gap) + right
}

func (v *view) body(p painter, now time.Time) []string {
	if len(v.all) == 0 {
		return []string{"no handoff logs in " + tilde(v.home), p.dim("the handoff skill writes one when it takes a task")}
	}
	title := p.section("HANDOFFS", !v.focusEvents)
	f := v.current()
	if f == nil {
		return []string{title + p.dim(fmt.Sprintf("  0 of %d", len(v.all))), "  no handoff matches /" + v.filter}
	}

	// Rows that are neither a handoff nor an event: the header and its blank line, the
	// two section titles, the column titles, two blank lines and the footer.
	head := detailHead(p, f, v.cols)
	room := v.rows - 8 - len(head)
	listRoom := min(len(v.shown), max(1, room/2))
	v.eventRoom = max(0, room-listRoom)

	position := fmt.Sprintf("  %d/%d", v.selected+1, len(v.shown))
	if v.filter != "" {
		position += fmt.Sprintf(" of %d  /%s", len(v.all), v.filter)
	}
	list := listLines(p, v.shown, v.selected, !v.focusEvents, v.cols, now)
	first := max(0, min(v.selected-listRoom/2, len(v.shown)-listRoom))
	lines := []string{title + p.dim(position), list[0]}
	lines = append(lines, list[1+first:1+first+listRoom]...)

	total := len(f.events)
	end := v.eventEnd
	if end == 0 || end > total {
		end = total
	}
	start := max(0, end-v.eventRoom)
	span := fmt.Sprintf("  %d-%d of %d", start+1, end, total)
	if v.eventEnd != 0 {
		span += "  paused, G follows"
	}
	timeFormat := "15:04:05"
	if v.cols >= wideCols {
		timeFormat = "Jan 02 15:04:05"
	}
	lines = append(lines, "", p.section("EVENTS", v.focusEvents)+"  "+p.bold(f.name)+p.dim(span))
	lines = append(lines, head...)
	lines = append(lines, "")
	return append(lines, eventLines(p, f, f.events[start:end], timeFormat)...)
}

func (v *view) footer(p painter) string {
	if v.typing {
		return p.bold("/") + v.filter + p.attr(sgrReverse, " ") + "  " + p.hints("enter", "keep", "esc", "clear")
	}
	pairs := []string{"j/k", "move", "g/G", "ends", "tab", "events"}
	switch {
	case len(v.all) == 0:
		pairs = nil
	case v.focusEvents:
		pairs = []string{"j/k", "scroll", "g/G", "ends", "tab", "handoffs", "/", "filter"}
	case v.filter != "":
		pairs = append(pairs, "esc", "clear")
	default:
		pairs = append(pairs, "/", "filter")
	}
	return p.hints(append(pairs, "?", "help", "q", "quit")...)
}

func helpLines(p painter) []string {
	keys := [][2]string{
		{"j k  ↓ ↑", "next and previous handoff, or newer and older events"},
		{"g G", "first and last handoff, or oldest and newest events"},
		{"PgUp PgDn", "one page"},
		{"tab", "move the focus between the handoffs and the events"},
		{"enter", "focus the events of the selected handoff"},
		{"/", "filter the handoffs by name, state or repo"},
		{"esc", "leave the events, then clear the filter"},
		{"?", "this help"},
		{"q", "quit"},
	}
	lines := []string{p.bold("KEYS")}
	for _, key := range keys {
		lines = append(lines, "  "+p.bold(pad(key[0], 11))+key[1])
	}
	marks := make([]string, 0, 5)
	for _, status := range []string{"done", "started", "blocked", "failed", "pending"} {
		marks = append(marks, p.status(status, p.mark(status)+" "+status))
	}
	return append(lines, "", p.bold("MARKS"), "  "+strings.Join(marks, "   "),
		"", p.bold("STEPS"), "  "+strings.Join(steps[:4], " → ")+" →", "  "+strings.Join(steps[4:], " → "))
}
