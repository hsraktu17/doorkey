"use client";

import {
  Bar,
  BarChart,
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
  type TooltipContentProps,
} from "recharts";
import type { Bucket } from "@/lib/metrics";
import { formatCount, formatMs } from "@/lib/format";
import { isIsolated, tickInterval, toLatencyPoints, toTrafficPoints, type LatencyPoint, type TrafficPoint } from "@/lib/series";
import { Card, DataTable, Legend, LegendItem, td, th } from "./ui";

const HEIGHT = 220;
const axisTick = { fill: "var(--muted)", fontSize: 12 };

const COLORS = {
  ok: "var(--series-1)",
  errors: "var(--status-critical)",
  p50: "var(--series-1)",
  p95: "var(--series-2)",
};

function TooltipShell({ title, rows }: { title: string; rows: { key: string; color: string; label: string; value: string }[] }) {
  return (
    <div className="rounded-lg border border-line bg-surface px-3 py-2 text-xs shadow-sm">
      <p className="mb-1 text-muted">{title}</p>
      <ul className="space-y-0.5">
        {rows.map((r) => (
          <li key={r.key} className="flex items-center gap-2">
            <span aria-hidden className="h-0.5 w-3 rounded-full" style={{ background: r.color }} />
            <span className="font-semibold tabular text-ink">{r.value}</span>
            <span className="text-ink-2">{r.label}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function TrafficTooltip({ active, payload }: TooltipContentProps) {
  const p = payload?.[0]?.payload as TrafficPoint | undefined;
  if (!active || !p) return null;
  return (
    <TooltipShell
      title={p.label}
      rows={[
        { key: "ok", color: COLORS.ok, label: "successful", value: formatCount(p.ok) },
        { key: "err", color: COLORS.errors, label: "errors", value: formatCount(p.errors) },
      ]}
    />
  );
}

function LatencyTooltip({ active, payload }: TooltipContentProps) {
  const p = payload?.[0]?.payload as LatencyPoint | undefined;
  if (!active || !p) return null;
  if (p.requests === 0) return <TooltipShell title={p.label} rows={[{ key: "none", color: "var(--muted)", label: "no requests", value: "–" }]} />;
  return (
    <TooltipShell
      title={`${p.label} · ${formatCount(p.requests)} requests`}
      rows={[
        { key: "p95", color: COLORS.p95, label: "p95", value: formatMs(p.p95 ?? 0) },
        { key: "p50", color: COLORS.p50, label: "p50", value: formatMs(p.p50 ?? 0) },
      ]}
    />
  );
}

export function TrafficChart({ series }: { series: Bucket[] }) {
  const data = toTrafficPoints(series);
  return (
    <Card title="Requests per minute" subtitle="Errors are 5xx, rate-limited (429) everywhere, or streams that broke after starting">
      <Legend>
        <LegendItem color={COLORS.ok} label="Successful" />
        <LegendItem color={COLORS.errors} label="Errors" />
      </Legend>
      <div className="mt-2" role="img" aria-label={`Requests per minute over ${data.length} minutes, split into successful and errors`}>
        <ResponsiveContainer width="100%" height={HEIGHT}>
          <BarChart data={data} margin={{ top: 8, right: 4, bottom: 0, left: -12 }} barCategoryGap="20%">
            <CartesianGrid vertical={false} stroke="var(--grid)" />
            <XAxis dataKey="label" tick={axisTick} tickLine={false} axisLine={{ stroke: "var(--axis)" }} interval={tickInterval(data.length)} minTickGap={8} />
            <YAxis tick={axisTick} tickLine={false} axisLine={false} allowDecimals={false} width={44} tickFormatter={(v: number) => formatCount(v)} />
            <Tooltip content={TrafficTooltip} cursor={{ fill: "var(--hover)" }} isAnimationActive={false} />
            <Bar dataKey="ok" stackId="t" fill={COLORS.ok} maxBarSize={24} stroke="var(--surface)" strokeWidth={1} isAnimationActive={false} />
            <Bar dataKey="errors" stackId="t" fill={COLORS.errors} maxBarSize={24} radius={[4, 4, 0, 0]} stroke="var(--surface)" strokeWidth={1} isAnimationActive={false} />
          </BarChart>
        </ResponsiveContainer>
      </div>
      <DataTable summary="Show as table">
        <table className="w-full">
          <thead>
            <tr>
              <th className={th}>Minute</th>
              <th className={th}>Requests</th>
              <th className={th}>Errors</th>
            </tr>
          </thead>
          <tbody>
            {data.map((p) => (
              <tr key={p.start} className="border-t border-line">
                <td className={td}>{p.label}</td>
                <td className={td}>{p.requests}</td>
                <td className={td}>{p.errors}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </DataTable>
    </Card>
  );
}

type DotProps = { cx?: number; cy?: number; index?: number };

export function LatencyChart({ series }: { series: Bucket[] }) {
  const data = toLatencyPoints(series);
  const ring = { r: 4, stroke: "var(--surface)", strokeWidth: 2 };
  // A minute with empty minutes on both sides has no line segment; mark it with a dot.
  const lonely = (key: "p50" | "p95", color: string) =>
    function LonelyDot({ cx, cy, index = 0 }: DotProps) {
      if (cx == null || cy == null || !isIsolated(data, index, key)) return <g key={`${key}-${index}`} />;
      return <circle key={`${key}-${index}`} cx={cx} cy={cy} fill={color} {...ring} />;
    };
  return (
    <Card title="Latency per minute" subtitle="Total request time through the gateway, including retries and fallbacks">
      <Legend>
        <LegendItem shape="line" color={COLORS.p95} label="p95" />
        <LegendItem shape="line" color={COLORS.p50} label="p50" />
      </Legend>
      <div className="mt-2" role="img" aria-label={`p50 and p95 latency per minute over ${data.length} minutes`}>
        <ResponsiveContainer width="100%" height={HEIGHT}>
          <LineChart data={data} margin={{ top: 8, right: 16, bottom: 0, left: -4 }}>
            <CartesianGrid vertical={false} stroke="var(--grid)" />
            <XAxis dataKey="label" tick={axisTick} tickLine={false} axisLine={{ stroke: "var(--axis)" }} interval={tickInterval(data.length)} minTickGap={8} />
            <YAxis tick={axisTick} tickLine={false} axisLine={false} width={68} tickFormatter={(v: number) => formatMs(v)} />
            <Tooltip content={LatencyTooltip} cursor={{ stroke: "var(--axis)", strokeWidth: 1 }} isAnimationActive={false} />
            <Line type="monotone" dataKey="p95" stroke={COLORS.p95} strokeWidth={2} dot={lonely("p95", COLORS.p95)} activeDot={{ ...ring, fill: COLORS.p95 }} connectNulls={false} isAnimationActive={false} />
            <Line type="monotone" dataKey="p50" stroke={COLORS.p50} strokeWidth={2} dot={lonely("p50", COLORS.p50)} activeDot={{ ...ring, fill: COLORS.p50 }} connectNulls={false} isAnimationActive={false} />
          </LineChart>
        </ResponsiveContainer>
      </div>
      <DataTable summary="Show as table">
        <table className="w-full">
          <thead>
            <tr>
              <th className={th}>Minute</th>
              <th className={th}>Requests</th>
              <th className={th}>p50</th>
              <th className={th}>p95</th>
            </tr>
          </thead>
          <tbody>
            {data.map((p) => (
              <tr key={p.start} className="border-t border-line">
                <td className={td}>{p.label}</td>
                <td className={td}>{p.requests}</td>
                <td className={td}>{p.p50 === null ? "–" : formatMs(p.p50)}</td>
                <td className={td}>{p.p95 === null ? "–" : formatMs(p.p95)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </DataTable>
    </Card>
  );
}
