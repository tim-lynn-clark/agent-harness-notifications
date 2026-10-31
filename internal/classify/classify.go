// Package classify maps hook events to attention categories (spec §5) and
// cancellation behaviour (spec §6). PermissionRequest and Elicitation register
// a prompt silently; Claude's "still unanswered" notification triggers its
// sound (spec §5.1). It is pure: no I/O.
package classify

import (
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/category"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/event"
)

// Cancel says which registered prompt, if any, an event cancels.
type Cancel int

const (
	CancelNone Cancel = iota
	// CancelAll clears the session's registered prompt.
	CancelAll
	// CancelMatchingTool clears it only when tool_name and agent_id match the pending request.
	CancelMatchingTool
)

// OrphanWindow is how long after a PermissionRequest/Elicitation an unmatched
// notification (no registered prompt of its kind) is still dropped (spec §5.1).
const OrphanWindow = 30 * time.Second

// Result is the classification of one event.
type Result struct {
	Category category.ID // sound (or registered prompt) category; empty: none
	Register bool        // record a pending prompt of Category; schedule no sound now
	Cancel   Cancel
}

// Recent holds the per-session history the classifier needs.
type Recent struct {
	Pending         category.ID // category of the registered, unanswered prompt; "" if none
	LastPermission  time.Time
	LastElicitation time.Time
}

// Classify maps one hook event to a Result.
func Classify(e event.Event, now time.Time, r Recent) Result {
	switch e.HookEventName {
	case "PermissionRequest":
		switch e.ToolName {
		case "AskUserQuestion":
			return Result{Category: category.Question, Register: true}
		case "ExitPlanMode":
			return Result{Category: category.PlanApproval, Register: true}
		default:
			return Result{Category: category.Permission, Register: true}
		}
	case "Elicitation":
		return Result{Category: category.MCPInput, Register: true}
	case "Notification":
		return classifyNotification(e.NotificationType, now, r)
	case "Stop":
		res := Result{Cancel: CancelAll, Category: category.Done}
		switch {
		case len(e.BackgroundTasks) > 0:
			res.Category = category.DoneBackground
		case EndsWithQuestion(e.LastAssistantMessage):
			res.Category = category.DoneQuestion
		}
		return res
	case "StopFailure":
		return Result{Category: category.Error, Cancel: CancelAll}
	case "PostToolUse", "PostToolUseFailure":
		return Result{Cancel: CancelMatchingTool}
	case "PermissionDenied", "UserPromptSubmit", "ElicitationResult", "SessionEnd":
		return Result{Cancel: CancelAll}
	}
	return Result{}
}

func classifyNotification(kind string, now time.Time, r Recent) Result {
	switch kind {
	case "permission_prompt":
		switch r.Pending {
		case category.Question, category.PlanApproval, category.Permission:
			return Result{Category: r.Pending}
		}
		if orphan(r.LastPermission, now) {
			return Result{Category: category.Permission}
		}
	case "elicitation_dialog", "elicitation_url_dialog":
		if r.Pending == category.MCPInput || orphan(r.LastElicitation, now) {
			return Result{Category: category.MCPInput}
		}
	case "idle_prompt":
		return Result{Category: category.IdleReminder}
	case "quota_auto_resume_disabled", "quota_auto_resume_stale":
		return Result{Category: category.UsageLimit}
	}
	return Result{}
}

func orphan(last, now time.Time) bool {
	return last.IsZero() || now.Sub(last) > OrphanWindow
}
