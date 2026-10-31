package main

import (
	"flag"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/announce"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/app"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/audio"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/category"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/config"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/kokoro"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/label"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/state"
)

// playLockWait bounds how long a fire waits for another agent's sound. A clip
// plus a spoken name can hold the lock for ~5 s, so the wait is well above it.
const playLockWait = 15 * time.Second

// spawnFire starts `cnw fire ARGS…` detached from the hook process, with no
// stdio, so Claude Code does not wait for it.
func spawnFire(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, append([]string{"fire"}, args...)...)
	cmd.SysProcAttr = detachedProcAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// runFire waits out the delay, computes the label and the announcement
// sequence (which may record a missing name with Kokoro or `say`), then takes
// the play lock, claims the sound, and plays the whole sequence under the lock.
// Recording before locking keeps the lock short. Locking before claiming
// means a lock timeout neither consumes the prompt nor starts the cooldown,
// and a prompt answered during the wait stays silent. No session lock is held
// while waiting for play.lock.
func runFire(args []string) {
	fs := flag.NewFlagSet("fire", flag.ContinueOnError)
	session := fs.String("session", "", "")
	seq := fs.Int64("seq", 0, "")
	cat := fs.String("category", "", "")
	delayMS := fs.Int64("delay", 0, "")
	cwd := fs.String("cwd", "", "")
	if err := fs.Parse(args); err != nil {
		return
	}
	time.Sleep(time.Duration(*delayMS) * time.Millisecond)

	st, err := openStore()
	if err != nil {
		return
	}
	rec := state.Record{Event: "fire", SessionID: *session, Result: *cat}
	defer func() {
		rec.Time = time.Now().Format(time.RFC3339Nano)
		_ = st.Log(rec)
	}()

	cfg, _, err := loadConfig()
	if err != nil {
		rec.Outcome = "config: " + err.Error()
		return
	}
	c := cfg.For(category.ID(*cat))
	files := []string{c.Sound}
	var note string
	if cfg.Announce != announce.ModeOff && *cwd != "" {
		l := label.For(*cwd, label.ExecGit).Aliased(cfg.Names)
		rec.Label = l.String()
		sp, voice := speechFor(cfg, st.Dir)
		files, note = announce.Sequence(cfg.Announce, voice, st.Dir, c.Sound, l, sp)
	}

	release, ok, err := st.AcquirePlayLock(playLockWait)
	if err != nil {
		rec.Outcome = "lock: " + err.Error()
		return
	}
	if !ok {
		rec.Outcome = "lock-timeout"
		return
	}
	defer release()

	outcome, err := app.Claim(st, *session, *seq, time.Now(), cfg.Cooldown)
	if err != nil {
		rec.Outcome = "state: " + err.Error()
		return
	}
	if outcome != app.OutcomePlay {
		rec.Outcome = outcome
		return
	}

	backend, err := audio.Detect(runtime.GOOS, exec.LookPath)
	if err != nil {
		rec.Outcome = err.Error()
		return
	}
	// The play lock is held for the whole sequence, so announcements from two
	// agents never interleave. Only the clip's failure fails the sound.
	for _, f := range files {
		if err := audio.Play(backend, f, c.Volume); err != nil {
			if f == c.Sound {
				rec.Outcome = "play: " + err.Error()
				return
			}
			note = "identity: " + err.Error()
		}
	}
	rec.Outcome = "played"
	if note != "" {
		rec.Outcome += " (" + note + ")"
	}
}

// speechFor picks the text-to-speech for spoken names and the voice it
// speaks with: Kokoro, once installed (`cnw kokoro install`), in kokoro_voice,
// the bundled clips' voice; else `say` on macOS with the configured voice;
// else nil, which means chimes.
func speechFor(cfg *config.Config, stateDir string) (announce.Speech, string) {
	if dir := kokoro.Dir(stateDir); kokoro.Installed(dir) {
		return kokoro.Speech{Dir: dir}, cfg.KokoroVoice
	}
	if runtime.GOOS == "darwin" {
		return announce.Say{}, cfg.Voice
	}
	return nil, cfg.Voice
}
