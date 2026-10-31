package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/state"
)

// TestHookContract checks that runHook writes nothing to stdout and logs the
// expected outcome, even for bad input and a malformed config (spec §11 T6).
func TestHookContract(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("volume = \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CNW_STATE_DIR", stateDir)
	t.Setenv("CNW_CONFIG", cfgPath)

	cases := []struct {
		name, input, result, outcome string
		prefix                       bool
	}{
		{"garbage", "garbage", "error", "parse: ", true},
		{"stop", `{"hook_event_name":"Stop","session_id":"s1","last_assistant_message":"All done."}`, "done", "config: ", true},
		{"permission", `{"hook_event_name":"PermissionRequest","session_id":"s2","tool_name":"Bash"}`, "permission", "registered", false},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if n := captureStdout(t, func() { runHook(strings.NewReader(c.input)) }); n != 0 {
				t.Fatalf("runHook wrote %d bytes to stdout", n)
			}
			recs := readLog(t, stateDir)
			if len(recs) != i+1 {
				t.Fatalf("log has %d records, want %d", len(recs), i+1)
			}
			r := recs[i]
			okOutcome := r.Outcome == c.outcome
			if c.prefix {
				okOutcome = strings.HasPrefix(r.Outcome, c.outcome)
			}
			if r.Result != c.result || !okOutcome {
				t.Fatalf("record = %+v, want result %q outcome %q", r, c.result, c.outcome)
			}
		})
	}
}

func captureStdout(t *testing.T, fn func()) int {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan int)
	go func() {
		b, _ := io.ReadAll(r)
		done <- len(b)
	}()
	fn()
	os.Stdout = orig
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	n := <-done
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return n
}

func readLog(t *testing.T, dir string) []state.Record {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "events.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	var recs []state.Record
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r state.Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		recs = append(recs, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return recs
}
