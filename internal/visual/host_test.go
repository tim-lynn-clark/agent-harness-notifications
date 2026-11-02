package visual

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHostScriptReplacesOlderCopies(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "visual-host-000000000000.js")
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := HostScript(dir)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != string(Script()) {
		t.Fatalf("script not written: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("an older host script was left behind")
	}
}

func TestScriptJoinsHostAndCritters(t *testing.T) {
	got := string(Script())
	if !strings.HasPrefix(got, string(hostJS)) || !strings.HasSuffix(got, string(crittersJS)) {
		t.Fatal("Script is not host.js followed by critters.js")
	}
	base := scriptName(join([]byte("h"), []byte("c")))
	for name, s := range map[string][]byte{
		"host changed":     join([]byte("h2"), []byte("c")),
		"critters changed": join([]byte("h"), []byte("c2")),
	} {
		if scriptName(s) == base {
			t.Errorf("%s: the name did not change", name)
		}
	}
	if string(join([]byte("h"), []byte("c"))) != "h\nc" {
		t.Fatal("join must keep both parts")
	}
}

func TestHostAliveFollowsTheHeartbeat(t *testing.T) {
	dir := t.TempDir()
	beatAt := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	if HostAlive(dir, beatAt) {
		t.Fatal("no heartbeat file: not alive")
	}
	beat := filepath.Join(dir, HeartbeatFile)
	if err := os.WriteFile(beat, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(beat, beatAt, beatAt); err != nil {
		t.Fatal(err)
	}
	if !HostAlive(dir, beatAt.Add(14900*time.Millisecond)) {
		t.Fatal("14.9 s old: alive")
	}
	if HostAlive(dir, beatAt.Add(15*time.Second)) {
		t.Fatal("15 s old: not alive")
	}
}

func TestWritePreviewKeys(t *testing.T) {
	dir := t.TempDir()
	if err := WritePreview(dir, Preview{Category: "done", Label: "p", Color: "#000000", MaxRung: 2, Critter: "cat", StartedMS: 9}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, PreviewFile))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"category": "done", "label": "p", "color": "#000000", "max_rung": 2.0, "critter": "cat", "started_ms": 9.0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}
