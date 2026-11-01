package main

import (
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/config"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/state"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/visual"
)

// refreshVisual rewrites the visual snapshot after an attention change. Like
// sounds, it never fails the caller. It always rewrites, with built-in
// defaults when the config can't be loaded, so a cleared session never lingers
// in the snapshot. It reads the config directly: the snapshot needs no sounds.
func refreshVisual(st state.Store) {
	cfg, _, err := config.Load(config.Path())
	if err != nil {
		cfg = config.Default()
	}
	_ = visual.Write(st, cfg, time.Now())
}
