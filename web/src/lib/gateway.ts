// Server-side access to the doorkey gateway. Only route handlers import this;
// the gateway URL and key never reach the browser.

import type { Metrics, MetricsError, Window } from "./metrics";

export const DEFAULT_GATEWAY_URL = "http://127.0.0.1:8080";
const TIMEOUT_MS = 4000;

export type GatewayResult =
  | { ok: true; metrics: Metrics }
  | { ok: false; status: number; body: MetricsError };

function config() {
  return {
    url: (process.env.DOORKEY_URL || DEFAULT_GATEWAY_URL).replace(/\/+$/, ""),
    apiKey: process.env.DOORKEY_API_KEY || "",
  };
}

/** Fetches /admin/metrics, turning every failure into a typed error. */
export async function fetchMetrics(window: Window, fetchImpl: typeof fetch = fetch): Promise<GatewayResult> {
  const { url, apiKey } = config();
  const headers: Record<string, string> = { Accept: "application/json" };
  if (apiKey) headers.Authorization = `Bearer ${apiKey}`;

  let res: Response;
  try {
    res = await fetchImpl(`${url}/admin/metrics?window=${encodeURIComponent(window)}`, {
      headers,
      cache: "no-store",
      signal: AbortSignal.timeout(TIMEOUT_MS),
    });
  } catch (err) {
    const reason = err instanceof Error ? err.message : String(err);
    return {
      ok: false,
      status: 502,
      body: { error: { type: "gateway_unreachable", message: `Can't reach the gateway at ${url} (${reason})` } },
    };
  }

  if (!res.ok) {
    let detail = "";
    try {
      const body = (await res.json()) as { error?: { message?: string } };
      detail = body.error?.message ?? "";
    } catch {
      // not JSON; keep the status only
    }
    const hint =
      res.status === 401
        ? "the gateway rejected the key: set DOORKEY_API_KEY for the dashboard to the gateway's key"
        : res.status === 404
          ? "this gateway has no /admin/metrics endpoint"
          : detail || res.statusText;
    return {
      ok: false,
      status: res.status === 401 || res.status === 404 ? 502 : res.status >= 500 ? 502 : res.status,
      body: { error: { type: "gateway_error", message: `Gateway returned ${res.status}: ${hint}` } },
    };
  }

  try {
    return { ok: true, metrics: (await res.json()) as Metrics };
  } catch {
    return {
      ok: false,
      status: 502,
      body: { error: { type: "gateway_error", message: "Gateway returned invalid JSON" } },
    };
  }
}
