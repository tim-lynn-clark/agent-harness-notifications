---
description: "Play a category's configured sound: /cnw:play CATEGORY"
argument-hint: CATEGORY
disable-model-invocation: true
# omp skips skills marked disabled; its /cnw:* commands come from omp/cnw.ts instead, because omp
# neither runs !`…` nor expands ${CLAUDE_PLUGIN_ROOT}. Claude Code ignores this key.
enabled: false
allowed-tools:
  - Bash("${CLAUDE_PLUGIN_ROOT}/bin/cnw" play)
  - Bash("${CLAUDE_PLUGIN_ROOT}/bin/cnw" play *)
---

Output of `cnw play $ARGUMENTS`:

```text
!`"${CLAUDE_PLUGIN_ROOT}/bin/cnw" play $ARGUMENTS || true`
```

Empty output means the sound played: say so in one line. Otherwise show the output verbatim and
explain the error; the categories are question, plan_approval, permission, mcp_input,
done_question, done, done_background, error, usage_limit, idle_reminder. Don't run anything else.
