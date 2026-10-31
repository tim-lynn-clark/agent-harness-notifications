// Package event parses the Claude Code hook payload fields cnw uses.
package event

import (
	"encoding/json"
	"io"
)

// Event holds the subset of hook input fields cnw reads. Unknown fields are ignored.
type Event struct {
	HookEventName        string            `json:"hook_event_name"`
	SessionID            string            `json:"session_id"`
	Cwd                  string            `json:"cwd"`
	AgentID              string            `json:"agent_id"`
	ToolName             string            `json:"tool_name"`
	NotificationType     string            `json:"notification_type"`
	LastAssistantMessage string            `json:"last_assistant_message"`
	BackgroundTasks      []json.RawMessage `json:"background_tasks"`
}

// Parse decodes one hook payload.
func Parse(r io.Reader) (Event, error) {
	var e Event
	err := json.NewDecoder(r).Decode(&e)
	return e, err
}
