import { describe, expect, it } from "vitest";
import { sampleMetrics } from "../../test/fixture";
import { tickInterval, toLatencyPoints, toTrafficPoints } from "./series";

describe("series", () => {
  const { series } = sampleMetrics();

  it("splits each minute into successful and errors", () => {
    const pts = toTrafficPoints(series, "UTC");
    expect(pts.map((p) => p.label)).toEqual(["09:58", "09:59", "10:00", "10:01", "10:02", "10:03"]);
    expect(pts.map((p) => [p.ok, p.errors])).toEqual([[0, 0], [0, 0], [2, 0], [1, 1], [0, 1], [0, 1]]);
  });

  it("leaves gaps instead of zero latency for empty minutes", () => {
    const pts = toLatencyPoints(series, "UTC");
    expect(pts[0].p50).toBeNull();
    expect(pts[0].p95).toBeNull();
    expect(pts[3]).toMatchObject({ p50: 1, p95: 120 });
  });

  it("thins x-axis ticks for long windows", () => {
    expect(tickInterval(6)).toBe(0);
    expect(tickInterval(15)).toBe(1);
    expect(tickInterval(60)).toBe(7);
    expect(tickInterval(1440)).toBe(179);
  });
});

describe("isIsolated", () => {
  it("flags a value with gaps on both sides", async () => {
    const { isIsolated } = await import("./series");
    const pts = toLatencyPoints(sampleMetrics().series, "UTC"); // null null 5 1 900 400
    expect(isIsolated(pts, 2, "p50")).toBe(false);
    const lone = pts.map((p, i) => (i === 3 ? p : { ...p, p50: null, p95: null }));
    expect(isIsolated(lone, 3, "p95")).toBe(true);
    expect(isIsolated(lone, 0, "p95")).toBe(false);
  });
});
