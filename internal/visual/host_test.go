package visual

import (
	"os"
	"path/filepath"
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
	if data, err := os.ReadFile(path); err != nil || string(data) != string(hostJS) {
		t.Fatalf("script not written: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("an older host script was left behind")
	}
}

func TestHostAliveFollowsTheHeartbeat(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if HostAlive(dir, now) {
		t.Fatal("no heartbeat file: not alive")
	}
	beat := filepath.Join(dir, HeartbeatFile)
	if err := os.WriteFile(beat, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !HostAlive(dir, now) {
		t.Fatal("fresh heartbeat: alive")
	}
	if HostAlive(dir, now.Add(16*time.Second)) {
		t.Fatal("heartbeat older than 15 s: not alive")
	}
}
