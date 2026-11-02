package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
// A failed write is logged to events.log.
func refreshVisual(st state.Store) {
	cfg, _, err := config.Load(config.Path())
	if err != nil {
		cfg = config.Default()
	}
	now := time.Now()
	if err := visual.Write(st, cfg, now); err != nil {
		rec := newRecord(now)
		rec.Event, rec.Outcome = "visual", "error: "+err.Error()
		_ = st.Log(rec)
	}
}

const visualUsage = `usage:
  cnw visual install       show waiting sessions on screen and in the menu bar (macOS)
  cnw visual uninstall     remove it
  cnw visual critters      list the desk critters; * marks the configured one
  cnw visual preview [CATEGORY] [--critter ID]
                           run the whole escalation in 12 s (default: permission,
                           the configured critter)
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
	case "critters":
		return runVisualCritters()
	case "host":
		return runVisualHost()
	case "jump":
		return runVisualJump(args[1:])
	case "preview":
		return runVisualPreview(args[1:])
	case "install":
		return runVisualInstall()
	case "uninstall":
		return runVisualUninstall()
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
	if err != nil {
		fmt.Fprintln(os.Stderr, "cnw visual jump:", err)
		return 1
	}
	if s.Attention == nil {
		fmt.Fprintln(os.Stderr, "cnw visual jump: session is not waiting")
		return 1
	}
	if err := jump.Run(s.Attention.Jump, jump.Exec); err != nil {
		fmt.Fprintln(os.Stderr, "cnw visual jump:", err)
		return 1
	}
	return 0
}

// parsePreviewArgs reads `[CATEGORY] [--critter ID]` in either order; critter
// is "" when not given.
func parsePreviewArgs(args []string) (id category.ID, critter string, err error) {
	id = category.Permission
	seenCategory := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--critter":
			if i+1 >= len(args) {
				return "", "", errors.New("--critter needs an id (list: cnw visual critters)")
			}
			i++
			critter = args[i]
		case strings.HasPrefix(a, "--critter="):
			critter = strings.TrimPrefix(a, "--critter=")
		case strings.HasPrefix(a, "-") || seenCategory:
			return "", "", fmt.Errorf("unexpected argument %q", a)
		default:
			id, seenCategory = category.ID(a), true
		}
	}
	if _, ok := category.DefaultFor(id); !ok {
		return "", "", fmt.Errorf("unknown category %q", id)
	}
	if critter != "" && !config.ValidCritter(critter) {
		return "", "", fmt.Errorf("unknown critter %q, not one of: %s", critter, strings.Join(config.CritterIDs(), ", "))
	}
	return id, critter, nil
}

func runVisualPreview(args []string) int {
	id, critter, err := parsePreviewArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprint(os.Stderr, visualUsage)
		return 2
	}
	// An explicit --critter must work while the configured critter is the
	// invalid value being replaced, so a broken config falls back to the
	// defaults with a warning.
	cfg, _, err := loadConfig()
	if err != nil {
		if critter == "" {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "warning: %v (previewing with the default settings)\n", err)
		cfg = config.Default()
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !visual.HostAlive(st.Dir, time.Now()) {
		fmt.Fprintln(os.Stderr, "the visual host isn't running: run `cnw visual install` first")
		return 1
	}
	if critter == "" {
		critter = cfg.Critter
	}
	cwd, _ := os.Getwd()
	c := cfg.For(id)
	p := visual.Preview{
		Category: string(id), Label: label.For(cwd, label.ExecGit).Aliased(cfg.Names).String(),
		Color: c.Color, MaxRung: c.MaxRung, Critter: critter, StartedMS: time.Now().UnixMilli(),
	}
	if err := visual.WritePreview(st.Dir, p); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("previewing %s for 12 s: glow, halo at 3 s, critter (%s) at 6 s (highest rung %d)\n", id, critter, c.MaxRung)
	return 0
}

// runVisualCritters lists the critters (spec §8). A config that fails to load
// still lists them, with none marked.
func runVisualCritters() int {
	cfg, _, err := loadConfig()
	current := ""
	if err == nil {
		current = cfg.Critter
	}
	writeCritters(os.Stdout, current)
	if err != nil {
		fmt.Printf("config: %v\n", err)
	} else {
		fmt.Printf("Current: %s (critter under [visual] in %s).\n", current, config.Path())
	}
	fmt.Println("Preview: cnw visual preview [CATEGORY] --critter ID")
	return 0
}

// writeCritters prints one line per critter, with "*" marking current.
func writeCritters(w io.Writer, current string) {
	for _, c := range config.Critters {
		mark := " "
		if c.ID == current {
			mark = "*"
		}
		fmt.Fprintf(w, "%s %-8s %s\n", mark, c.ID, c.Description)
	}
}

func runVisualInstall() int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := visual.Install(home, st.Dir, exe, jump.Exec); err != nil {
		fmt.Fprintln(os.Stderr, "cnw visual install:", err)
		return 1
	}
	refreshVisual(st)
	fmt.Printf("installed %s\nthe orb is in the menu bar; try: cnw visual preview\n", visual.PlistPath(home))
	return 0
}

func runVisualUninstall() int {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := visual.Uninstall(home, st.Dir, jump.Exec); err != nil {
		fmt.Fprintln(os.Stderr, "cnw visual uninstall:", err)
		return 1
	}
	fmt.Println("removed the visual signal")
	return 0
}

// healVisual re-points an installed host at this binary after a plugin
// upgrade. It never fails the caller.
func healVisual(st state.Store) {
	if runtime.GOOS != "darwin" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	_, _ = visual.Heal(home, st.Dir, exe, jump.Exec)
}

// reportVisual adds the visual signal's doctor lines (spec §9).
func reportVisual(report func(status, format string, a ...any)) {
	st, err := openStore()
	if err != nil {
		return
	}
	if _, err := exec.LookPath("osascript"); err != nil {
		report("FAIL", "visual: osascript not found")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		report("FAIL", "visual: %v", err)
		return
	}
	// The plist is checked even without visual.installed here: an install made
	// with another CNW_STATE_DIR records it only in that directory.
	p, err := visual.ReadPlist(home)
	if _, installed := visual.InstalledPath(st.Dir); !installed && errors.Is(err, fs.ErrNotExist) {
		report("INFO", "visual signal not installed (`cnw visual install` to enable)")
		return
	}
	if err != nil {
		report("FAIL", "visual: can't read the LaunchAgent (run `cnw visual install`): %v", err)
		return
	}
	if _, err := os.Stat(p.Exe); err != nil {
		report("FAIL", "visual: the LaunchAgent runs %s, which no longer exists (run `cnw visual install`)", p.Exe)
	} else if exe, err := os.Executable(); err == nil && exe != p.Exe {
		report("INFO", "visual: the LaunchAgent runs %s (another cnw install)", p.Exe)
	}
	if p.StateDir != st.Dir {
		report("FAIL", "visual: the LaunchAgent watches %s, but hooks write %s (run `cnw visual install`)", p.StateDir, st.Dir)
	}
	if visual.HostAlive(p.StateDir, time.Now()) {
		report("OK", "visual host running")
	} else if visual.QuitFromMenu(jump.Exec) {
		report("INFO", "visual host was quit from its menu (`cnw visual install` to restart)")
	} else {
		report("FAIL", "visual host not running: no heartbeat in 15 s; run `cnw visual install` or log out and back in%s", hostLogHint(p.StateDir))
	}
	report("INFO", "visual: macOS asks once for permission to control Terminal or iTerm2 on the first jump")
	reportOrcaCLI(report, os.Getenv)
}

// hostLogHint points at the host's log only when it has something in it.
func hostLogHint(stateDir string) string {
	path := filepath.Join(stateDir, visual.LogFile)
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		return ""
	}
	return " (log: " + path + ")"
}

// reportOrcaCLI warns, inside Orca, when jump.Detect would not find the orca
// CLI, so jumps could only bring Orca to the front.
func reportOrcaCLI(report func(status, format string, a ...any), getenv func(string) string) {
	if getenv("ORCA_TERMINAL_HANDLE") == "" && getenv("ORCA_PANE_KEY") == "" {
		return
	}
	dir := getenv("ORCA_CLI_BIN_DIR")
	if dir == "" {
		report("WARN", "visual: ORCA_CLI_BIN_DIR is not set; jumps only bring Orca to the front")
		return
	}
	if _, err := exec.LookPath(filepath.Join(dir, "orca")); err != nil {
		report("WARN", "visual: orca CLI not found at %s; jumps only bring Orca to the front", filepath.Join(dir, "orca"))
	}
}
