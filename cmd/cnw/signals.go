package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/app"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/category"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/classify"
)

// maxReplyBytes caps the final reply read by turn-end; only its last
// paragraph matters (spec §5.2).
const maxReplyBytes = 1 << 20

// Direct signals let agents other than Claude Code (omp) drive cnw without
// Claude-shaped hook events: they report prompts and their resolution
// themselves. Like `cnw hook`, each command writes nothing to stdout.

// runNotify implements `cnw notify CATEGORY --session ID [--cwd DIR] [--pending] [--after MS]`.
// --pending registers a prompt that `cnw resolve` can cancel before it plays.
func runNotify(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: cnw notify CATEGORY --session ID [--cwd DIR] [--pending] [--after MS]")
		return 2
	}
	id := category.ID(args[0])
	if _, ok := category.DefaultFor(id); !ok {
		fmt.Fprintf(os.Stderr, "unknown category %q\n", args[0])
		return 2
	}
	fs := flag.NewFlagSet("notify", flag.ContinueOnError)
	session := fs.String("session", "", "session id")
	cwd := fs.String("cwd", "", "session working directory, for the spoken project/worktree")
	pending := fs.Bool("pending", false, "register a prompt that resolve can cancel")
	afterMS := fs.Int64("after", 0, "extra wait in milliseconds before the category's delay_ms")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if *session == "" || *afterMS < 0 {
		fmt.Fprintln(os.Stderr, "notify: --session is required and --after must be >= 0")
		return 2
	}

	now := time.Now()
	st, err := openStore()
	if err != nil {
		return 1
	}
	rec := newRecord(now)
	rec.Event, rec.SessionID, rec.Result = "notify", *session, string(id)
	defer func() { _ = st.Log(rec) }()

	var seq int64
	if *pending {
		if seq, err = app.Register(st, *session, id, now); err != nil {
			rec.Outcome = "state: " + err.Error()
			return 1
		}
	}
	schedule(&rec, *session, *cwd, seq, id, time.Duration(*afterMS)*time.Millisecond)
	return 0
}

// runResolve implements `cnw resolve --session ID`: the session's pending
// prompt was answered, so its scheduled sound must not play.
func runResolve(args []string) int {
	fs := flag.NewFlagSet("resolve", flag.ContinueOnError)
	session := fs.String("session", "", "session id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *session == "" {
		fmt.Fprintln(os.Stderr, "resolve: --session is required")
		return 2
	}
	st, err := openStore()
	if err != nil {
		return 1
	}
	rec := newRecord(time.Now())
	rec.Event, rec.SessionID, rec.Result = "resolve", *session, "cancel"
	defer func() { _ = st.Log(rec) }()
	if err := app.Resolve(st, *session); err != nil {
		rec.Outcome = "state: " + err.Error()
		return 1
	}
	return 0
}

// runTurnEnd implements `cnw turn-end --session ID [--cwd DIR] [--error]`, with the
// agent's final reply on stdin. It cancels any pending prompt and plays
// error, done_question (reply ends with a question, spec §5.2), or done.
func runTurnEnd(args []string, stdin io.Reader) int {
	fs := flag.NewFlagSet("turn-end", flag.ContinueOnError)
	session := fs.String("session", "", "session id")
	cwd := fs.String("cwd", "", "session working directory, for the spoken project/worktree")
	failed := fs.Bool("error", false, "the turn ended on an error")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *session == "" {
		fmt.Fprintln(os.Stderr, "turn-end: --session is required")
		return 2
	}
	reply, err := io.ReadAll(io.LimitReader(stdin, maxReplyBytes))
	if err != nil && !errors.Is(err, io.EOF) {
		reply = nil
	}

	now := time.Now()
	st, err := openStore()
	if err != nil {
		return 1
	}
	rec := newRecord(now)
	rec.Event, rec.SessionID = "turn-end", *session
	defer func() { _ = st.Log(rec) }()

	id := category.Done
	switch {
	case *failed:
		id = category.Error
	case classify.EndsWithQuestion(string(reply)):
		id = category.DoneQuestion
	}
	rec.Result = string(id)
	if err := app.Resolve(st, *session); err != nil {
		rec.Outcome = "state: " + err.Error()
		return 1
	}
	schedule(&rec, *session, *cwd, 0, id, 0)
	return 0
}
