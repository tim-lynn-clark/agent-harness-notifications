package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

func TestWriteVoicesMarksCurrentAndDescribesIDs(t *testing.T) {
	var b bytes.Buffer
	writeVoices(&b, []string{"af_heart", "am_puck", "bf_emma", "bm_george"}, "bf_emma")
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	want := [][]string{
		{"af_heart", "US", "English", "female"},
		{"am_puck", "US", "English", "male"},
		{"*", "bf_emma", "GB", "English", "female"},
		{"bm_george", "GB", "English", "male"},
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines: %q", len(lines), b.String())
	}
	for i, line := range lines {
		if got := strings.Fields(line); !slices.Equal(got, want[i]) {
			t.Errorf("line %d = %q, want fields %v", i, line, want[i])
		}
	}
	// One column for the marker, so ids line up whether or not they're current.
	if strings.Index(lines[0], "af_heart") != strings.Index(lines[2], "bf_emma") {
		t.Errorf("ids not aligned:\n%s", b.String())
	}
}
