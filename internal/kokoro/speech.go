package kokoro

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/wav"
)

// recordTimeout bounds one synthesis. A name takes about 2 s on an Apple
// silicon Mac; slower machines, and a first run scanned by antivirus, take
// longer.
const recordTimeout = 30 * time.Second

// Speech speaks with the Kokoro engine installed in Dir. It implements
// announce.Speech.
type Speech struct{ Dir string }

// Installed reports whether the engine is installed and voice is a supported
// Kokoro voice.
func (s Speech) Installed(voice string) bool { return Supported(voice) && Installed(s.Dir) }

// Record speaks text with voice into dest as 16-bit mono 24 kHz WAV, trimmed
// and levelled like the bundled clips.
func (s Speech) Record(voice, text, dest string) error {
	sid, ok := SpeakerID(voice)
	lex, english := lexicon(voice)
	if !ok || !english {
		return fmt.Errorf("Kokoro voice %q is not an English Kokoro v1.0 voice", voice)
	}
	raw := dest + ".raw.wav"
	defer os.Remove(raw)
	exe := filepath.Join(s.Dir, "bin", "sherpa-onnx-offline-tts")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	model := filepath.Join(s.Dir, "model")
	ctx, cancel := context.WithTimeout(context.Background(), recordTimeout)
	defer cancel()
	// "--" keeps a text starting with "-" from being read as an option.
	cmd := exec.CommandContext(ctx, exe,
		"--num-threads="+strconv.Itoa(min(runtime.NumCPU(), 4)),
		"--kokoro-model="+filepath.Join(model, "model.onnx"),
		"--kokoro-voices="+filepath.Join(model, "voices.bin"),
		"--kokoro-tokens="+filepath.Join(model, "tokens.txt"),
		"--kokoro-lexicon="+filepath.Join(model, lex),
		"--kokoro-data-dir="+filepath.Join(model, "espeak-ng-data"),
		"--sid="+strconv.Itoa(sid),
		"--output-filename="+raw,
		"--", text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", filepath.Base(exe), err, lastLine(out))
	}
	data, err := os.ReadFile(raw)
	if err != nil {
		return err
	}
	samples, rate, err := wav.Decode(data)
	if err != nil {
		return fmt.Errorf("engine output: %w", err)
	}
	return os.WriteFile(dest, wav.Encode(Level(samples, rate), rate), 0o600)
}

func lastLine(b []byte) string {
	lines := bytes.Split(bytes.TrimSpace(b), []byte("\n"))
	return string(bytes.TrimSpace(lines[len(lines)-1]))
}
