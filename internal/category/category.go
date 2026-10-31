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

// Default is a category's built-in delay and enabled flag.
type Default struct {
	Delay   time.Duration
	Enabled bool
}

var defaults = map[ID]Default{
	Question:       {Enabled: true},
	PlanApproval:   {Enabled: true},
	Permission:     {Enabled: true},
	MCPInput:       {Enabled: true},
	DoneQuestion:   {Enabled: true},
	Done:           {Enabled: true},
	DoneBackground: {Enabled: false},
	Error:          {Enabled: true},
	UsageLimit:     {Enabled: true},
	IdleReminder:   {Enabled: true},
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
