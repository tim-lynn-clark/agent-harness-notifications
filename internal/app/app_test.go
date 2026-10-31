package app

import (
	"testing"
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/category"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/classify"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/event"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/state"
)

var t0 = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

func perm(session, tool, agent string) event.Event {
	return event.Event{HookEventName: "PermissionRequest", SessionID: session, ToolName: tool, AgentID: agent}
}

func prompt(session string) event.Event {
	return event.Event{HookEventName: "Notification", SessionID: session, NotificationType: "permission_prompt"}
}

func mustApply(t *testing.T, st state.Store, e event.Event, now time.Time) (classify.Result, int64) {
	t.Helper()
	res, seq, err := Apply(st, e, now)
	if err != nil {
		t.Fatal(err)
	}
	return res, seq
}

func claim(t *testing.T, st state.Store, session string, seq int64, now time.Time, cooldown time.Duration) string {
	t.Helper()
	out, err := Claim(st, session, seq, now, cooldown)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRegistrationPlaysNothing(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	res, seq := mustApply(t, st, perm("s", "Bash", ""), t0)
	if res != (classify.Result{Category: category.Permission, Register: true}) || seq != 0 {
		t.Fatalf("registration: %+v seq=%d", res, seq)
	}
}

func TestUnansweredPromptPlaysOnce(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	mustApply(t, st, perm("s", "AskUserQuestion", ""), t0)
	res, seq := mustApply(t, st, prompt("s"), t0.Add(6*time.Second))
	if res.Category != category.Question || seq == 0 {
		t.Fatalf("unanswered prompt: %+v seq=%d", res, seq)
	}
	if got := claim(t, st, "s", seq, t0.Add(6*time.Second), 0); got != OutcomePlay {
		t.Fatalf("got %s", got)
	}
	if got := claim(t, st, "s", seq, t0.Add(7*time.Second), 0); got != OutcomeCancelled {
		t.Fatalf("a pending sound must play only once, got %s", got)
	}
}

func TestAnsweredPromptIsDropped(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	mustApply(t, st, perm("s", "Bash", ""), t0)
	mustApply(t, st, event.Event{HookEventName: "PostToolUse", SessionID: "s", ToolName: "Bash"}, t0.Add(2*time.Second))
	res, seq := mustApply(t, st, prompt("s"), t0.Add(20*time.Second))
	if res.Category != "" || seq != 0 {
		t.Fatalf("answered prompt must be dropped: %+v seq=%d", res, seq)
	}
}

func TestNewerRegistrationReplacesOlder(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	mustApply(t, st, perm("s", "Bash", ""), t0)
	mustApply(t, st, perm("s", "AskUserQuestion", ""), t0.Add(time.Second))
	res, seq := mustApply(t, st, prompt("s"), t0.Add(7*time.Second))
	if res.Category != category.Question || seq == 0 {
		t.Fatalf("latest registration must win: %+v seq=%d", res, seq)
	}
}

func TestCancellationIsPerSession(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	mustApply(t, st, perm("a", "Bash", ""), t0)
	mustApply(t, st, perm("b", "Bash", ""), t0)
	mustApply(t, st, event.Event{HookEventName: "UserPromptSubmit", SessionID: "a"}, t0.Add(time.Second))
	if res, seq := mustApply(t, st, prompt("a"), t0.Add(6*time.Second)); res.Category != "" || seq != 0 {
		t.Fatalf("session a must be dropped: %+v seq=%d", res, seq)
	}
	if res, seq := mustApply(t, st, prompt("b"), t0.Add(6*time.Second)); res.Category != category.Permission || seq == 0 {
		t.Fatalf("session b must be unaffected: %+v seq=%d", res, seq)
	}
}

func TestPostToolUseCancelsOnlyMatchingToolAndAgent(t *testing.T) {
	cases := []struct {
		name        string
		tool, agent string
		cancels     bool
	}{
		{"same tool, main agent", "Bash", "", true},
		{"different tool", "Read", "", false},
		{"same tool, other agent", "Bash", "sub-1", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := state.Store{Dir: t.TempDir()}
			mustApply(t, st, perm("s", "Bash", ""), t0)
			mustApply(t, st, event.Event{HookEventName: "PostToolUse", SessionID: "s", ToolName: c.tool, AgentID: c.agent}, t0.Add(time.Second))
			res, seq := mustApply(t, st, prompt("s"), t0.Add(6*time.Second))
			if c.cancels {
				if res.Category != "" || seq != 0 {
					t.Fatalf("must be dropped: %+v seq=%d", res, seq)
				}
			} else if res.Category != category.Permission || seq == 0 {
				t.Fatalf("must still sound: %+v seq=%d", res, seq)
			}
		})
	}
}

func TestOrphanPromptInOtherSessionSounds(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	mustApply(t, st, perm("s", "Bash", ""), t0)
	res, seq := mustApply(t, st, prompt("other"), t0.Add(6*time.Second))
	if res.Category != category.Permission || seq != 0 {
		t.Fatalf("orphan prompt sounds uncancellably: %+v seq=%d", res, seq)
	}
}

func TestStopClearsRegistrationAndCooldownApplies(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	mustApply(t, st, perm("s", "Bash", ""), t0)
	res, stopSeq := mustApply(t, st, event.Event{HookEventName: "Stop", SessionID: "s", LastAssistantMessage: "Done."}, t0.Add(time.Second))
	if res.Category != category.Done || stopSeq != 0 {
		t.Fatalf("stop: %+v seq=%d", res, stopSeq)
	}
	if res, seq := mustApply(t, st, prompt("s"), t0.Add(6*time.Second)); res.Category != "" || seq != 0 {
		t.Fatalf("Stop must clear the registration: %+v seq=%d", res, seq)
	}
	const cd = 1500 * time.Millisecond
	if out := claim(t, st, "s", 0, t0.Add(1*time.Second), cd); out != OutcomePlay {
		t.Fatalf("first done sound: %s", out)
	}
	if out := claim(t, st, "s", 0, t0.Add(2*time.Second), cd); out != OutcomeCooldown {
		t.Fatalf("second sound inside cooldown: %s", out)
	}
	if out := claim(t, st, "s", 0, t0.Add(3*time.Second), cd); out != OutcomePlay {
		t.Fatalf("sound after cooldown: %s", out)
	}
}

func TestAnswerAfterTriggerCancels(t *testing.T) {
	answers := []event.Event{
		{HookEventName: "PostToolUse", SessionID: "s", ToolName: "Bash"},
		{HookEventName: "UserPromptSubmit", SessionID: "s"},
	}
	for _, a := range answers {
		t.Run(a.HookEventName, func(t *testing.T) {
			st := state.Store{Dir: t.TempDir()}
			mustApply(t, st, perm("s", "Bash", ""), t0)
			res, seq := mustApply(t, st, prompt("s"), t0.Add(6*time.Second))
			if res.Category != category.Permission || seq <= 0 {
				t.Fatalf("trigger: %+v seq=%d", res, seq)
			}
			mustApply(t, st, a, t0.Add(7*time.Second))
			if got := claim(t, st, "s", seq, t0.Add(8*time.Second), 0); got != OutcomeCancelled {
				t.Fatalf("answer after trigger must cancel, got %s", got)
			}
		})
	}
}

func TestRegisterThenResolveCancels(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	seq, err := Register(st, "omp", category.Question, t0)
	if err != nil || seq == 0 {
		t.Fatalf("register: seq=%d err=%v", seq, err)
	}
	if err := Resolve(st, "omp"); err != nil {
		t.Fatal(err)
	}
	if got := claim(t, st, "omp", seq, t0.Add(6*time.Second), 0); got != OutcomeCancelled {
		t.Fatalf("a resolved prompt must stay silent, got %s", got)
	}
}

func TestRegisteredPromptPlaysOnceWhenUnresolved(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	first, err := Register(st, "omp", category.Permission, t0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Register(st, "omp", category.Question, t0.Add(time.Second))
	if err != nil || second == first {
		t.Fatalf("a new registration needs a new seq: first=%d second=%d err=%v", first, second, err)
	}
	if got := claim(t, st, "omp", first, t0.Add(6*time.Second), 0); got != OutcomeCancelled {
		t.Fatalf("superseded registration must not play, got %s", got)
	}
	if got := claim(t, st, "omp", second, t0.Add(7*time.Second), 0); got != OutcomePlay {
		t.Fatalf("current registration must play, got %s", got)
	}
	if got := claim(t, st, "omp", second, t0.Add(8*time.Second), 0); got != OutcomeCancelled {
		t.Fatalf("a played registration must not play twice, got %s", got)
	}
}

func TestResolveLeavesOtherSessions(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	a, _ := Register(st, "a", category.Question, t0)
	b, _ := Register(st, "b", category.Question, t0)
	if err := Resolve(st, "a"); err != nil {
		t.Fatal(err)
	}
	if got := claim(t, st, "a", a, t0.Add(6*time.Second), 0); got != OutcomeCancelled {
		t.Fatalf("session a: %s", got)
	}
	if got := claim(t, st, "b", b, t0.Add(6*time.Second), 0); got != OutcomePlay {
		t.Fatalf("session b must be unaffected: %s", got)
	}
}
