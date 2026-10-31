package classify

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/category"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/event"
)

var t0 = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

func TestClassify(t *testing.T) {
	bg := []json.RawMessage{json.RawMessage(`{"id":"t1"}`)}
	cases := []struct {
		name string
		e    event.Event
		r    Recent
		want Result
	}{
		{"question", event.Event{HookEventName: "PermissionRequest", ToolName: "AskUserQuestion"}, Recent{},
			Result{Category: category.Question, Register: true}},
		{"plan", event.Event{HookEventName: "PermissionRequest", ToolName: "ExitPlanMode"}, Recent{},
			Result{Category: category.PlanApproval, Register: true}},
		{"bash permission", event.Event{HookEventName: "PermissionRequest", ToolName: "Bash"}, Recent{},
			Result{Category: category.Permission, Register: true}},
		{"elicitation", event.Event{HookEventName: "Elicitation"}, Recent{},
			Result{Category: category.MCPInput, Register: true}},
		{"permission_prompt for registered question", event.Event{HookEventName: "Notification", NotificationType: "permission_prompt"},
			Recent{Pending: category.Question, LastPermission: t0.Add(-6 * time.Second)}, Result{Category: category.Question}},
		{"permission_prompt for registered plan", event.Event{HookEventName: "Notification", NotificationType: "permission_prompt"},
			Recent{Pending: category.PlanApproval, LastPermission: t0.Add(-6 * time.Second)}, Result{Category: category.PlanApproval}},
		{"permission_prompt for registered permission", event.Event{HookEventName: "Notification", NotificationType: "permission_prompt"},
			Recent{Pending: category.Permission, LastPermission: t0.Add(-6 * time.Second)}, Result{Category: category.Permission}},
		{"permission_prompt answered (no pending, recent request)", event.Event{HookEventName: "Notification", NotificationType: "permission_prompt"},
			Recent{LastPermission: t0.Add(-6 * time.Second)}, Result{}},
		{"permission_prompt at window edge is dropped", event.Event{HookEventName: "Notification", NotificationType: "permission_prompt"},
			Recent{LastPermission: t0.Add(-OrphanWindow)}, Result{}},
		{"permission_prompt orphan (none recorded)", event.Event{HookEventName: "Notification", NotificationType: "permission_prompt"},
			Recent{}, Result{Category: category.Permission}},
		{"permission_prompt orphan (stale)", event.Event{HookEventName: "Notification", NotificationType: "permission_prompt"},
			Recent{LastPermission: t0.Add(-31 * time.Second)}, Result{Category: category.Permission}},
		{"permission_prompt with registered mcp_input, recent request", event.Event{HookEventName: "Notification", NotificationType: "permission_prompt"},
			Recent{Pending: category.MCPInput, LastPermission: t0.Add(-6 * time.Second)}, Result{}},
		{"elicitation_dialog for registered mcp_input", event.Event{HookEventName: "Notification", NotificationType: "elicitation_dialog"},
			Recent{Pending: category.MCPInput, LastElicitation: t0.Add(-6 * time.Second)}, Result{Category: category.MCPInput}},
		{"elicitation_dialog with registered question, recent elicitation", event.Event{HookEventName: "Notification", NotificationType: "elicitation_dialog"},
			Recent{Pending: category.Question, LastElicitation: t0.Add(-6 * time.Second)}, Result{}},
		{"elicitation_dialog answered", event.Event{HookEventName: "Notification", NotificationType: "elicitation_dialog"},
			Recent{LastElicitation: t0.Add(-6 * time.Second)}, Result{}},
		{"elicitation_url_dialog orphan", event.Event{HookEventName: "Notification", NotificationType: "elicitation_url_dialog"},
			Recent{}, Result{Category: category.MCPInput}},
		{"idle", event.Event{HookEventName: "Notification", NotificationType: "idle_prompt"}, Recent{},
			Result{Category: category.IdleReminder}},
		{"quota disabled", event.Event{HookEventName: "Notification", NotificationType: "quota_auto_resume_disabled"}, Recent{},
			Result{Category: category.UsageLimit}},
		{"quota stale", event.Event{HookEventName: "Notification", NotificationType: "quota_auto_resume_stale"}, Recent{},
			Result{Category: category.UsageLimit}},
		{"quota fired ignored", event.Event{HookEventName: "Notification", NotificationType: "quota_auto_resume_fired"}, Recent{}, Result{}},
		{"unknown notification ignored", event.Event{HookEventName: "Notification", NotificationType: "brand_new_type"}, Recent{}, Result{}},
		{"stop done", event.Event{HookEventName: "Stop", LastAssistantMessage: "All done."}, Recent{},
			Result{Category: category.Done, Cancel: CancelAll}},
		{"stop question", event.Event{HookEventName: "Stop", LastAssistantMessage: "Want me to continue?"}, Recent{},
			Result{Category: category.DoneQuestion, Cancel: CancelAll}},
		{"stop background beats question", event.Event{HookEventName: "Stop", LastAssistantMessage: "Continue?", BackgroundTasks: bg}, Recent{},
			Result{Category: category.DoneBackground, Cancel: CancelAll}},
		{"stop failure", event.Event{HookEventName: "StopFailure"}, Recent{}, Result{Category: category.Error, Cancel: CancelAll}},
		{"post tool use", event.Event{HookEventName: "PostToolUse", ToolName: "Bash"}, Recent{}, Result{Cancel: CancelMatchingTool}},
		{"post tool use failure", event.Event{HookEventName: "PostToolUseFailure", ToolName: "Bash"}, Recent{}, Result{Cancel: CancelMatchingTool}},
		{"permission denied", event.Event{HookEventName: "PermissionDenied"}, Recent{}, Result{Cancel: CancelAll}},
		{"elicitation result", event.Event{HookEventName: "ElicitationResult"}, Recent{}, Result{Cancel: CancelAll}},
		{"session end", event.Event{HookEventName: "SessionEnd"}, Recent{}, Result{Cancel: CancelAll}},
		{"user prompt", event.Event{HookEventName: "UserPromptSubmit"}, Recent{}, Result{Cancel: CancelAll}},
		{"pre tool use never cancels", event.Event{HookEventName: "PreToolUse", ToolName: "Bash"}, Recent{}, Result{}},
		{"subagent stop ignored", event.Event{HookEventName: "SubagentStop"}, Recent{}, Result{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Classify(c.e, t0, c.r); got != c.want {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}
