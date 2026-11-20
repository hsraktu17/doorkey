import { describe, expect, it } from "vitest";
import { sampleMetrics } from "../../test/fixture";
import { isErrorStatus, isWindow, parseTried, type Metrics } from "./metrics";

// Keys every object of each type must have. If the Go JSON changes, the golden
// file changes, and this test fails until the TS types are updated too.
const shape: Record<string, string[]> = {
  root: ["generated_at", "started_at", "window_seconds", "capacity", "totals", "series", "providers", "routes", "recent"],
  totals: ["requests", "errors", "error_rate", "fallbacks", "fallback_rate", "stream_requests", "requests_per_min", "latency_ms", "ttft_ms", "prompt_tokens", "completion_tokens", "cost_usd"],
  bucket: ["start", "requests", "errors", "p50_ms", "p95_ms"],
  provider: ["provider", "attempts", "served", "failure_rate", "status_2xx", "status_429", "status_4xx", "status_5xx", "network_errors", "latency_ms", "tokens", "cost_usd"],
  route: ["route", "requests", "errors", "fallbacks", "latency_ms"],
  recent: ["time", "request_id", "route", "requested_model", "provider", "model", "stream", "status", "latency_ms", "ttft_ms", "attempts", "tried", "prompt_tokens", "completion_tokens", "cost_usd"],
};

const keys = (o: object) => Object.keys(o).filter((k) => k !== "error").sort();

describe("metrics contract (Go golden file)", () => {
  const m: Metrics = sampleMetrics();

  it("matches the TS types key for key", () => {
    expect(keys(m)).toEqual([...shape.root].sort());
    expect(keys(m.totals)).toEqual([...shape.totals].sort());
    for (const b of m.series) expect(keys(b)).toEqual([...shape.bucket].sort());
    for (const p of m.providers) expect(keys(p)).toEqual([...shape.provider].sort());
    for (const r of m.routes) expect(keys(r)).toEqual([...shape.route].sort());
    for (const r of m.recent) expect(keys(r)).toEqual([...shape.recent].sort());
  });

  it("has the numbers the Go test asserts", () => {
    expect(m.totals.requests).toBe(6);
    expect(m.totals.errors).toBe(3);
    expect(m.totals.error_rate).toBe(0.5);
    expect(m.recent).toHaveLength(6);
  });

  it("agrees with Go on which requests are errors", () => {
    const errorIds = m.recent.filter((r) => isErrorStatus(r, r.request_id === "r6")).map((r) => r.request_id);
    expect(errorIds.sort()).toEqual(["r3", "r5", "r6"]);
  });
});

describe("parseTried", () => {
  it("parses an attempt chain", () => {
    expect(parseTried("mock-flaky:429 mock:200")).toEqual([
      { provider: "mock-flaky", status: 429 },
      { provider: "mock", status: 200 },
    ]);
  });

  it("handles empty and junk input", () => {
    expect(parseTried("")).toEqual([]);
    expect(parseTried("  ")).toEqual([]);
    expect(parseTried("nocolon :200 a:x")).toEqual([]);
  });

  it("keeps network errors (status 0)", () => {
    expect(parseTried("groq:0")).toEqual([{ provider: "groq", status: 0 }]);
  });
});

describe("isWindow", () => {
  it("accepts only offered windows", () => {
    expect(isWindow("15m")).toBe(true);
    expect(isWindow("24h")).toBe(true);
    expect(isWindow("2m")).toBe(false);
    expect(isWindow(null)).toBe(false);
  });
});
