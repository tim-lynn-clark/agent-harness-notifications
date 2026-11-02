// Command hostscript prints the visual host script exactly as HostScript
// writes it (host.js followed by critters.js), so CI can compile it:
//
//	go run ./scripts/hostscript > host.js && osacompile -l JavaScript -o host.scpt host.js
package main

import (
	"os"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/visual"
)

func main() {
	if _, err := os.Stdout.Write(visual.Script()); err != nil {
		os.Exit(1)
	}
}
