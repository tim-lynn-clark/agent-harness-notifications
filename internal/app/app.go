// Package app joins classification and session state: Apply records a hook
// event, Claim decides whether a scheduled sound may play (spec §6).
package app

import (
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/category"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/classify"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/event"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/state"
)

// Apply classifies e against the session's history, applies its cancellation,
// and records prompt timestamps. A registering result (PermissionRequest,
// Elicitation) stores a new pending prompt and schedules nothing (seq 0). A
// Notification whose category matches the registered prompt triggers its
// sound and returns that prompt's seq, so the sound stays cancellable and
// Claim consumes it. Every other result returns seq 0.
func Apply(st state.Store, e event.Event, now time.Time) (res classify.Result, seq int64, err error) {
	_, err = st.Update(e.SessionID, func(s *state.Session) {
		r := classify.Recent{
			LastPermission:  fromMS(s.LastPermissionMS),
			LastElicitation: fromMS(s.LastElicitationMS),
		}
		if s.Pending != nil {
			r.Pending = category.ID(s.Pending.Category)
		}
		res = classify.Classify(e, now, r)

		switch res.Cancel {
		case classify.CancelAll:
			s.Pending = nil
			s.Attention = nil
			raiseCleared(s, now)
		case classify.CancelMatchingTool:
			if s.Pending != nil && s.Pending.ToolName == e.ToolName && s.Pending.AgentID == e.AgentID {
				s.Pending = nil
			}
			if a := s.Attention; a != nil && a.ToolName != "" && a.ToolName == e.ToolName && a.AgentID == e.AgentID {
				s.Attention = nil
			}
		}

		switch e.HookEventName {
		case "PermissionRequest":
			s.LastPermissionMS = now.UnixMilli()
		case "Elicitation":
			s.LastElicitationMS = now.UnixMilli()
		}

		switch {
		case res.Register:
			s.NextSeq++
			s.Pending = &state.Pending{
				Seq:       s.NextSeq,
				Category:  string(res.Category),
				ToolName:  e.ToolName,
				AgentID:   e.AgentID,
				CreatedMS: now.UnixMilli(),
			}
		case res.Category != "" && e.HookEventName == "Notification" &&
			s.Pending != nil && s.Pending.Category == string(res.Category):
			seq = s.Pending.Seq
		}
	})
	return res, seq, err
}

// Register records a pending prompt of category c for sessionID, replacing any
// earlier one, and returns its seq. Agents that report prompt resolution
// directly (omp) use Register/Resolve instead of Claude Code's hook events.
func Register(st state.Store, sessionID string, c category.ID, now time.Time) (int64, error) {
	var seq int64
	_, err := st.Update(sessionID, func(s *state.Session) {
		s.NextSeq++
		seq = s.NextSeq
		s.Pending = &state.Pending{Seq: seq, Category: string(c), CreatedMS: now.UnixMilli()}
	})
	return seq, err
}

// Resolve clears sessionID's pending prompt, so a sound scheduled for it stays
// silent, and its attention, because the user has acted.
func Resolve(st state.Store, sessionID string, now time.Time) error {
	_, err := st.Update(sessionID, func(s *state.Session) {
		s.Pending = nil
		s.Attention = nil
		raiseCleared(s, now)
	})
	return err
}

// Outcomes reported by Claim.
const (
	OutcomePlay      = "play"
	OutcomeCancelled = "cancelled"
	OutcomeCooldown  = "cooldown"
)

// Claim is called by the detached player after its delay. For seq > 0 it
// plays only if that registered prompt is still current, and consumes it. Every
// sound respects the per-session cooldown. On OutcomePlay the play time is
// recorded. On OutcomePlay and OutcomeCooldown the session is marked as
// waiting on the user with att (visual attention spec §4.2).
func Claim(st state.Store, sessionID string, seq int64, now time.Time, cooldown time.Duration, att state.Attention) (string, error) {
	outcome := OutcomePlay
	_, err := st.Update(sessionID, func(s *state.Session) {
		if seq > 0 {
			if s.Pending == nil || s.Pending.Seq != seq {
				outcome = OutcomeCancelled
				return
			}
			att.ToolName, att.AgentID = s.Pending.ToolName, s.Pending.AgentID
			s.Pending = nil
		}
		markAttention(s, sessionID, att)
		if s.LastPlayedMS != 0 && now.Sub(fromMS(s.LastPlayedMS)) < cooldown {
			outcome = OutcomeCooldown
			return
		}
		s.LastPlayedMS = now.UnixMilli()
	})
	return outcome, err
}

// MarkWaiting marks sessionID as waiting on the user with att when its sound
// could not play (the play lock timed out or failed). Like Claim, for seq > 0
// it acts only if that prompt is still current, but it neither consumes the
// prompt nor starts the cooldown, so a later fire of the same seq still plays.
func MarkWaiting(st state.Store, sessionID string, seq int64, att state.Attention) error {
	_, err := st.Update(sessionID, func(s *state.Session) {
		if seq > 0 {
			if s.Pending == nil || s.Pending.Seq != seq {
				return
			}
			att.ToolName, att.AgentID = s.Pending.ToolName, s.Pending.AgentID
		}
		markAttention(s, sessionID, att)
	})
	return err
}

// raiseCleared records that the user acted at now. It never moves the mark
// backwards, so a clear handled late cannot let an older event's attention in.
func raiseCleared(s *state.Session, now time.Time) {
	if ms := now.UnixMilli(); ms > s.AttentionClearedMS {
		s.AttentionClearedMS = ms
	}
}

// markAttention records att unless the user acted after the triggering event,
// and lets an idle reminder keep an existing attention so its ladder goes on.
func markAttention(s *state.Session, sessionID string, att state.Attention) {
	if att.Category == "" || s.AttentionClearedMS > att.SinceMS {
		return
	}
	if att.Category == string(category.IdleReminder) && s.Attention != nil {
		return
	}
	att.SessionID = sessionID
	s.Attention = &att
}

func fromMS(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}
