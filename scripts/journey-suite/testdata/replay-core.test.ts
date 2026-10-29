import { describe, expect, test } from "bun:test";
import http2 from "node:http2";
import { canonicalize, turnHash, Replay, type Cassette, type Paths } from "./replay-core.ts";
const paths: Paths = { workdir: "/tmp/w1", state: "/tmp/s1", gateDir: "/tmp/s1/gates", authoredTargetIds: ["T1151", "T1183"] };
const changed: Paths = { workdir: "/tmp/w2", state: "/tmp/s2", gateDir: "/tmp/s2/gates", authoredTargetIds: paths.authoredTargetIds };
const a = `STATE DIR: /tmp/s1\n/tmp/w1 /tmp/s1/gates id=ab12cd34 T1151 T991 1234567890123456789012345678901234567890 2026-10-29T12:30:00Z 123e4567-e89b-12d3-a456-426614174000`;
const b = `STATE DIR: /tmp/s2\n/tmp/w2 /tmp/s2/gates id=deadbeef T1151 T992 abcdefabcdefabcdefabcdefabcdefabcdefabcd 2026-10-30T12:30:00Z 123e4567-e89b-12d3-a456-426614174001`;
const cassette = (hash: string): Cassette => ({ version: 1, journey: "j51", source_hash: "source", entries: [{ seat: "worker-0", hash, output: { content: ["recorded"] } }] });

describe("T1300 Stage B fail-closed replay", () => {
  test("canonical substitutions, and authored target/brief preservation", () => {
    expect(canonicalize(a, paths)).toBe(canonicalize(b, changed));
    expect(canonicalize(a, paths)).toContain("T1151 ${TID} ${SHA} ${TS} ${UUID}");
    expect(turnHash({ context: a }, paths)).toBe(turnHash({ context: b }, changed));
    expect(turnHash({ context: a + " new brief" }, paths)).not.toBe(turnHash({ context: a }, paths));
    expect(turnHash({ context: a.replace("T1151", "T1152") }, paths)).not.toBe(turnHash({ context: a }, paths));
  });
  test("source drift fails before first model call", () => expect(() => new Replay(cassette("x"), "different")).toThrow("stale"));
  test("exact turn replays; miss, exhausted and unused entries fail stale, never live", () => {
    const hits: string[] = [];
    const native = http2.connect;
    const fetch = globalThis.fetch;
    http2.connect = ((...args: Parameters<typeof http2.connect>) => { hits.push("http2"); throw Error("T1300 BYPASS"); }) as typeof http2.connect;
    globalThis.fetch = (async () => { hits.push("fetch"); throw Error("T1300 BYPASS"); }) as typeof fetch;
    try {
      const hash = turnHash({ context: a }, paths);
      const replay = new Replay(cassette(hash), "source");
      expect(replay.next("worker-0", { context: b }, changed)).toEqual({ content: ["recorded"] });
      replay.finish();
      expect(() => replay.next("worker-0", { context: b }, changed)).toThrow("stale cassette");
      expect(() => new Replay(cassette(hash), "source").next("worker-0", { context: b + " DRIFT" }, changed)).toThrow("prompt mismatch");
      expect(() => new Replay(cassette(hash), "source").finish()).toThrow("unused entries");
      expect(hits).toEqual([]);
    } finally { http2.connect = native; globalThis.fetch = fetch; }
  });
});
