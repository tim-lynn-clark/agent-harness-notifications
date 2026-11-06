// Package category defines the attention categories (spec §5) and their defaults.
package category

import "time"

// ID is a stable attention-category identifier used as a config key.
type ID string

const (
	Question       ID = "question"
	PlanApproval   ID = "plan_approval"
	Permission     ID = "permission"
	MCPInput       ID = "mcp_input"
	DoneQuestion   ID = "done_question"
	Done           ID = "done"
	DoneBackground ID = "done_background"
	Error          ID = "error"
	UsageLimit     ID = "usage_limit"
	IdleReminder   ID = "idle_reminder"
)

// Default is a category's built-in delay, enabled flag, and visual signal
// (visual attention spec §5.2).
type Default struct {
	Delay      time.Duration
	Enabled    bool
	Color      string        // #RRGGBB
	MaxRung    int           // highest escalation rung: 1 aurora, 2 halo, 3 critter
	ClearAfter time.Duration // the visual signal ends this long after it began; 0 never
}

// How long each kind of visual signal lasts before it ends on its own; the
// attention itself still waits for the user (visual attention spec §4.4).
const (
	doneClear   = 90 * time.Second // a finished turn: glow, then a brief halo
	faultClear  = 2 * time.Minute  // error, usage limit: glow and halo
	promptClear = 3 * time.Minute  // waiting on an answer: a minute at each rung
)

var defaults = map[ID]Default{
	Question:       {Enabled: true, Color: "#22C3E6", MaxRung: 3, ClearAfter: promptClear},
	PlanApproval:   {Enabled: true, Color: "#9B6BFF", MaxRung: 3, ClearAfter: promptClear},
	Permission:     {Enabled: true, Color: "#F5A524", MaxRung: 3, ClearAfter: promptClear},
	MCPInput:       {Enabled: true, Color: "#14B8A6", MaxRung: 3, ClearAfter: promptClear},
	DoneQuestion:   {Enabled: true, Color: "#22C3E6", MaxRung: 3, ClearAfter: promptClear},
	Done:           {Enabled: true, Color: "#3FCF6E", MaxRung: 2, ClearAfter: doneClear},
	DoneBackground: {Enabled: false, Color: "#3FCF6E", MaxRung: 2, ClearAfter: doneClear},
	Error:          {Enabled: true, Color: "#F04444", MaxRung: 2, ClearAfter: faultClear},
	UsageLimit:     {Enabled: true, Color: "#E040A0", MaxRung: 2, ClearAfter: faultClear},
	IdleReminder:   {Enabled: true, Color: "#F5D90A", MaxRung: 2, ClearAfter: promptClear},
}

// All returns every category in spec order.
func All() []ID {
	return []ID{Question, PlanApproval, Permission, MCPInput, DoneQuestion, Done, DoneBackground, Error, UsageLimit, IdleReminder}
}

// DefaultFor returns the built-in defaults for id and whether id is known.
func DefaultFor(id ID) (Default, bool) {
	d, ok := defaults[id]
	return d, ok
}
