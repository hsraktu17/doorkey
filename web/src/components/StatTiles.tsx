import type { Totals } from "@/lib/metrics";
import { formatCount, formatMs, formatPct, formatUSD } from "@/lib/format";

type Tile = { label: string; value: string; detail: string; testId: string };

export function tilesFor(t: Totals): Tile[] {
  return [
    {
      label: "Requests",
      value: formatCount(t.requests),
      detail: `${t.requests_per_min.toFixed(t.requests_per_min < 10 ? 1 : 0)} per min`,
      testId: "tile-requests",
    },
    {
      label: "Error rate",
      value: formatPct(t.error_rate),
      detail: `${formatCount(t.errors)} ${t.errors === 1 ? "error" : "errors"}`,
      testId: "tile-errors",
    },
    {
      label: "Latency p95",
      value: formatMs(t.latency_ms.p95),
      detail: `p50 ${formatMs(t.latency_ms.p50)} · p99 ${formatMs(t.latency_ms.p99)}`,
      testId: "tile-latency",
    },
    {
      label: "Time to first token p95",
      value: t.stream_requests > 0 ? formatMs(t.ttft_ms.p95) : "–",
      detail: t.stream_requests > 0 ? `p50 ${formatMs(t.ttft_ms.p50)} · ${formatCount(t.stream_requests)} streams` : "no streams yet",
      testId: "tile-ttft",
    },
    {
      label: "Fallback rate",
      value: formatPct(t.fallback_rate),
      detail: `${formatCount(t.fallbacks)} served after a failed attempt`,
      testId: "tile-fallbacks",
    },
    {
      label: "Tokens",
      value: formatCount(t.prompt_tokens + t.completion_tokens),
      detail: `${formatCount(t.prompt_tokens)} in · ${formatCount(t.completion_tokens)} out`,
      testId: "tile-tokens",
    },
    {
      label: "Cost",
      value: formatUSD(t.cost_usd),
      detail: "from pricing.json",
      testId: "tile-cost",
    },
  ];
}

export function StatTiles({ totals }: { totals: Totals }) {
  return (
    <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-7">
      {tilesFor(totals).map((tile) => (
        <div key={tile.label} data-testid={tile.testId} className="min-w-0 rounded-xl border border-line bg-surface px-4 py-3">
          <dt className="truncate text-xs text-ink-2">{tile.label}</dt>
          <dd className="mt-1 text-2xl font-semibold tracking-tight text-ink">{tile.value}</dd>
          <dd className="mt-0.5 truncate text-xs text-muted">{tile.detail}</dd>
        </div>
      ))}
    </dl>
  );
}
