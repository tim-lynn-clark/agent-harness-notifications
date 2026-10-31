// omp adapter for cnw: turns omp agent events into cnw sounds.
//
// omp reports when a prompt is answered, so prompts are registered with
// `cnw notify --pending` and cancelled with `cnw resolve`. The same ~6 s gate
// Claude Code applies before its "still waiting" notification keeps a prompt
// you answer straight away silent.
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { fileURLToPath } from "node:url";

const PROMPT_GATE_MS = 6000;
const IDLE_REMINDER_MS = 60_000;

type TimerHandle = unknown;
interface Ctx {
  agent?: { kind?: string };
  cwd?: string;
  sessionManager?: { getSessionId?: () => string };
  setTimeout?: (fn: () => void, ms: number) => TimerHandle;
  clearTimer?: (handle: TimerHandle) => void;
}
type Handler = (event: unknown, ctx: Ctx) => void;
interface ExtensionAPI {
  on(event: string, handler: Handler): void;
}

function field(value: unknown, key: string): unknown {
  return value !== null && typeof value === "object" && key in value
    ? (value as Record<string, unknown>)[key]
    : undefined;
}

// The binary cnw's build script writes for this platform, relative to this file.
function cnwBinary(): string | undefined {
  const os = { darwin: "darwin", linux: "linux", win32: "windows" }[process.platform as string];
  const arch = { arm64: "arm64", x64: "amd64" }[process.arch as string];
  if (!os || !arch) return undefined;
  const exe = os === "windows" ? "cnw.exe" : "cnw";
  const path = fileURLToPath(new URL(`../bin/${os}-${arch}/${exe}`, import.meta.url));
  return existsSync(path) ? path : undefined;
}

// Final assistant message of a run: its stop reason and text.
function lastReply(event: unknown): { stopReason?: string; text: string } {
  const messages = field(event, "messages");
  if (!Array.isArray(messages)) return { text: "" };
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i];
    if (field(m, "role") !== "assistant") continue;
    const content = field(m, "content");
    const text = Array.isArray(content)
      ? content
          .filter((b) => field(b, "type") === "text")
          .map((b) => String(field(b, "text") ?? ""))
          .join("\n")
      : "";
    const stopReason = field(m, "stopReason");
    return { stopReason: typeof stopReason === "string" ? stopReason : undefined, text };
  }
  return { text: "" };
}

export default function cnwExtension(pi: ExtensionAPI): void {
  const bin = cnwBinary();
  if (!bin) return; // not built for this platform: stay silent
  const fallbackSession = `omp-${process.pid}`;
  let idleTimer: { handle: TimerHandle; ctx: Ctx } | undefined;

  const run = (args: string[], stdin?: string): void => {
    try {
      const child = spawn(bin, args, { stdio: [stdin === undefined ? "ignore" : "pipe", "ignore", "ignore"] });
      child.on("error", () => {});
      if (stdin !== undefined) child.stdin?.end(stdin);
      child.unref();
    } catch {
      // A sound is never worth disturbing the session.
    }
  };
  const session = (ctx: Ctx): string => {
    try {
      return ctx.sessionManager?.getSessionId?.() || fallbackSession;
    } catch {
      return fallbackSession;
    }
  };
  const clearIdle = (): void => {
    if (idleTimer) idleTimer.ctx.clearTimer?.(idleTimer.handle);
    idleTimer = undefined;
  };
  // --cwd lets cnw announce the project and worktree after the clip.
  const prompt = (category: string, ctx: Ctx): void => {
    const dir = ctx.cwd ? ["--cwd", ctx.cwd] : [];
    run(["notify", category, "--session", session(ctx), ...dir, "--pending", "--after", String(PROMPT_GATE_MS)]);
  };
  const resolve = (ctx: Ctx): void => run(["resolve", "--session", session(ctx)]);

  // Subagents run headless; only the main agent's prompts and turns need you.
  const on = (event: string, handler: Handler): void =>
    pi.on(event, (e, ctx) => {
      if (ctx.agent?.kind !== "sub") handler(e, ctx);
    });

  on("tool_approval_requested", (_e, ctx) => prompt("permission", ctx));
  on("tool_approval_resolved", (_e, ctx) => resolve(ctx));
  on("tool_call", (e, ctx) => {
    if (field(e, "toolName") === "ask") prompt("question", ctx);
  });
  on("tool_execution_end", (e, ctx) => {
    if (field(e, "toolName") === "ask") resolve(ctx);
  });
  on("plan_review_requested", (_e, ctx) => prompt("plan_approval", ctx));
  on("plan_review_resolved", (_e, ctx) => resolve(ctx));

  for (const event of ["input", "agent_start", "session_shutdown"]) on(event, () => clearIdle());

  on("agent_end", (e, ctx) => {
    if (field(e, "willContinue") === true) return; // more work is already scheduled
    const { stopReason, text } = lastReply(e);
    const sid = session(ctx);
    if (stopReason === "aborted") {
      resolve(ctx); // you interrupted it: no sound
      return;
    }
    const dir = ctx.cwd ? ["--cwd", ctx.cwd] : [];
    run(["turn-end", "--session", sid, ...dir, ...(stopReason === "error" ? ["--error"] : [])], text);
    clearIdle();
    const handle = ctx.setTimeout?.(() => {
      idleTimer = undefined;
      run(["notify", "idle_reminder", "--session", sid, ...dir]);
    }, IDLE_REMINDER_MS);
    if (handle !== undefined) idleTimer = { handle, ctx };
  });
}
