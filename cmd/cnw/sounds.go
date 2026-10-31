package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/config"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/kokoro"
)

// loadConfig loads the user's config, points every category without a
// configured sound at the bundled clips of its kokoro_voice, and fails if that
// voice has no bundled clips (voice selection spec §6).
func loadConfig() (*config.Config, []string, error) {
	cfg, warnings, err := config.Load(config.Path())
	if err != nil {
		return nil, warnings, err
	}
	cfg.BundledDir = soundsDir()
	if err := checkBundledVoice(cfg.BundledDir, cfg.KokoroVoice); err != nil {
		return nil, warnings, err
	}
	return cfg, warnings, nil
}

// bundledVoices lists the voices with bundled clips: the directories under
// bundledDir named after a supported Kokoro voice, in name order.
func bundledVoices(bundledDir string) ([]string, error) {
	entries, err := os.ReadDir(bundledDir)
	if err != nil {
		return nil, err
	}
	var voices []string
	for _, e := range entries {
		if e.IsDir() && kokoro.Supported(e.Name()) {
			voices = append(voices, e.Name())
		}
	}
	return voices, nil
}

// checkBundledVoice fails unless voice has bundled clips. With no bundled
// directory there are no bundled clips to choose from, so nothing to check.
func checkBundledVoice(bundledDir, voice string) error {
	if bundledDir == "" {
		return nil
	}
	voices, err := bundledVoices(bundledDir)
	if err != nil {
		return fmt.Errorf("bundled clips: %w", err)
	}
	if !slices.Contains(voices, voice) {
		return fmt.Errorf("kokoro_voice %q has no bundled clips; available: %s", voice, strings.Join(voices, ", "))
	}
	return nil
}

// soundsDir finds the bundled clips; tests replace it.
var soundsDir = bundledSoundsDir

// bundledSoundsDir is <plugin root>/sounds. The binary lives at
// <plugin root>/bin/<os>-<arch>/cnw, so the root is two levels up.
func bundledSoundsDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Join(filepath.Dir(exe), "..", "..", "sounds")
}
