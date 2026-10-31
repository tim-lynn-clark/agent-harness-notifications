// Command cnw maps coding-agent events (Claude Code hooks, omp extension
// signals) to user-chosen sounds.
package main

import (
	"fmt"
	"os"
)

const usage = `usage:
  cnw hook                 handle one Claude Code hook event from stdin (used by hooks.json)
  cnw notify CATEGORY --session ID [--cwd DIR] [--pending] [--after MS]
                           schedule a category's sound; --pending lets resolve cancel it
  cnw resolve --session ID cancel the session's pending prompt sound
  cnw turn-end --session ID [--cwd DIR] [--error]
                           the agent finished; final reply on stdin picks done/done_question
  cnw play CATEGORY        play a category's configured sound now
  cnw init                 write the default config file if none exists
  cnw voices [play ID [CATEGORY]]
                           list the bundled voices, or hear one; kokoro_voice picks one
  cnw kokoro install [--from DIR] | remove
                           opt in to spoken names in the kokoro_voice voice (downloads ~376 MB)
  cnw doctor               check setup and show recent events
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "hook":
		runHook(os.Stdin)
		os.Exit(0) // never block or fail Claude Code
	case "fire":
		runFire(os.Args[2:])
	case "play":
		os.Exit(runPlay(os.Args[2:]))
	case "notify":
		os.Exit(runNotify(os.Args[2:]))
	case "resolve":
		os.Exit(runResolve(os.Args[2:]))
	case "turn-end":
		os.Exit(runTurnEnd(os.Args[2:], os.Stdin))
	case "init":
		os.Exit(runInit())
	case "voices":
		os.Exit(runVoices(os.Args[2:]))
	case "kokoro":
		os.Exit(runKokoro(os.Args[2:]))
	case "doctor":
		os.Exit(runDoctor())
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}
