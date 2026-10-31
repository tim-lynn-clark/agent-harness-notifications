package announce

import (
	"encoding/binary"
	"reflect"
	"testing"
)

func TestChimeIndex(t *testing.T) {
	seen := map[int]bool{}
	for _, p := range []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta", "iota", "kappa"} {
		n := ChimeIndex(p)
		if n < 0 || n >= ChimeCount {
			t.Fatalf("ChimeIndex(%q) = %d out of range", p, n)
		}
		if n != ChimeIndex(p) {
			t.Fatalf("ChimeIndex(%q) is not stable", p)
		}
		seen[n] = true
	}
	if len(seen) < 3 {
		t.Fatalf("10 projects landed on only %d chimes", len(seen))
	}
}

func TestChimeWAVFormat(t *testing.T) {
	for n := 0; n < ChimeCount; n++ {
		w := ChimeWAV(n)
		le := binary.LittleEndian
		if string(w[0:4]) != "RIFF" || string(w[8:16]) != "WAVEfmt " || string(w[36:40]) != "data" {
			t.Fatalf("chime %d: bad RIFF/WAVE layout", n)
		}
		if le.Uint16(w[20:]) != 1 || le.Uint16(w[22:]) != 1 || le.Uint32(w[24:]) != SampleRate || le.Uint16(w[34:]) != 16 {
			t.Fatalf("chime %d: not PCM mono 22.05 kHz 16-bit", n)
		}
		dataLen := le.Uint32(w[40:])
		if int(dataLen) != len(w)-44 || le.Uint32(w[4:]) != 36+dataLen {
			t.Fatalf("chime %d: size fields disagree with the payload", n)
		}
		if ms := 1000 * int(dataLen) / 2 / SampleRate; ms < 340 || ms > 360 {
			t.Fatalf("chime %d lasts %d ms, want ~350", n, ms)
		}
	}
	if reflect.DeepEqual(ChimeWAV(0), ChimeWAV(1)) {
		t.Fatal("chimes must differ")
	}
}
