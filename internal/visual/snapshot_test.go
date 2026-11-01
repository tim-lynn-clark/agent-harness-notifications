package visual

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/config"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/state"
)

var t0 = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func loadConfig(t *testing.T, body string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestBuildSortsResolvesAndDropsStale(t *testing.T) {
	cfg := loadConfig(t, "[visual]\nstale_after_h = 1\n[categories.done]\ncolor = \"#000001\"\nmax_rung = 1\n")
	atts := []state.Attention{
		{SessionID: "new", Category: "done", SinceMS: t0.Add(-time.Minute).UnixMilli(), Label: "b"},
		{SessionID: "old", Category: "permission", SinceMS: t0.Add(-10 * time.Minute).UnixMilli(), Label: "a"},
		{SessionID: "dead", Category: "error", SinceMS: t0.Add(-2 * time.Hour).UnixMilli()},
	}
	snap := Build(atts, cfg, t0)
	if len(snap.Waiting) != 2 || snap.Waiting[0].SessionID != "old" || snap.Waiting[1].SessionID != "new" {
		t.Fatalf("want old then new, stale dropped: %+v", snap.Waiting)
	}
	if w := snap.Waiting[1]; w.Color != "#000001" || w.MaxRung != 1 {
		t.Fatalf("configured category not resolved: %+v", w)
	}
	if snap.StaleAfterMS != time.Hour.Milliseconds() || snap.HaloAfterMS != cfg.HaloAfter.Milliseconds() {
		t.Fatalf("thresholds: %+v", snap)
	}
}

func TestWriteReflectsClearedSessions(t *testing.T) {
	cfg := loadConfig(t, "")
	st := state.Store{Dir: t.TempDir()}
	set := func(a *state.Attention) {
		if _, err := st.Update("s", func(s *state.Session) { s.Attention = a }); err != nil {
			t.Fatal(err)
		}
		if err := Write(st, cfg, t0); err != nil {
			t.Fatal(err)
		}
	}
	read := func() Snapshot {
		var snap Snapshot
		data, err := os.ReadFile(filepath.Join(st.Dir, SnapshotFile))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &snap); err != nil {
			t.Fatal(err)
		}
		return snap
	}
	set(&state.Attention{SessionID: "s", Category: "done", SinceMS: t0.UnixMilli()})
	if got := read(); len(got.Waiting) != 1 || got.Version != 1 {
		t.Fatalf("after set: %+v", got)
	}
	set(nil)
	if got := read(); got.Waiting == nil || len(got.Waiting) != 0 {
		t.Fatalf("after clear want an empty list, got %+v", got.Waiting)
	}
}
