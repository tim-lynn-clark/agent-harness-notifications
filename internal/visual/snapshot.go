// Package visual drives the macOS visual signal: the snapshot it renders, the
// embedded host script, and its LaunchAgent (visual attention spec §4.5, §5, §8).
package visual

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gofrs/flock"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/category"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/config"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/state"
)

// SnapshotFile is the snapshot's name in the state directory.
const SnapshotFile = "visual.json"

// Entry is one waiting session.
type Entry struct {
	SessionID string `json:"session_id"`
	Category  string `json:"category"`
	SinceMS   int64  `json:"since_ms"`
	Label     string `json:"label"`
	Color     string `json:"color"`
	MaxRung   int    `json:"max_rung"`
	// ClearAfterMS ends the entry's signal this long after SinceMS; 0 never.
	ClearAfterMS int64 `json:"clear_after_ms"`
}

// Snapshot is everything the renderer needs; it never reads the config.
type Snapshot struct {
	Version        int     `json:"version"`
	HaloAfterMS    int64   `json:"halo_after_ms"`
	CritterAfterMS int64   `json:"critter_after_ms"`
	StaleAfterMS   int64   `json:"stale_after_ms"`
	Critter        string  `json:"critter"` // a config.Critters id; "random" picks per appearance
	Waiting        []Entry `json:"waiting"`
}

// Build resolves atts against cfg, drops stale and timed-out ones, and sorts
// oldest first.
func Build(atts []state.Attention, cfg *config.Config, now time.Time) Snapshot {
	snap := Snapshot{
		Version:        1,
		HaloAfterMS:    cfg.HaloAfter.Milliseconds(),
		CritterAfterMS: cfg.CritterAfter.Milliseconds(),
		StaleAfterMS:   cfg.StaleAfter.Milliseconds(),
		Critter:        cfg.Critter,
		Waiting:        []Entry{},
	}
	for _, a := range atts {
		age := now.Sub(time.UnixMilli(a.SinceMS))
		if age > cfg.StaleAfter {
			continue
		}
		c := cfg.For(category.ID(a.Category))
		if c.ClearAfter > 0 && age > c.ClearAfter {
			continue
		}
		snap.Waiting = append(snap.Waiting, Entry{
			SessionID: a.SessionID, Category: a.Category, SinceMS: a.SinceMS,
			Label: a.Label, Color: c.Color, MaxRung: c.MaxRung, ClearAfterMS: c.ClearAfter.Milliseconds(),
		})
	}
	sort.Slice(snap.Waiting, func(i, j int) bool {
		a, b := snap.Waiting[i], snap.Waiting[j]
		if a.SinceMS != b.SinceMS {
			return a.SinceMS < b.SinceMS
		}
		return a.SessionID < b.SessionID
	})
	return snap
}

// Write rebuilds the snapshot from every session. Reading and writing happen
// under one lock, so the last writer always reflects the latest sessions.
// It runs as Store.OnAttention, with a session lock held, so it must never take
// a session lock itself; it takes only visual.lock and reads sessions unlocked.
func Write(st state.Store, cfg *config.Config, now time.Time) error {
	if err := os.MkdirAll(st.Dir, 0o700); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(st.Dir, "visual.lock"))
	if err := lock.Lock(); err != nil {
		return err
	}
	defer lock.Unlock()
	atts, err := st.Attentions()
	if err != nil {
		return err
	}
	data, err := json.Marshal(Build(atts, cfg, now))
	if err != nil {
		return err
	}
	return state.WriteFileAtomic(filepath.Join(st.Dir, SnapshotFile), data)
}
