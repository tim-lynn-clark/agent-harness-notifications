---
description: Check the cnw setup (config, clips, audio player, spoken names) and show recent sound events
disable-model-invocation: true
# omp skips skills marked disabled; its /cnw:* commands come from omp/cnw.ts instead, because omp
# neither runs !`…` nor expands ${CLAUDE_PLUGIN_ROOT}. Claude Code ignores this key.
enabled: false
allowed-tools:
  - Bash("${CLAUDE_PLUGIN_ROOT}/bin/cnw" doctor)
---

Output of `cnw doctor`, run from the current directory:

```text
!`"${CLAUDE_PLUGIN_ROOT}/bin/cnw" doctor || true`
```

Show the user this report verbatim in a code block. Then, if any line starts with FAIL or WARN,
explain each one and give the fix the line names. Don't run anything else.
