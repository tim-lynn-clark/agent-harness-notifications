package main

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/tim-lynn-clark/agent-harness-notifications/internal/category"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/config"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/jump"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/label"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/state"
	"github.com/tim-lynn-clark/agent-harness-notifications/internal/visual"
)

// refreshVisual rewrites the visual snapshot after an attention change. Like
// sounds, it never fails the caller. It always rewrites, with built-in
// defaults when the config can't be loaded, so a cleared session never lingers
// in the snapshot. It reads the config directly: the snapshot needs no sounds.
func refreshVisual(st state.Store) {
	cfg, _, err := config.Load(config.Path())
	if err != nil {
		cfg = config.Default()
	}
	_ = visual.Write(st, cfg, time.Now())
}

const visualUsage = `usage:
  cnw visual install       show waiting sessions on screen and in the menu bar (macOS)
  cnw visual uninstall     remove it
  cnw visual preview [CATEGORY]
                           run the whole escalation in 12 s (default: permission)
`

func runVisual(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, visualUsage)
		return 2
	}
	if runtime.GOOS != "darwin" {
		fmt.Fprintln(os.Stderr, "cnw visual: only macOS is supported so far")
		return 1
	}
	switch args[0] {
	case "host":
		return runVisualHost()
	case "jump":
		return runVisualJump(args[1:])
	case "preview":
		return runVisualPreview(args[1:])
	}
	fmt.Fprint(os.Stderr, visualUsage)
	return 2
}

// runVisualHost replaces this process with the JXA host (spec §5.1).
func runVisualHost() int {
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	script, err := visual.HostScript(st.Dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	err = execReplace("/usr/bin/osascript", []string{"osascript", "-l", "JavaScript", script, st.Dir, exe})
	fmt.Fprintln(os.Stderr, "cnw visual host:", err)
	return 1
}

// runVisualJump brings a waiting session's terminal to the front (spec §6.2).
func runVisualJump(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: cnw visual jump SESSION_ID")
		return 2
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	s, err := st.Load(args[0])
	if err != nil || s.Attention == nil {
		fmt.Fprintln(os.Stderr, "cnw visual jump: session is not waiting")
		return 1
	}
	if err := jump.Run(s.Attention.Jump, jump.Exec); err != nil {
		fmt.Fprintln(os.Stderr, "cnw visual jump:", err)
		return 1
	}
	return 0
}

func runVisualPreview(args []string) int {
	id := category.Permission
	if len(args) > 0 {
		id = category.ID(args[0])
	}
	if _, ok := category.DefaultFor(id); !ok {
		fmt.Fprintf(os.Stderr, "unknown category %q\n", id)
		return 2
	}
	cfg, _, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cwd, _ := os.Getwd()
	c := cfg.For(id)
	p := visual.Preview{
		Category: string(id), Label: label.For(cwd, label.ExecGit).Aliased(cfg.Names).String(),
		Color: c.Color, MaxRung: c.MaxRung, StartedMS: time.Now().UnixMilli(),
	}
	if err := visual.WritePreview(st.Dir, p); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !visual.HostAlive(st.Dir, time.Now()) {
		fmt.Println("the visual host isn't running: run `cnw visual install` first")
		return 1
	}
	fmt.Printf("previewing %s for 12 s: glow, halo at 3 s, critter at 6 s (highest rung %d)\n", id, c.MaxRung)
	return 0
}
