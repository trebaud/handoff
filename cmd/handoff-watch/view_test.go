package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

func writeLog(t *testing.T, home, name string, lines ...string) {
	t.Helper()
	dir := filepath.Join(home, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func line(minute int, repo, step, status, message string) string {
	repoField := "null"
	if repo != "" {
		repoField = fmt.Sprintf("%q", repo)
	}
	return fmt.Sprintf(`{"ts":"2026-01-02T15:%02d:00.000Z","repo":%s,"actor":"agent","step":%q,"status":%q,"message":%q}`,
		minute, repoField, step, status, message)
}

func testView(t *testing.T, rows, cols int) *view {
	t.Helper()
	home := t.TempDir()
	var long []string
	for i := 0; i < 40; i++ {
		long = append(long, line(i%60, "api", "implement", "started", fmt.Sprintf("event %d %s", i, strings.Repeat("long ", 40))))
	}
	writeLog(t, home, "running-long", long...)
	writeLog(t, home, "all-done", line(1, "", "read-task", "done", "read"), line(2, "web", "relay", "done", "relayed"))
	writeLog(t, home, "broken", line(3, "api", "review", "failed", "typecheck"), "garbage")
	writeLog(t, home, "waiting", line(4, "api", "implement", "blocked", "needs a decision"))
	writeLog(t, home, "early", line(5, "", "read-task", "started", "reading"))

	v := &view{home: home, rows: rows, cols: cols}
	v.load(readFeatures(home))
	return v
}

func names(features []*feature) []string {
	out := make([]string, len(features))
	for i, f := range features {
		out[i] = f.name
	}
	return out
}

func TestFeaturesNeedingAPersonComeFirst(t *testing.T) {
	v := testView(t, 24, 80)
	want := []string{"broken", "waiting", "running-long", "early", "all-done"}
	if got := names(v.all); !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestFrameFitsTheTerminal(t *testing.T) {
	p := painter{style: true, color: true, running: spinner[0]}
	for _, size := range [][2]int{{12, 60}, {24, 80}, {40, 120}, {60, 200}, {5, 20}} {
		v := testView(t, size[0], size[1])
		states := map[string]func(){
			"list":   func() {},
			"events": func() { v.key("\t"); v.key("k"); v.key("\x1b[5~") },
			"filter": func() { v.key("/"); v.key("z") },
			"help":   func() { v.key("\r"); v.key("\x1b"); v.key("?") },
		}
		for name, enter := range states {
			for i := 0; i < len(v.all)+1; i++ {
				enter()
				lines := v.frame(p, testNow)
				if len(lines) != size[0] {
					t.Fatalf("%s %dx%d: %d lines", name, size[1], size[0], len(lines))
				}
				for _, text := range lines {
					if width := visibleWidth(text); width > size[1] {
						t.Fatalf("%s %dx%d: line of %d cells: %q", name, size[1], size[0], width, text)
					}
				}
				*v = *testView(t, size[0], size[1])
				for j := 0; j <= i; j++ {
					v.key("j")
				}
			}
		}
	}
}

func TestSelectionFollowsTheHandoff(t *testing.T) {
	v := testView(t, 24, 80)
	v.key("G")
	if v.selectedName != "all-done" {
		t.Fatalf("selected %q", v.selectedName)
	}
	v.load([]*feature{v.all[4], v.all[0], v.all[1]})
	if v.current().name != "all-done" || v.selected != 0 {
		t.Fatalf("selected %q at %d", v.current().name, v.selected)
	}
}

func TestFilter(t *testing.T) {
	v := testView(t, 24, 80)
	for _, key := range splitKeys("/BLOCK\r") {
		v.key(key)
	}
	if got := names(v.shown); !reflect.DeepEqual(got, []string{"waiting"}) || v.typing {
		t.Fatalf("shown = %v, typing = %v", got, v.typing)
	}
	v.key("\x1b")
	if len(v.shown) != len(v.all) {
		t.Fatalf("esc left %d of %d", len(v.shown), len(v.all))
	}
	v.key("/")
	v.key("q")
	v.key("\x7f")
	if !v.typing || v.filter != "" {
		t.Fatalf("typing = %v, filter = %q", v.typing, v.filter)
	}
}

func TestEventScroll(t *testing.T) {
	v := testView(t, 24, 80)
	v.key("/")
	for _, key := range splitKeys("long\r\t") {
		v.key(key)
	}
	v.frame(painter{}, testNow)
	total := len(v.current().events)
	v.key("k")
	if v.eventEnd != total-1 {
		t.Fatalf("after k: end %d of %d", v.eventEnd, total)
	}
	v.key("g")
	if v.eventEnd != v.eventRoom {
		t.Fatalf("after g: end %d, room %d", v.eventEnd, v.eventRoom)
	}
	frame := strings.Join(v.frame(painter{}, testNow), "\n")
	if !strings.Contains(frame, "event 0 ") || !strings.Contains(frame, "paused") {
		t.Fatalf("oldest event not shown:\n%s", frame)
	}
	v.key("G")
	if v.eventEnd != 0 {
		t.Fatalf("after G: end %d", v.eventEnd)
	}
}

func TestSplitKeys(t *testing.T) {
	got := splitKeys("jj\x1b[B\x1bOA\x1b[5~é\x1b")
	want := []string{"j", "j", "\x1b[B", "\x1bOA", "\x1b[5~", "é", "\x1b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestClip(t *testing.T) {
	p := painter{style: true, color: true}
	text := p.bold("abc") + p.hue(sgrError, "defgh")
	got := clip(text, 5)
	if visibleWidth(got) != 5 || !strings.HasSuffix(got, "\x1b[0m") || !strings.Contains(got, "…") {
		t.Fatalf("clip = %q", got)
	}
	if clip("abc", 3) != "abc" {
		t.Fatal("a line that fits must not change")
	}
}

func TestPlainSnapshotHasNoEscape(t *testing.T) {
	v := testView(t, 24, 80)
	if out := strings.Join(snapshot(painter{}, v.home, v.all, testNow), "\n"); strings.Contains(out, "\x1b") {
		t.Fatalf("escape sequence in the plain snapshot")
	}
}
