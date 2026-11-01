---
description: "Spoken names in the clips' voice: /cnw:kokoro install [--from DIR] | urls | remove"
argument-hint: "install [--from DIR] | urls | remove"
disable-model-invocation: true
# omp skips skills marked disabled; its /cnw:* commands come from omp/cnw.ts instead, because omp
# neither runs !`…` nor expands ${CLAUDE_PLUGIN_ROOT}. Claude Code ignores this key.
enabled: false
allowed-tools:
  - Bash("${CLAUDE_PLUGIN_ROOT}/bin/cnw" kokoro)
  - Bash("${CLAUDE_PLUGIN_ROOT}/bin/cnw" kokoro *)
---

Run this exact command once with the Bash tool, with a 10-minute timeout, because `install`
downloads about 376 MB (it isn't injected here: injected commands stop after 2 minutes):

```bash
"${CLAUDE_PLUGIN_ROOT}/bin/cnw" kokoro $ARGUMENTS
```

Show the user its output verbatim in a code block and explain any error. With no arguments it
prints its usage; show that. Don't run anything else.
