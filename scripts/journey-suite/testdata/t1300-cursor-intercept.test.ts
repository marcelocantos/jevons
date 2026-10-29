// 🎯T1300 Stage A: prove Cursor OMP model turns go through the planted
// catalog mock (getBundledModel), not native cursor-agent http2.
// Copied into a staged sidecar and run with `bun test` (see the Go driver).

import { createHash } from "node:crypto";
import fs from "node:fs";
import http2 from "node:http2";
import os from "node:os";
import path from "node:path";
import { describe, expect, test } from "bun:test";
import type { Context } from "@oh-my-pi/pi-ai";
import { plantCursorCatalogMock, t1300CursorModel } from "./t1300-cursor-catalog.ts";

const http2Hits: string[] = [];
http2.connect = ((...args: Parameters<typeof http2.connect>) => {
  http2Hits.push(String(args[0]));
  throw new Error(`T1300 BYPASS: cursor-agent http2.connect ${args[0]}`);
}) as typeof http2.connect;

globalThis.fetch = (async (input: RequestInfo | URL) => {
  throw new Error(`T1300 BYPASS: fetch ${String(input)}`);
}) as typeof fetch;

function lastUserText(context: Context): string {
  const users = context.messages.filter((m) => m.role === "user");
  const last = users.at(-1);
  if (!last || last.role !== "user") return "";
  const c = last.content;
  if (typeof c === "string") return c;
  if (!Array.isArray(c)) return "";
  return c.filter((b) => b && (b as { type?: string }).type === "text").map((b) => String((b as { text?: string }).text ?? "")).join("");
}

function promptHash(text: string): string {
  return createHash("sha256").update(text).digest("hex");
}

const writePrompt = "T1300_WRITE";
const writeHash = promptHash(writePrompt);

function t1300Handler(context: Context) {
  const text = lastUserText(context);
  const hash = promptHash(text);
  if (!text.includes("T1300_WRITE")) {
    return { throw: `T1300 stale: prompt hash mismatch got=${hash.slice(0, 12)} want=${writeHash.slice(0, 12)}` };
  }
  const results = context.messages.filter((m) => m.role === "toolResult");
  if (results.length === 0) {
    return {
      content: [{ type: "toolCall" as const, id: "t1300-write", name: "Write", arguments: { path: "marker.txt", content: "t1300-marker\n" } }],
    };
  }
  if (results.length === 1) {
    return {
      content: [{ type: "toolCall" as const, id: "t1300-bash", name: "Bash", arguments: { command: "cat marker.txt" } }],
    };
  }
  return { content: ["T1300_DONE"] };
}

describe("T1300 Stage A cursor catalog intercept", () => {
  test("composer-2.5 model turns hit the planted mock, not http2", async () => {
    http2Hits.length = 0;
    plantCursorCatalogMock(t1300Handler);
    const { createSeatAgent } = await import("./seat.ts");
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "t1300-intercept-"));
    const events: { type: string; text?: string; error?: string }[] = [];
    const agent = createSeatAgent({
      provider: "cursor",
      model: t1300CursorModel,
      token: "fixture",
      cwd: dir,
      emit: (ev) => events.push(ev),
      callTool: async () => {
        throw new Error("host tool should not run in Stage A");
      },
    });
    await agent.prompt(writePrompt);
    expect(http2Hits, "Cursor bypassed the catalog mock (native http2)").toEqual([]);
    expect(fs.readFileSync(path.join(dir, "marker.txt"), "utf8")).toBe("t1300-marker\n");
    const ends = events.filter((e) => e.type === "turn_end");
    expect(ends.length).toBeGreaterThanOrEqual(1);
    expect(ends.at(-1)?.error).toBeUndefined();
    const blob = events.map((e) => JSON.stringify(e)).join("\n");
    expect(blob.includes("T1300_DONE")).toBe(true);
  }, { timeout: 20_000 });

  test("a prompt that is not in the cassette fails stale, never live", async () => {
    http2Hits.length = 0;
    plantCursorCatalogMock(t1300Handler);
    const { createSeatAgent } = await import("./seat.ts");
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "t1300-stale-"));
    const events: { type: string; error?: string; text?: string }[] = [];
    const agent = createSeatAgent({
      provider: "cursor",
      model: t1300CursorModel,
      token: "fixture",
      cwd: dir,
      emit: (ev) => events.push(ev),
      callTool: async () => "",
    });
    let thrown = "";
    try {
      await agent.prompt("T1300_DRIFT this brief is not in the cassette");
    } catch (err) {
      thrown = String(err);
    }
    expect(http2Hits, "stale miss fell through to native http2").toEqual([]);
    const err = thrown + "\n" + events.map((e) => `${e.error ?? ""} ${e.text ?? ""} ${JSON.stringify(e)}`).join("\n");
    expect(err.includes("stale")).toBe(true);
    expect(fs.existsSync(path.join(dir, "marker.txt"))).toBe(false);
  }, { timeout: 20_000 });
});
