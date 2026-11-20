import type { Bucket } from "./metrics";
import { formatClock } from "./format";

export type TrafficPoint = { label: string; start: string; ok: number; errors: number; requests: number };
export type LatencyPoint = { label: string; start: string; p50: number | null; p95: number | null; requests: number };

/** Per-minute successful vs error counts for the stacked traffic chart. */
export function toTrafficPoints(series: Bucket[], timeZone?: string): TrafficPoint[] {
  return series.map((b) => ({
    label: formatClock(b.start, timeZone),
    start: b.start,
    ok: Math.max(0, b.requests - b.errors),
    errors: b.errors,
    requests: b.requests,
  }));
}

/**
 * Per-minute latency percentiles. Minutes with no traffic become null so the
 * line breaks there instead of dropping to a misleading 0 ms.
 */
export function toLatencyPoints(series: Bucket[], timeZone?: string): LatencyPoint[] {
  return series.map((b) => ({
    label: formatClock(b.start, timeZone),
    start: b.start,
    p50: b.requests > 0 ? b.p50_ms : null,
    p95: b.requests > 0 ? b.p95_ms : null,
    requests: b.requests,
  }));
}

/** Tick interval so a long window shows ~8 x-axis labels, not one per minute. */
export function tickInterval(points: number, target = 8): number {
  return Math.max(0, Math.ceil(points / target) - 1);
}

/** True when a point has a value but both neighbours are gaps, so a line can't show it. */
export function isIsolated(points: LatencyPoint[], i: number, key: "p50" | "p95"): boolean {
  const v = points[i]?.[key];
  return v != null && points[i - 1]?.[key] == null && points[i + 1]?.[key] == null;
}
