// Types for the gateway's GET /admin/metrics response. They mirror
// internal/usage/metrics.go; the golden file
// internal/usage/testdata/metrics.sample.json is checked against them in tests.

export type Percentiles = { p50: number; p95: number; p99: number };

export type Totals = {
  requests: number;
  errors: number;
  error_rate: number;
  fallbacks: number;
  fallback_rate: number;
  stream_requests: number;
  requests_per_min: number;
  latency_ms: Percentiles;
  ttft_ms: Percentiles;
  prompt_tokens: number;
  completion_tokens: number;
  cost_usd: number;
};

export type Bucket = {
  start: string;
  requests: number;
  errors: number;
  p50_ms: number;
  p95_ms: number;
};

export type ProviderStats = {
  provider: string;
  attempts: number;
  served: number;
  failure_rate: number;
  status_2xx: number;
  status_429: number;
  status_4xx: number;
  status_5xx: number;
  network_errors: number;
  latency_ms: Percentiles;
  tokens: number;
  cost_usd: number;
};

export type RouteStats = {
  route: string;
  requests: number;
  errors: number;
  fallbacks: number;
  latency_ms: Percentiles;
};

export type RecentRequest = {
  time: string;
  request_id: string;
  route: string;
  requested_model: string;
  provider: string;
  model: string;
  stream: boolean;
  status: number;
  latency_ms: number;
  ttft_ms: number;
  attempts: number;
  tried: string;
  prompt_tokens: number;
  completion_tokens: number;
  cost_usd: number;
  error?: string;
};

export type Metrics = {
  generated_at: string;
  started_at: string;
  window_seconds: number;
  capacity: number;
  totals: Totals;
  series: Bucket[];
  providers: ProviderStats[];
  routes: RouteStats[];
  recent: RecentRequest[];
};

/** Error body the /api/metrics route returns when it can't get metrics. */
export type MetricsError = {
  error: { type: "gateway_unreachable" | "gateway_error" | "invalid_request"; message: string };
};

/** Time windows offered in the UI; values are Go durations the gateway accepts. */
export const WINDOWS = [
  { value: "5m", label: "5 min" },
  { value: "15m", label: "15 min" },
  { value: "1h", label: "1 hour" },
  { value: "6h", label: "6 hours" },
  { value: "24h", label: "24 hours" },
] as const;

export type Window = (typeof WINDOWS)[number]["value"];

export const DEFAULT_WINDOW: Window = "15m";

export function isWindow(v: string | null | undefined): v is Window {
  return WINDOWS.some((w) => w.value === v);
}

/** True when a request counts as an error; matches usage.IsError in Go. */
export function isErrorStatus(r: Pick<RecentRequest, "status" | "error">, streamError = false): boolean {
  return streamError || r.status >= 500 || r.status === 429;
}

/** One attempt parsed from a "tried" chain like "gemini:429 groq:200". */
export type TriedStep = { provider: string; status: number };

export function parseTried(tried: string): TriedStep[] {
  return tried
    .split(" ")
    .filter(Boolean)
    .map((part) => {
      const i = part.lastIndexOf(":");
      return { provider: part.slice(0, i), status: Number(part.slice(i + 1)) };
    })
    .filter((s) => s.provider !== "" && Number.isFinite(s.status));
}
