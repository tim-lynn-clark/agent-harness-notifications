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

// passiveClear ends the visual signal of categories with nothing to answer.
const passiveClear = 5 * time.Minute

var defaults = map[ID]Default{
	Question:       {Enabled: true, Color: "#22C3E6", MaxRung: 3},
	PlanApproval:   {Enabled: true, Color: "#9B6BFF", MaxRung: 3},
	Permission:     {Enabled: true, Color: "#F5A524", MaxRung: 3},
	MCPInput:       {Enabled: true, Color: "#14B8A6", MaxRung: 3},
	DoneQuestion:   {Enabled: true, Color: "#22C3E6", MaxRung: 3},
	Done:           {Enabled: true, Color: "#3FCF6E", MaxRung: 2, ClearAfter: passiveClear},
	DoneBackground: {Enabled: false, Color: "#3FCF6E", MaxRung: 2, ClearAfter: passiveClear},
	Error:          {Enabled: true, Color: "#F04444", MaxRung: 2, ClearAfter: passiveClear},
	UsageLimit:     {Enabled: true, Color: "#E040A0", MaxRung: 2, ClearAfter: passiveClear},
	IdleReminder:   {Enabled: true, Color: "#F5D90A", MaxRung: 2},
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
