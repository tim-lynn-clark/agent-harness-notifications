---
description: "On-screen signal and menu bar orb for waiting sessions (macOS): /cnw:visual install | uninstall | preview [CATEGORY]"
argument-hint: "install | uninstall | preview [CATEGORY]"
disable-model-invocation: true
# omp skips skills marked disabled; its /cnw:* commands come from omp/cnw.ts instead, because omp
# neither runs !`…` nor expands ${CLAUDE_PLUGIN_ROOT}. Claude Code ignores this key.
enabled: false
allowed-tools:
  - Bash("${CLAUDE_PLUGIN_ROOT}/bin/cnw" visual)
  - Bash("${CLAUDE_PLUGIN_ROOT}/bin/cnw" visual *)
---

Run this exact command once with the Bash tool:

```bash
"${CLAUDE_PLUGIN_ROOT}/bin/cnw" visual $ARGUMENTS
```

Show the user its output verbatim in a code block and explain any error. With no arguments it
prints its usage; show that. Don't run anything else.
