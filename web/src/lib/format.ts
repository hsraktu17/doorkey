// Number and time formatting for the dashboard. Kept pure so it is easy to test.

const compact = new Intl.NumberFormat("en-US", { notation: "compact", maximumFractionDigits: 1 });
const whole = new Intl.NumberFormat("en-US");

/** 1,284 under 10,000; 12.9K / 4.2M above. */
export function formatCount(n: number): string {
  if (!Number.isFinite(n)) return "–";
  return Math.abs(n) < 10_000 ? whole.format(Math.round(n)) : compact.format(n);
}

/** Milliseconds: "0.21 ms", "84 ms", "1.24 s". */
export function formatMs(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return "–";
  if (ms < 0.01) return "<0.01 ms";
  if (ms < 1) return `${ms.toFixed(2)} ms`;
  if (ms < 1000) return `${ms < 10 ? ms.toFixed(1) : Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(2)} s`;
}

/** A 0..1 ratio as a percentage: "0%", "4.2%", "100%". */
export function formatPct(ratio: number): string {
  if (!Number.isFinite(ratio)) return "–";
  const pct = ratio * 100;
  if (pct === 0 || pct === 100) return `${pct}%`;
  return `${pct < 10 ? pct.toFixed(1) : Math.round(pct)}%`;
}

/** USD, showing small free-tier amounts without rounding them to $0.00. */
export function formatUSD(usd: number): string {
  if (!Number.isFinite(usd)) return "–";
  if (usd === 0) return "$0";
  if (usd < 0.01) return `$${usd.toPrecision(2)}`;
  return new Intl.NumberFormat("en-US", { style: "currency", currency: "USD" }).format(usd);
}

/** HH:MM in the viewer's time zone. */
export function formatClock(iso: string, timeZone?: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "–";
  return d.toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit", timeZone });
}

/** HH:MM:SS in the viewer's time zone. */
export function formatClockSeconds(iso: string, timeZone?: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "–";
  return d.toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit", second: "2-digit", timeZone });
}

/** "3m", "2h 5m", "4d 1h" since a start time. */
export function formatUptime(startIso: string, nowIso: string): string {
  const secs = Math.max(0, (Date.parse(nowIso) - Date.parse(startIso)) / 1000);
  if (!Number.isFinite(secs)) return "–";
  const d = Math.floor(secs / 86400);
  const h = Math.floor((secs % 86400) / 3600);
  const m = Math.floor((secs % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${Math.max(m, secs > 0 ? 1 : 0)}m`;
}

/** Status class for a request's HTTP status. */
export type StatusTone = "good" | "warning" | "serious" | "critical" | "neutral";

export function statusTone(status: number): StatusTone {
  if (status === 0) return "neutral"; // network error
  if (status < 300) return "good";
  if (status === 429) return "warning";
  if (status === 499) return "neutral"; // client went away
  if (status < 500) return "serious";
  return "critical";
}
