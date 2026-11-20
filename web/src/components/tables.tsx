import type { ProviderStats, RecentRequest, RouteStats } from "@/lib/metrics";
import { parseTried } from "@/lib/metrics";
import { formatClockSeconds, formatCount, formatMs, formatPct, statusTone, type StatusTone } from "@/lib/format";
import { Card, Legend, LegendItem, StatusBadge, toneVar, td, th } from "./ui";

type Segment = { key: string; tone: StatusTone; label: string; count: number };

/** Attempt outcomes for one provider, in a fixed order so colors never move. */
export function providerSegments(p: ProviderStats): Segment[] {
  return [
    { key: "2xx", tone: "good", label: "2xx", count: p.status_2xx },
    { key: "429", tone: "warning", label: "429", count: p.status_429 },
    { key: "4xx", tone: "serious", label: "other 4xx", count: p.status_4xx },
    { key: "5xx", tone: "critical", label: "5xx", count: p.status_5xx },
    { key: "net", tone: "neutral", label: "network", count: p.network_errors },
  ];
}

function OutcomeBar({ provider }: { provider: ProviderStats }) {
  const segs = providerSegments(provider).filter((s) => s.count > 0);
  const total = segs.reduce((n, s) => n + s.count, 0);
  const text = segs.map((s) => `${s.count} ${s.label}`).join(", ");
  return (
    <div className="min-w-40">
      <div
        role="img"
        aria-label={`${provider.provider} attempts: ${text || "none"}`}
        className="flex h-2 gap-0.5 overflow-hidden rounded-full bg-hover"
      >
        {segs.map((s) => (
          <span key={s.key} title={`${s.count} ${s.label}`} style={{ width: `${(s.count / total) * 100}%`, background: toneVar[s.tone] }} />
        ))}
      </div>
      <p className="mt-1 text-xs text-ink-2 tabular" data-testid={`outcomes-${provider.provider}`}>
        {text || "no attempts"}
      </p>
    </div>
  );
}

export function ProviderTable({ providers }: { providers: ProviderStats[] }) {
  return (
    <Card
      title="Providers"
      subtitle="Every attempt the router made, including retries and attempts that fell back to another provider"
      action={
        <Legend>
          {providerSegments(providers[0] ?? ({} as ProviderStats)).map((s) => (
            <LegendItem key={s.key} color={toneVar[s.tone]} label={s.label} />
          ))}
        </Legend>
      }
    >
      {providers.length === 0 ? (
        <p className="text-sm text-muted">No provider attempts in this window.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-xs">
                <th className={th}>Provider</th>
                <th className={th}>Attempt outcomes</th>
                <th className={`${th} text-right`}>Attempts</th>
                <th className={`${th} text-right`}>Served</th>
                <th className={`${th} text-right`}>Failure rate</th>
                <th className={`${th} text-right`}>p50</th>
                <th className={`${th} text-right`}>p95</th>
                <th className={`${th} text-right`}>Tokens</th>
              </tr>
            </thead>
            <tbody>
              {providers.map((p) => (
                <tr key={p.provider} data-testid={`provider-${p.provider}`} className="border-t border-line align-top">
                  <td className={`${td} font-medium`}>{p.provider}</td>
                  <td className={td}>
                    <OutcomeBar provider={p} />
                  </td>
                  <td className={`${td} text-right`}>{formatCount(p.attempts)}</td>
                  <td className={`${td} text-right`}>{formatCount(p.served)}</td>
                  <td className={`${td} text-right`}>{formatPct(p.failure_rate)}</td>
                  <td className={`${td} text-right`}>{formatMs(p.latency_ms.p50)}</td>
                  <td className={`${td} text-right`}>{formatMs(p.latency_ms.p95)}</td>
                  <td className={`${td} text-right`}>{formatCount(p.tokens)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}

export function RoutesTable({ routes }: { routes: RouteStats[] }) {
  return (
    <Card title="Routes" subtitle="Requests by the route they resolved to">
      {routes.length === 0 ? (
        <p className="text-sm text-muted">No requests in this window.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-xs">
                <th className={th}>Route</th>
                <th className={`${th} text-right`}>Requests</th>
                <th className={`${th} text-right`}>Errors</th>
                <th className={`${th} text-right`}>Fallbacks</th>
                <th className={`${th} text-right`}>p50</th>
                <th className={`${th} text-right`}>p95</th>
              </tr>
            </thead>
            <tbody>
              {routes.map((r) => (
                <tr key={r.route} data-testid={`route-${r.route}`} className="border-t border-line">
                  <td className={`${td} font-medium`}>{r.route}</td>
                  <td className={`${td} text-right`}>{formatCount(r.requests)}</td>
                  <td className={`${td} text-right`}>{formatCount(r.errors)}</td>
                  <td className={`${td} text-right`}>{formatCount(r.fallbacks)}</td>
                  <td className={`${td} text-right`}>{formatMs(r.latency_ms.p50)}</td>
                  <td className={`${td} text-right`}>{formatMs(r.latency_ms.p95)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}

/** The router's attempt chain, e.g. mock-flaky 429 → mock 200. */
export function TriedChain({ tried }: { tried: string }) {
  const steps = parseTried(tried);
  if (steps.length === 0) return <span className="text-muted">–</span>;
  return (
    <ol className="flex items-center gap-1" aria-label="Attempts in order">
      {steps.map((s, i) => (
        <li key={i} className="flex items-center gap-1">
          {i > 0 ? (
            <span aria-hidden className="text-muted">
              →
            </span>
          ) : null}
          <span className="inline-flex items-center gap-1 whitespace-nowrap rounded-md border border-line px-1.5 py-0.5 text-xs">
            <span className="text-ink-2">{s.provider}</span>
            <StatusBadge status={s.status} tone={statusTone(s.status)} />
          </span>
        </li>
      ))}
    </ol>
  );
}

export function RecentRequests({ recent, timeZone }: { recent: RecentRequest[]; timeZone?: string }) {
  return (
    <Card title="Recent requests" subtitle="Newest first, up to 50 in this window">
      {recent.length === 0 ? (
        <p className="text-sm text-muted">No requests in this window.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-xs">
                <th className={th}>Time</th>
                <th className={th}>Route</th>
                <th className={th}>Requested</th>
                <th className={th}>Served by</th>
                <th className={th}>Status</th>
                <th className={`${th} text-right`}>Latency</th>
                <th className={`${th} text-right`}>TTFT</th>
                <th className={`${th} text-right`}>Tokens</th>
                <th className={th}>Attempts</th>
              </tr>
            </thead>
            <tbody>
              {recent.map((r) => (
                <tr key={r.request_id} data-testid="recent-row" className="border-t border-line align-top">
                  <td className={`${td} whitespace-nowrap`} title={r.request_id}>
                    {formatClockSeconds(r.time, timeZone)}
                  </td>
                  <td className={td}>{r.route || "–"}</td>
                  <td className={`${td} max-w-48 truncate`} title={r.requested_model}>
                    {r.requested_model || "–"}
                    {r.stream ? <span className="ml-1 text-xs text-muted">stream</span> : null}
                  </td>
                  <td className={`${td} max-w-56 truncate`} title={r.provider ? `${r.provider}/${r.model}` : r.error}>
                    {r.provider ? `${r.provider}/${r.model}` : <span className="text-muted">none</span>}
                  </td>
                  <td className={td}>
                    <StatusBadge status={r.status} tone={statusTone(r.status)} />
                  </td>
                  <td className={`${td} text-right`}>{formatMs(r.latency_ms)}</td>
                  <td className={`${td} text-right`}>{r.stream ? formatMs(r.ttft_ms) : "–"}</td>
                  <td className={`${td} text-right`}>{formatCount(r.prompt_tokens + r.completion_tokens)}</td>
                  <td className={td}>
                    <TriedChain tried={r.tried} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}
