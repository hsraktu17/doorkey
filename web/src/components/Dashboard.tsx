"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import type { Metrics, MetricsError, Window } from "@/lib/metrics";
import { formatClockSeconds, formatUptime } from "@/lib/format";
import { StatTiles } from "./StatTiles";
import { LatencyChart, TrafficChart } from "./charts";
import { ProviderTable, RecentRequests, RoutesTable } from "./tables";
import { WindowPicker } from "./WindowPicker";

export const POLL_MS = 5000;

type Loaded = { window: Window; metrics: Metrics };
type Failure = { window: Window; error: MetricsError["error"] };

async function load(window: Window, signal: AbortSignal): Promise<Loaded | Failure> {
  try {
    const res = await fetch(`/api/metrics?window=${window}`, { signal, cache: "no-store" });
    const body = await res.json();
    if (!res.ok) return { window, error: (body as MetricsError).error ?? { type: "gateway_error", message: `HTTP ${res.status}` } };
    return { window, metrics: body as Metrics };
  } catch (err) {
    if (signal.aborted) throw err;
    return { window, error: { type: "gateway_unreachable", message: "Could not reach the dashboard's /api/metrics route." } };
  }
}

export function Dashboard({ initialWindow, pollMs = POLL_MS }: { initialWindow: Window; pollMs?: number }) {
  const [window, setWindow] = useState<Window>(initialWindow);
  const [live, setLive] = useState(true);
  const [data, setData] = useState<Loaded | null>(null);
  const [failure, setFailure] = useState<Failure | null>(null);
  const [tick, setTick] = useState(0);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  // Fetch whenever the window changes or a poll tick fires.
  useEffect(() => {
    const ctrl = new AbortController();
    load(window, ctrl.signal)
      .then((r) => {
        if ("metrics" in r) {
          setData(r);
          setFailure(null);
        } else {
          setFailure(r);
        }
      })
      .catch(() => {}) // aborted
      .finally(() => {
        if (ctrl.signal.aborted || !live) return;
        timer.current = setTimeout(() => {
          if (document.visibilityState === "visible") setTick((t) => t + 1);
        }, pollMs);
      });
    return () => {
      ctrl.abort();
      clearTimeout(timer.current);
    };
  }, [window, tick, live, pollMs]);

  // A hidden tab skips its poll; refetch as soon as it is visible again.
  useEffect(() => {
    const onVisible = () => {
      if (document.visibilityState === "visible" && live) setTick((t) => t + 1);
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => document.removeEventListener("visibilitychange", onVisible);
  }, [live]);

  const changeWindow = useCallback((w: Window) => {
    setWindow(w);
    const url = new URL(globalThis.location.href);
    url.searchParams.set("window", w);
    globalThis.history.replaceState(null, "", url);
  }, []);

  const metrics = data?.metrics;
  const stale = data !== null && data.window !== window;

  return (
    <main className="mx-auto max-w-[1440px] px-4 py-6 sm:px-6 lg:px-8">
      <header className="mb-5 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold text-ink">doorkey APM</h1>
          <p className="text-xs text-ink-2" data-testid="status-line">
            {metrics ? (
              <>
                Updated {formatClockSeconds(metrics.generated_at)} · gateway up {formatUptime(metrics.started_at, metrics.generated_at)} ·
                in-memory, last {metrics.capacity.toLocaleString("en-US")} requests, resets on restart
              </>
            ) : (
              "Connecting to the gateway…"
            )}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <WindowPicker value={window} onChange={changeWindow} />
          <button
            type="button"
            onClick={() => setLive((l) => !l)}
            aria-pressed={live}
            className="inline-flex items-center gap-1.5 rounded-lg border border-line bg-surface px-2.5 py-1.5 text-xs font-medium text-ink-2 hover:bg-hover hover:text-ink"
          >
            <span aria-hidden className="size-2 rounded-full" style={{ background: live ? "var(--status-good)" : "var(--status-neutral)" }} />
            {live ? "Live" : "Paused"}
          </button>
        </div>
      </header>

      {failure ? <GatewayDown error={failure.error} hasData={!!metrics} /> : null}

      {metrics ? (
        <div className={`space-y-4 transition-opacity ${stale ? "opacity-60" : ""}`} aria-busy={stale}>
          <StatTiles totals={metrics.totals} />
          {metrics.totals.requests === 0 ? (
            <EmptyState />
          ) : (
            <>
              <div className="grid gap-4 lg:grid-cols-2">
                <TrafficChart series={metrics.series} />
                <LatencyChart series={metrics.series} />
              </div>
              <ProviderTable providers={metrics.providers} />
              <RoutesTable routes={metrics.routes} />
              <RecentRequests recent={metrics.recent} />
            </>
          )}
        </div>
      ) : failure ? null : (
        <p className="py-16 text-center text-sm text-muted">Loading metrics…</p>
      )}
    </main>
  );
}

function GatewayDown({ error, hasData }: { error: MetricsError["error"]; hasData: boolean }) {
  return (
    <div role="alert" data-testid="gateway-down" className="mb-4 flex gap-3 rounded-xl border border-line bg-surface p-4">
      <span aria-hidden className="mt-1 size-2.5 shrink-0 rounded-full" style={{ background: "var(--status-critical)" }} />
      <div className="text-sm">
        <p className="font-semibold text-critical-text">
          {error.type === "gateway_unreachable" ? "Gateway unreachable" : "Gateway error"}
          {hasData ? " – showing the last good data" : ""}
        </p>
        <p className="mt-0.5 text-ink-2">{error.message}</p>
        <p className="mt-1 text-xs text-muted">
          Start it with <code className="rounded bg-hover px-1">make run</code> and check DOORKEY_URL / DOORKEY_API_KEY in web/.env.local.
        </p>
      </div>
    </div>
  );
}

function EmptyState() {
  return (
    <section className="rounded-xl border border-dashed border-line bg-surface p-6 text-sm" data-testid="empty-state">
      <h2 className="font-semibold text-ink">No requests in this window yet</h2>
      <p className="mt-1 text-ink-2">Send one through the gateway. The demo route uses mock providers only, so it costs nothing:</p>
      <pre className="mt-3 overflow-x-auto rounded-lg bg-hover p-3 text-xs text-ink">
        {`curl -s localhost:8080/v1/chat/completions \\
  -H "Authorization: Bearer $DOORKEY_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"demo","messages":[{"role":"user","content":"hi"}]}'`}
      </pre>
      <p className="mt-2 text-xs text-muted">Or run <code>npm run traffic</code> from web/ to send a steady mix against the demo route.</p>
    </section>
  );
}
