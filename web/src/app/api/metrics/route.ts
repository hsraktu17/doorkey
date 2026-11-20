import type { NextRequest } from "next/server";
import { fetchMetrics } from "@/lib/gateway";
import { DEFAULT_WINDOW, isWindow, WINDOWS, type MetricsError } from "@/lib/metrics";

// GET /api/metrics?window=15m proxies the gateway's /admin/metrics. Reading the
// request makes this run per request (never prerendered or cached).
export async function GET(request: NextRequest) {
  const raw = request.nextUrl.searchParams.get("window") ?? DEFAULT_WINDOW;
  if (!isWindow(raw)) {
    const body: MetricsError = {
      error: {
        type: "invalid_request",
        message: `window must be one of ${WINDOWS.map((w) => w.value).join(", ")}`,
      },
    };
    return Response.json(body, { status: 400 });
  }

  const result = await fetchMetrics(raw);
  const headers = { "Cache-Control": "no-store" };
  if (!result.ok) {
    return Response.json(result.body, { status: result.status, headers });
  }
  return Response.json(result.metrics, { headers });
}
