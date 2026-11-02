package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/category"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/state"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/visual"
)

// signalEnv points cnw at temp state and a config that disables every
// category, so the commands log their classification without spawning fire.
func signalEnv(t *testing.T) state.Store {
	t.Helper()
	dir := t.TempDir()
	var cfg strings.Builder
	for _, id := range category.All() {
		cfg.WriteString("[categories." + string(id) + "]\nenabled = false\n")
	}
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(cfg.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(dir, "state")
	t.Setenv("CNW_STATE_DIR", stateDir)
	t.Setenv("CNW_CONFIG", cfgPath)
	// The test binary has no bundled clips beside it; with no bundled dir
	// there is no kokoro_voice to check.
	orig := soundsDir
	soundsDir = func() string { return "" }
	t.Cleanup(func() { soundsDir = orig })
	return state.Store{Dir: stateDir}
}

func pendingOf(t *testing.T, st state.Store, session string) *state.Pending {
	t.Helper()
	s, err := st.Update(session, func(*state.Session) {})
	if err != nil {
		t.Fatal(err)
	}
	return s.Pending
}

func TestTurnEndClassifiesReply(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		reply string
		want  string
	}{
		{"plain summary", []string{"--session", "s"}, "All tests pass.", "done"},
		{"closing question", []string{"--session", "s"}, "I fixed it.\n\nWant me to open a PR?", "done_question"},
		{"error wins over question", []string{"--session", "s", "--error"}, "Should I retry?", "error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := signalEnv(t)
			var code int
			if n := captureStdout(t, func() { code = runTurnEnd(c.args, strings.NewReader(c.reply)) }); n != 0 {
				t.Fatalf("turn-end wrote %d bytes to stdout", n)
			}
			if code != 0 {
				t.Fatalf("exit code %d", code)
			}
			recs := readLog(t, st.Dir)
			if len(recs) != 1 || recs[0].Event != "turn-end" || recs[0].Result != c.want || recs[0].Outcome != "disabled" {
				t.Fatalf("log = %+v, want one turn-end %s/disabled", recs, c.want)
			}
		})
	}
}

func TestNotifyPendingThenResolve(t *testing.T) {
	st := signalEnv(t)
	if code := runNotify([]string{"question", "--session", "omp-1", "--pending", "--after", "6000"}); code != 0 {
		t.Fatalf("notify exit %d", code)
	}
	p := pendingOf(t, st, "omp-1")
	if p == nil || p.Category != "question" || p.Seq == 0 {
		t.Fatalf("notify --pending must register a question prompt, got %+v", p)
	}
	if code := runResolve([]string{"--session", "omp-1"}); code != 0 {
		t.Fatalf("resolve exit %d", code)
	}
	if p := pendingOf(t, st, "omp-1"); p != nil {
		t.Fatalf("resolve must clear the prompt, got %+v", p)
	}
	recs := readLog(t, st.Dir)
	if len(recs) != 2 || recs[0].Event != "notify" || recs[0].Result != "question" || recs[1].Event != "resolve" {
		t.Fatalf("log = %+v", recs)
	}
}

func TestTurnEndClearsPendingPrompt(t *testing.T) {
	st := signalEnv(t)
	runNotify([]string{"permission", "--session", "omp-2", "--pending"})
	runTurnEnd([]string{"--session", "omp-2"}, strings.NewReader("Done."))
	if p := pendingOf(t, st, "omp-2"); p != nil {
		t.Fatalf("turn end must clear the pending prompt, got %+v", p)
	}
}

func TestNotifyWithoutPendingRegistersNothing(t *testing.T) {
	st := signalEnv(t)
	if code := runNotify([]string{"idle_reminder", "--session", "omp-3"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if p := pendingOf(t, st, "omp-3"); p != nil {
		t.Fatalf("plain notify must not register a prompt, got %+v", p)
	}
}

func TestSignalUsageErrors(t *testing.T) {
	signalEnv(t)
	for name, code := range map[string]int{
		"no category":      runNotify(nil),
		"unknown category": runNotify([]string{"lunch", "--session", "s"}),
		"notify session":   runNotify([]string{"done"}),
		"negative after":   runNotify([]string{"done", "--session", "s", "--after", "-1"}),
		"resolve session":  runResolve(nil),
		"turn-end session": runTurnEnd(nil, strings.NewReader("")),
	} {
		if code != 2 {
			t.Errorf("%s: exit %d, want 2", name, code)
		}
	}
}

func TestResolveClearsSnapshotWithInvalidConfig(t *testing.T) {
	signalEnv(t)
	if err := os.WriteFile(os.Getenv("CNW_CONFIG"), []byte("[visual]\nhalo_after_s = 300\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Update("s", func(s *state.Session) {
		s.Attention = &state.Attention{SessionID: "s", Category: "done", SinceMS: time.Now().UnixMilli()}
	}); err != nil {
		t.Fatal(err)
	}
	runResolve([]string{"--session", "s"})
	data, err := os.ReadFile(filepath.Join(st.Dir, visual.SnapshotFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"waiting":[]`) {
		t.Fatalf("snapshot still lists the session: %s", data)
	}
}

func TestScheduleRecordsTheEventTimeAndTerminal(t *testing.T) {
	signalEnv(t)
	if err := os.WriteFile(os.Getenv("CNW_CONFIG"), []byte("[categories.done]\nsound = \"/x.wav\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var got []string
	orig := spawnFire
	spawnFire = func(args []string) error { got = args; return nil }
	t.Cleanup(func() { spawnFire = orig })

	at := time.UnixMilli(1234567)
	var rec state.Record
	schedule(&rec, "s", "/proj", 0, category.Done, 0, at)
	if rec.Outcome != "scheduled" {
		t.Fatalf("outcome %q", rec.Outcome)
	}
	flags := map[string]string{}
	for i := 0; i+1 < len(got); i += 2 {
		flags[got[i]] = got[i+1]
	}
	if flags["--at"] != "1234567" {
		t.Fatalf("--at in %v", got)
	}
	if _, ok := flags["--jump"]; ok != (runtime.GOOS == "darwin") {
		t.Fatalf("--jump present = %v on %s: %v", ok, runtime.GOOS, got)
	}
}

func TestRefreshVisualLogsAFailedWrite(t *testing.T) {
	st := signalEnv(t)
	// A directory where the snapshot goes makes the write fail.
	if err := os.MkdirAll(filepath.Join(st.Dir, visual.SnapshotFile, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	refreshVisual(st)
	recs := readLog(t, st.Dir)
	if len(recs) != 1 || recs[0].Event != "visual" || !strings.HasPrefix(recs[0].Outcome, "error: ") {
		t.Fatalf("log %+v", recs)
	}
}
