package announce

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Say is the macOS `say` Speech.
type Say struct{}

// sayVoiceLine matches a `say -v '?'` line: name, whitespace, locale token,
// whitespace, `#`. Names of 19+ characters are followed by a single space.
var sayVoiceLine = regexp.MustCompile(`^(.+?)\s+\S+\s+#`)

// Installed reports whether `say -v '?'` lists voice exactly. A listing that
// takes longer than 3 s counts as not installed.
func (Say) Installed(voice string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "say", "-v", "?").Output()
	return err == nil && VoiceListed(string(out), voice)
}

// Record speaks text with voice into dest as 16-bit mono 22.05 kHz WAV. "--"
// keeps a text starting with "-" from being read as an option. A recording
// that takes longer than 10 s fails.
func (Say) Record(voice, text, dest string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "say", "-v", voice, "-o", dest,
		"--file-format=WAVE", "--data-format=LEI16@22050", "--", text).Run()
}

// VoiceListed reports whether voice is one of the names in list, the output of
// `say -v '?'`. "Moira" does not match "Moira (Enhanced)".
func VoiceListed(list, voice string) bool {
	for _, line := range strings.Split(list, "\n") {
		if m := sayVoiceLine.FindStringSubmatch(line); m != nil && m[1] == voice {
			return true
		}
	}
	return false
}
