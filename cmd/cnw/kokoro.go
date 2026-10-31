package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/announce"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/audio"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/kokoro"
)

const kokoroUsage = `usage:
  cnw kokoro install [--from DIR]   download Kokoro and speak names in the kokoro_voice voice;
                                    --from reads the two archives from DIR instead of downloading
  cnw kokoro urls                   print the archive URLs for an offline --from install
  cnw kokoro remove                 delete the Kokoro install
`

// runKokoro installs or removes the opt-in Kokoro speech for names.
func runKokoro(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, kokoroUsage)
		return 2
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	dir := kokoro.Dir(st.Dir)
	switch args[0] {
	case "install":
		fs := flag.NewFlagSet("kokoro install", flag.ContinueOnError)
		from := fs.String("from", "", "")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			fmt.Fprint(os.Stderr, kokoroUsage)
			return 2
		}
		return kokoroInstall(st.Dir, dir, *from)
	case "urls":
		if len(args) != 1 {
			fmt.Fprint(os.Stderr, kokoroUsage)
			return 2
		}
		assets, err := kokoro.Assets(runtime.GOOS, runtime.GOARCH)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		for _, a := range assets {
			fmt.Println(a.URL)
		}
		return 0
	case "remove":
		if len(args) != 1 {
			fmt.Fprint(os.Stderr, kokoroUsage)
			return 2
		}
		if err := kokoro.Remove(dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("removed %s\n", dir)
		return 0
	}
	fmt.Fprint(os.Stderr, kokoroUsage)
	return 2
}

func kokoroInstall(stateDir, dir, from string) int {
	cfg, _, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	voice := cfg.KokoroVoice
	if kokoro.Installed(dir) {
		fmt.Printf("Kokoro is already installed in %s\n", dir)
		return 0
	}
	assets, err := kokoro.Assets(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if from == "" {
		var total int64
		for _, a := range assets {
			total += a.Size
		}
		fmt.Printf("Downloading the sherpa-onnx %s engine and the %s model (%d MB) into %s\n",
			kokoro.EngineVersion, kokoro.ModelName, total/1_000_000, dir)
	}
	if err := kokoro.Install(dir, from, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "install failed: %v\n\n", err)
		fmt.Fprintln(os.Stderr, "If downloads are blocked, fetch these files with a browser into one folder:")
		for _, a := range assets {
			fmt.Fprintf(os.Stderr, "  %s\n", a.URL)
		}
		fmt.Fprintln(os.Stderr, "then run: cnw kokoro install --from <folder>")
		return 1
	}

	// Names cached by an earlier install were recorded by that engine, so
	// drop them; the next announcement records them again.
	if err := announce.ClearNames(stateDir, kokoro.English()...); err != nil {
		fmt.Fprintf(os.Stderr, "clearing cached names: %v\n", err)
	}

	// Synthesise once now, so a blocked executable (AppLocker, WDAC) or a
	// missing library shows up here rather than as chimes later.
	check := filepath.Join(dir, "check.wav")
	defer os.Remove(check)
	start := time.Now()
	if err := (kokoro.Speech{Dir: dir}).Record(voice, "Spoken names now use this voice.", check); err != nil {
		kokoro.Remove(dir)
		fmt.Fprintf(os.Stderr, "the engine does not run on this machine, so it was removed: %v\n", err)
		return 1
	}
	fmt.Printf("Installed in %s. Names are spoken in voice %s (kokoro_voice); a test phrase took %.1f s.\n",
		dir, voice, time.Since(start).Seconds())
	if backend, err := audio.Detect(runtime.GOOS, exec.LookPath); err == nil {
		if err := audio.Play(backend, check, 1); err != nil {
			fmt.Fprintf(os.Stderr, "playing the test phrase: %v\n", err)
		}
	}
	return 0
}
