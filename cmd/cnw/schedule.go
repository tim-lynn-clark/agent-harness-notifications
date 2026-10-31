package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/category"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/state"
)

// newRecord starts an event-log record stamped with now and the Orca pane, if any.
func newRecord(now time.Time) state.Record {
	return state.Record{
		Time:         now.Format(time.RFC3339Nano),
		OrcaWorktree: os.Getenv("ORCA_WORKTREE_ID"),
		OrcaPane:     os.Getenv("ORCA_PANE_KEY"),
	}
}

// schedule spawns the detached player for category c when it is enabled and
// has a sound, waiting extra plus the category's delay_ms. seq > 0 ties the
// sound to a pending prompt so resolving the prompt cancels it. cwd, when
// known, lets the player announce the project and worktree. The outcome goes
// into rec.Outcome.
func schedule(rec *state.Record, sessionID, cwd string, seq int64, c category.ID, extra time.Duration) {
	cfg, _, err := loadConfig()
	if err != nil {
		rec.Outcome = "config: " + err.Error()
		return
	}
	cc := cfg.For(c)
	switch {
	case !cc.Enabled:
		rec.Outcome = "disabled"
	case cc.Sound == "":
		rec.Outcome = "no-sound"
	default:
		err := spawnFire([]string{
			"--session", sessionID,
			"--seq", strconv.FormatInt(seq, 10),
			"--category", string(c),
			"--delay", strconv.FormatInt((extra + cc.Delay).Milliseconds(), 10),
			"--cwd", cwd,
		})
		if err != nil {
			rec.Outcome = fmt.Sprintf("spawn: %v", err)
		} else {
			rec.Outcome = "scheduled"
		}
	}
}
