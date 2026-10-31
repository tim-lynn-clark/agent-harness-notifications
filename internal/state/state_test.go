package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDefaultDir(t *testing.T) {
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Skip("no user cache dir:", err)
	}
	custom := filepath.Join(t.TempDir(), "custom")
	cases := []struct {
		name, env, want string
	}{
		{"CNW_STATE_DIR set", custom, custom},
		{"CNW_STATE_DIR unset", "", filepath.Join(cache, "cnw", "state")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("CNW_STATE_DIR", c.env)
			t.Setenv("CLAUDE_PLUGIN_DATA", "/ignored")
			got, err := DefaultDir()
			if err != nil || got != c.want {
				t.Fatalf("DefaultDir() = %q, %v; want %q", got, err, c.want)
			}
		})
	}
}

func TestUpdatePersistsAndIsolatesSessions(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.Update("a", func(x *Session) { x.Pending = &Pending{Seq: 1, Category: "question"} }); err != nil {
		t.Fatal(err)
	}
	got, err := s.Update("a", func(*Session) {})
	if err != nil || got.Pending == nil || got.Pending.Seq != 1 {
		t.Fatalf("session a not persisted: %+v %v", got, err)
	}
	other, _ := s.Update("b", func(*Session) {})
	if other.Pending != nil {
		t.Fatal("session b must start empty")
	}
}

func TestUpdateSerialisesConcurrentWriters(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Update("a", func(x *Session) { x.NextSeq++ }); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, _ := s.Update("a", func(*Session) {})
	if got.NextSeq != 50 {
		t.Fatalf("lost updates: NextSeq = %d", got.NextSeq)
	}
}

func TestUnsafeSessionIDsDoNotEscapeDir(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir}
	if _, err := s.Update("../../etc/passwd", func(x *Session) { x.NextSeq = 7 }); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want exactly one .json and its .json.lock, got %d entries", len(entries))
	}
	var jsonName, lockName string
	for _, e := range entries {
		if strings.Contains(e.Name(), "..") || strings.Contains(e.Name(), "passwd") {
			t.Fatalf("unsafe file name %q", e.Name())
		}
		switch {
		case strings.HasSuffix(e.Name(), ".json.lock"):
			lockName = e.Name()
		case strings.HasSuffix(e.Name(), ".json"):
			jsonName = e.Name()
		}
	}
	if jsonName == "" || lockName != jsonName+".lock" {
		t.Fatalf("want x.json and x.json.lock, got %q and %q", jsonName, lockName)
	}
	got, _ := s.Update("../../etc/passwd", func(*Session) {})
	if got.NextSeq != 7 {
		t.Fatal("hashed session id not stable")
	}
}

func TestCorruptSessionFileStartsOver(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir}
	os.MkdirAll(filepath.Join(dir, "sessions"), 0o700)
	os.WriteFile(filepath.Join(dir, "sessions", "a.json"), []byte("{nope"), 0o600)
	got, err := s.Update("a", func(x *Session) { x.NextSeq++ })
	if err != nil || got.NextSeq != 1 {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestPruneRemovesOnlyStaleFiles(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir}
	s.Update("old", func(*Session) {})
	s.Update("new", func(*Session) {})
	old := time.Now().Add(-8 * 24 * time.Hour)
	for _, n := range []string{"old.json", "old.json.lock"} {
		os.Chtimes(filepath.Join(dir, "sessions", n), old, old)
	}
	if err := s.Prune(time.Now(), 7*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sessions", "old.json")); !os.IsNotExist(err) {
		t.Fatal("stale session kept")
	}
	if _, err := os.Stat(filepath.Join(dir, "sessions", "new.json")); err != nil {
		t.Fatal("fresh session removed")
	}
}

func TestPlayLockTimesOut(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	release, ok, err := s.AcquirePlayLock(time.Second)
	if err != nil || !ok {
		t.Fatalf("first acquire: ok=%v err=%v", ok, err)
	}
	defer release()
	start := time.Now()
	_, ok, err = s.AcquirePlayLock(200 * time.Millisecond)
	if err != nil || ok {
		t.Fatalf("second acquire must time out: ok=%v err=%v", ok, err)
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Fatal("did not wait for the timeout")
	}
}

func TestLogRotatesAndTails(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir}
	big := strings.Repeat("x", MaxLogBytes)
	os.WriteFile(filepath.Join(dir, "events.log"), []byte(big), 0o600)
	for i := 0; i < 3; i++ {
		if err := s.Log(Record{Result: fmt.Sprintf("r%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	if info, _ := os.Stat(filepath.Join(dir, "events.log.1")); info == nil || info.Size() != MaxLogBytes {
		t.Fatal("old log not rotated to events.log.1")
	}
	lines, err := s.Tail(2)
	if err != nil || len(lines) != 2 || !strings.Contains(lines[1], `"result":"r2"`) {
		t.Fatalf("tail = %v err %v", lines, err)
	}
}
