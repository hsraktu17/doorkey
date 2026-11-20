import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { sampleMetrics } from "../../test/fixture";
import { fetchMetrics } from "./gateway";

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

describe("fetchMetrics", () => {
  beforeEach(() => {
    vi.stubEnv("DOORKEY_URL", "http://gw.test:9999/");
    vi.stubEnv("DOORKEY_API_KEY", "test-key");
  });
  afterEach(() => vi.unstubAllEnvs());

  it("calls /admin/metrics with the key and window", async () => {
    const f = vi.fn(async () => json(sampleMetrics()));
    const r = await fetchMetrics("1h", f as unknown as typeof fetch);
    expect(r.ok).toBe(true);
    const [url, init] = f.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("http://gw.test:9999/admin/metrics?window=1h");
    expect((init.headers as Record<string, string>).Authorization).toBe("Bearer test-key");
    expect(init.cache).toBe("no-store");
  });

  it("omits the header when no key is set", async () => {
    vi.stubEnv("DOORKEY_API_KEY", "");
    const f = vi.fn(async () => json(sampleMetrics()));
    await fetchMetrics("15m", f as unknown as typeof fetch);
    const [, init] = f.mock.calls[0] as unknown as [string, RequestInit];
    expect((init.headers as Record<string, string>).Authorization).toBeUndefined();
  });

  it("reports an unreachable gateway as 502", async () => {
    const r = await fetchMetrics("15m", (async () => {
      throw new TypeError("fetch failed");
    }) as unknown as typeof fetch);
    expect(r).toMatchObject({ ok: false, status: 502, body: { error: { type: "gateway_unreachable" } } });
    if (!r.ok) expect(r.body.error.message).toContain("http://gw.test:9999");
  });

  it("explains a rejected key", async () => {
    const r = await fetchMetrics("15m", (async () => json({ error: { message: "invalid key" } }, 401)) as unknown as typeof fetch);
    expect(r).toMatchObject({ ok: false, status: 502, body: { error: { type: "gateway_error" } } });
    if (!r.ok) expect(r.body.error.message).toContain("DOORKEY_API_KEY");
  });

  it("passes through other 4xx and maps 5xx to 502", async () => {
    const bad = await fetchMetrics("15m", (async () => json({ error: { message: "bad window" } }, 400)) as unknown as typeof fetch);
    expect(bad).toMatchObject({ ok: false, status: 400 });
    const down = await fetchMetrics("15m", (async () => new Response("boom", { status: 503 })) as unknown as typeof fetch);
    expect(down).toMatchObject({ ok: false, status: 502 });
  });

  it("rejects invalid JSON", async () => {
    const r = await fetchMetrics("15m", (async () => new Response("<html>", { status: 200 })) as unknown as typeof fetch);
    expect(r).toMatchObject({ ok: false, status: 502, body: { error: { message: "Gateway returned invalid JSON" } } });
  });
});
