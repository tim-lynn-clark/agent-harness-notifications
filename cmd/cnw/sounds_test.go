package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/category"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/config"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/kokoro"
)

func TestBundledVoicesListsVoiceDirectoriesOnly(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"bf_emma", "af_heart", "xx_bogus", "ef_dora"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "am_puck"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := bundledVoices(dir)
	if err != nil {
		t.Fatal(err)
	}
	// xx_bogus isn't a Kokoro voice, ef_dora isn't English, am_puck is a file.
	if want := []string{"af_heart", "bf_emma"}; !slices.Equal(got, want) {
		t.Fatalf("bundledVoices = %v, want %v", got, want)
	}

	if err := checkBundledVoice(dir, "bf_emma"); err != nil {
		t.Fatalf("shipped voice rejected: %v", err)
	}
	err = checkBundledVoice(dir, "am_puck")
	if err == nil || !strings.Contains(err.Error(), "af_heart, bf_emma") {
		t.Fatalf("missing voice must fail and list the available ones: %v", err)
	}
	if err := checkBundledVoice("", "am_puck"); err != nil {
		t.Fatalf("no bundled dir means no bundled clips to check: %v", err)
	}
}

// The repository's sounds/ holds one directory per shipped voice, each with
// every category's clip, and the default voice ships.
func TestRepositorySoundsLayout(t *testing.T) {
	const dir = "../../sounds"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue // .DS_Store and friends; never committed
		}
		if !e.IsDir() || !kokoro.Supported(e.Name()) {
			t.Errorf("sounds/%s: only directories named after an English Kokoro voice belong here", e.Name())
			continue
		}
		for _, id := range category.All() {
			if _, err := os.Stat(filepath.Join(dir, e.Name(), string(id)+".wav")); err != nil {
				t.Errorf("sounds/%s: %v", e.Name(), err)
			}
		}
	}
	if err := checkBundledVoice(dir, config.DefaultKokoroVoice); err != nil {
		t.Error(err)
	}
}
