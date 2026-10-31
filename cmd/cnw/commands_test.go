package main

import "testing"

func TestVersionGate(t *testing.T) {
	cases := []struct {
		out  string
		ok   bool
		pass bool
	}{
		{"2.1.301 (Claude Code)", true, true},
		{"2.1.288 (Claude Code)", true, true},
		{"2.1.287 (Claude Code)", true, false},
		{"2.0.999", true, false},
		{"3.0.0", true, true},
		{"garbage", false, false},
	}
	for _, c := range cases {
		v, ok := parseVersion(c.out)
		if ok != c.ok {
			t.Fatalf("%q: parse ok=%v", c.out, ok)
		}
		if ok && atLeast(v, MinClaudeVersion) != c.pass {
			t.Fatalf("%q: atLeast=%v want %v", c.out, !c.pass, c.pass)
		}
	}
}
