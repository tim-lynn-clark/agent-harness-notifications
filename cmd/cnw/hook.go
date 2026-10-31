package main

import (
	"io"
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/app"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/classify"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/event"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/state"
)

const sessionMaxAge = 7 * 24 * time.Hour

func openStore() (state.Store, error) {
	dir, err := state.DefaultDir()
	return state.Store{Dir: dir}, err
}

// runHook handles one event. It writes nothing to stdout and never fails, so
// Claude Code never sees a hook decision or error from cnw.
func runHook(stdin io.Reader) {
	now := time.Now()
	st, err := openStore()
	if err != nil {
		return
	}
	rec := newRecord(now)
	defer func() { _ = st.Log(rec) }()

	e, err := event.Parse(stdin)
	if err != nil {
		rec.Result, rec.Outcome = "error", "parse: "+err.Error()
		return
	}
	rec.Event, rec.SessionID, rec.Cwd = e.HookEventName, e.SessionID, e.Cwd

	res, seq, err := app.Apply(st, e, now)
	if err != nil {
		rec.Result, rec.Outcome = "error", "state: "+err.Error()
		return
	}
	if e.HookEventName == "SessionEnd" {
		_ = st.Prune(now, sessionMaxAge)
	}

	switch {
	case res.Register:
		rec.Result, rec.Outcome = string(res.Category), "registered"
		return
	case res.Category != "":
		rec.Result = string(res.Category)
	case res.Cancel != classify.CancelNone:
		rec.Result = "cancel"
		return
	default:
		rec.Result = "ignored"
		return
	}

	schedule(&rec, e.SessionID, e.Cwd, seq, res.Category, 0)
}
