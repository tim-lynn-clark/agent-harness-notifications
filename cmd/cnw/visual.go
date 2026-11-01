package main

import (
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/state"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/visual"
)

// refreshVisual rewrites the visual snapshot after an attention change. Like
// sounds, it never fails the caller; a config error leaves the old snapshot.
func refreshVisual(st state.Store) {
	cfg, _, err := loadConfig()
	if err != nil {
		return
	}
	_ = visual.Write(st, cfg, time.Now())
}
