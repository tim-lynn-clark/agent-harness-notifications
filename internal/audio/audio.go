// Package audio plays a sound file through the platform's command-line
// player (spec §9).
package audio

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// winPlayerArgv is the Windows PowerShell player, used both natively and
// through WSL interop. prog is "powershell" or "powershell.exe".
func winPlayerArgv(prog, p string) []string {
	quoted := "'" + strings.ReplaceAll(p, "'", "''") + "'"
	return []string{prog, "-NoProfile", "-NonInteractive", "-Command",
		"(New-Object Media.SoundPlayer " + quoted + ").PlaySync()"}
}

// WSLBackend is the name of the Linux fallback that plays through Windows.
const WSLBackend = "powershell.exe"

// Backend is one external player.
type Backend struct {
	Name   string
	Volume bool // whether the backend honours the volume argument
	// Convert maps a local path to one the player understands; nil means none.
	Convert func(path string) (string, error)
	argv    func(path string, volume float64) []string
}

// Command returns the full argv that plays path at volume (0.0-1.0).
func (b Backend) Command(path string, volume float64) []string { return b.argv(path, volume) }

var backends = map[string][]Backend{
	"darwin": {
		{Name: "afplay", Volume: true, argv: func(p string, v float64) []string {
			return []string{"afplay", "-v", fmt.Sprintf("%.2f", v), p}
		}},
	},
	"linux": {
		{Name: "pw-play", Volume: true, argv: func(p string, v float64) []string {
			return []string{"pw-play", "--volume", fmt.Sprintf("%.2f", v), p}
		}},
		{Name: "paplay", Volume: true, argv: func(p string, v float64) []string {
			return []string{"paplay", fmt.Sprintf("--volume=%d", int(v*65536)), p}
		}},
		{Name: "aplay", Volume: false, argv: func(p string, _ float64) []string {
			return []string{"aplay", "-q", p}
		}},
		// WSL interop: WSLg ships a PulseAudio server but the stock images
		// ship no player, so fall back to Windows' own (issue #8).
		{Name: WSLBackend, Volume: false, Convert: wslPath, argv: func(p string, _ float64) []string {
			return winPlayerArgv(WSLBackend, p)
		}},
	},
	"windows": {
		{Name: "powershell", Volume: false, argv: func(p string, _ float64) []string {
			return winPlayerArgv("powershell", p)
		}},
	},
}

// wslPath converts a Linux path to its Windows form via wslpath(1).
func wslPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	out, err := exec.Command("wslpath", "-w", abs).Output()
	if err != nil {
		return "", fmt.Errorf("wslpath -w %s: %w", abs, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// IsWSL reports whether this Linux process runs under WSL.
func IsWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	v, err := os.ReadFile("/proc/version")
	return err == nil && strings.Contains(strings.ToLower(string(v)), "microsoft")
}

// ErrNoBackend means no supported player was found on PATH.
var ErrNoBackend = errors.New("no audio backend found on PATH")

// Detect returns the first backend for goos whose program lookPath finds.
func Detect(goos string, lookPath func(string) (string, error)) (Backend, error) {
	for _, b := range backends[goos] {
		if _, err := lookPath(b.Name); err == nil {
			return b, nil
		}
	}
	return Backend{}, ErrNoBackend
}

// Play plays path with b and waits for playback to finish.
func Play(b Backend, path string, volume float64) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("sound file: %w", err)
	}
	if b.Convert != nil {
		converted, err := b.Convert(path)
		if err != nil {
			return fmt.Errorf("%s: %w", b.Name, err)
		}
		path = converted
	}
	argv := b.Command(path, volume)
	out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", b.Name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
