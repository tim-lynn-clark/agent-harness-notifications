package event

import (
	"strings"
	"testing"
)

func TestParseRecordedStop(t *testing.T) {
	payload := `{"session_id":"s1","cwd":"/x","hook_event_name":"Stop","stop_hook_active":false,
"last_assistant_message":"Done.","background_tasks":[{"id":"t1","type":"shell"}],"session_crons":[]}`
	e, err := Parse(strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if e.HookEventName != "Stop" || e.SessionID != "s1" || e.LastAssistantMessage != "Done." || len(e.BackgroundTasks) != 1 {
		t.Fatalf("unexpected event: %+v", e)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse(strings.NewReader("not json")); err == nil {
		t.Fatal("expected error")
	}
}
