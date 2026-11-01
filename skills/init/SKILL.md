---
description: Write the default cnw config file if none exists
disable-model-invocation: true
# omp skips skills marked disabled; its /cnw:* commands come from omp/cnw.ts instead, because omp
# neither runs !`…` nor expands ${CLAUDE_PLUGIN_ROOT}. Claude Code ignores this key.
enabled: false
allowed-tools:
  - Bash("${CLAUDE_PLUGIN_ROOT}/bin/cnw" init)
---

Output of `cnw init`:

```text
!`"${CLAUDE_PLUGIN_ROOT}/bin/cnw" init || true`
```

Show the user this output verbatim, and explain any error. Don't run anything else.
