---
description: "List the bundled voices, or hear one: /cnw:voices [play ID [CATEGORY]]"
argument-hint: "[play ID [CATEGORY]]"
disable-model-invocation: true
# omp skips skills marked disabled; its /cnw:* commands come from omp/cnw.ts instead, because omp
# neither runs !`…` nor expands ${CLAUDE_PLUGIN_ROOT}. Claude Code ignores this key.
enabled: false
allowed-tools:
  - Bash("${CLAUDE_PLUGIN_ROOT}/bin/cnw" voices)
  - Bash("${CLAUDE_PLUGIN_ROOT}/bin/cnw" voices *)
---

Output of `cnw voices $ARGUMENTS`:

```text
!`"${CLAUDE_PLUGIN_ROOT}/bin/cnw" voices $ARGUMENTS || true`
```

Show the user this output verbatim in a code block. For `play`, the clip played unless the output
says otherwise; a line saying names need `cnw kokoro install` is a note, not an error: only the
spoken project name was skipped. Explain any real error. Don't run anything else.
