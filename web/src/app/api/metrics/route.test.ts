// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";
import { sampleMetrics } from "../../../../test/fixture";
import { GET } from "./route";

const req = (qs = "") => new NextRequest(`http://localhost/api/metrics${qs}`);

describe("GET /api/metrics", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("proxies the gateway's metrics", async () => {
    const f = vi.fn(async () => Response.json(sampleMetrics()));
    vi.stubGlobal("fetch", f);
    const res = await GET(req("?window=5m"));
    expect(res.status).toBe(200);
    expect(res.headers.get("Cache-Control")).toBe("no-store");
    expect((await res.json()).totals.requests).toBe(6);
    expect(String((f.mock.calls[0] as unknown[])[0])).toContain("/admin/metrics?window=5m");
  });

  it("defaults to the 15m window", async () => {
    const f = vi.fn(async () => Response.json(sampleMetrics()));
    vi.stubGlobal("fetch", f);
    await GET(req());
    expect(String((f.mock.calls[0] as unknown[])[0])).toContain("window=15m");
  });

  it("rejects windows the UI doesn't offer without calling the gateway", async () => {
    const f = vi.fn();
    vi.stubGlobal("fetch", f);
    const res = await GET(req("?window=7d"));
    expect(res.status).toBe(400);
    expect((await res.json()).error.type).toBe("invalid_request");
    expect(f).not.toHaveBeenCalled();
  });

  it("returns 502 when the gateway is down", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => Promise.reject(new TypeError("fetch failed"))));
    const res = await GET(req());
    expect(res.status).toBe(502);
    expect((await res.json()).error.type).toBe("gateway_unreachable");
  });
});
