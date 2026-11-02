package visual

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/category"
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

func TestBuildDropsTimedOutEntries(t *testing.T) {
	cfg := loadConfig(t, "[categories.permission]\nclear_after_s = 120\n")
	atts := []state.Attention{
		{SessionID: "done-old", Category: "done", SinceMS: t0.Add(-6 * time.Minute).UnixMilli()},
		{SessionID: "done-new", Category: "done", SinceMS: t0.Add(-4 * time.Minute).UnixMilli()},
		{SessionID: "perm-old", Category: "permission", SinceMS: t0.Add(-3 * time.Minute).UnixMilli()},
		{SessionID: "question", Category: "question", SinceMS: t0.Add(-5 * time.Hour).UnixMilli()},
	}
	snap := Build(atts, cfg, t0)
	if len(snap.Waiting) != 2 || snap.Waiting[0].SessionID != "question" || snap.Waiting[1].SessionID != "done-new" {
		t.Fatalf("want question then done-new: %+v", snap.Waiting)
	}
	if q, d := snap.Waiting[0], snap.Waiting[1]; q.ClearAfterMS != 0 || d.ClearAfterMS != (5*time.Minute).Milliseconds() {
		t.Fatalf("clear_after_ms: question %d, done %d", q.ClearAfterMS, d.ClearAfterMS)
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

func TestBuildWithDefaults(t *testing.T) {
	snap := Build([]state.Attention{{SessionID: "s", Category: "usage_limit", SinceMS: t0.UnixMilli(), Label: "notifier"}},
		config.Default(), t0)
	if snap.HaloAfterMS != 60000 || snap.CritterAfterMS != 120000 || snap.Critter != "random" {
		t.Fatalf("thresholds: halo %d critter %d %q", snap.HaloAfterMS, snap.CritterAfterMS, snap.Critter)
	}
	d, _ := category.DefaultFor(category.UsageLimit)
	if len(snap.Waiting) != 1 {
		t.Fatalf("waiting: %+v", snap.Waiting)
	}
	if w := snap.Waiting[0]; w.Label != "notifier" || w.Color != d.Color || w.MaxRung != d.MaxRung || d.Color == "" || d.MaxRung == 0 {
		t.Fatalf("entry %+v, want label notifier, %s, rung %d", w, d.Color, d.MaxRung)
	}
}

func TestSnapshotCarriesTheCritter(t *testing.T) {
	cfg := config.Default()
	cfg.Critter = "drone"
	data, err := json.Marshal(Build(nil, cfg, t0))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["critter"] != "drone" {
		t.Fatalf("critter %v in %s", got["critter"], data)
	}
}
