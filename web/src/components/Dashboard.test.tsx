import { afterEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { sampleMetrics } from "../../test/fixture";
import { Dashboard } from "./Dashboard";

const ok = (body: unknown) => Promise.resolve(Response.json(body));

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("Dashboard", () => {
  it("loads metrics for the initial window", async () => {
    const f = vi.fn(() => ok(sampleMetrics()));
    vi.stubGlobal("fetch", f);
    render(<Dashboard initialWindow="5m" />);
    expect(await screen.findByTestId("tile-requests")).toHaveTextContent("6");
    expect(f.mock.calls[0][0 as never]).toBe("/api/metrics?window=5m");
    expect(screen.getByTestId("status-line")).toHaveTextContent("resets on restart");
    expect(screen.getAllByTestId("recent-row")).toHaveLength(6);
  });

  it("shows the empty state with the demo curl", async () => {
    const empty = sampleMetrics();
    empty.totals.requests = 0;
    vi.stubGlobal("fetch", vi.fn(() => ok(empty)));
    render(<Dashboard initialWindow="15m" />);
    expect(await screen.findByTestId("empty-state")).toHaveTextContent('"model":"demo"');
  });

  it("explains when the gateway is down", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(Response.json({ error: { type: "gateway_unreachable", message: "Can't reach the gateway" } }, { status: 502 }))),
    );
    render(<Dashboard initialWindow="15m" />);
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Gateway unreachable");
    expect(alert).toHaveTextContent("Can't reach the gateway");
  });

  it("keeps the last good data when a later poll fails", async () => {
    let calls = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(() => (calls++ === 0 ? ok(sampleMetrics()) : Promise.reject(new TypeError("fetch failed")))),
    );
    render(<Dashboard initialWindow="15m" pollMs={20} />);
    expect(await screen.findByTestId("tile-requests")).toBeInTheDocument();
    expect(await screen.findByRole("alert")).toHaveTextContent("showing the last good data");
    expect(screen.getByTestId("tile-requests")).toHaveTextContent("6");
  });

  it("polls while live and stops when paused", async () => {
    const f = vi.fn(() => ok(sampleMetrics()));
    vi.stubGlobal("fetch", f);
    render(<Dashboard initialWindow="15m" pollMs={20} />);
    await waitFor(() => expect(f.mock.calls.length).toBeGreaterThanOrEqual(3));
    await userEvent.click(screen.getByRole("button", { name: "Live" }));
    expect(screen.getByRole("button", { name: "Paused" })).toHaveAttribute("aria-pressed", "false");
    const n = f.mock.calls.length;
    await act(() => new Promise((r) => setTimeout(r, 100)));
    expect(f.mock.calls.length).toBe(n);
  });

  it("refetches and updates the URL when the window changes", async () => {
    const f = vi.fn(() => ok(sampleMetrics()));
    vi.stubGlobal("fetch", f);
    render(<Dashboard initialWindow="15m" />);
    await screen.findByTestId("tile-requests");
    await userEvent.click(screen.getByRole("radio", { name: "6 hours" }));
    await waitFor(() => expect(f).toHaveBeenLastCalledWith("/api/metrics?window=6h", expect.anything()));
    expect(globalThis.location.search).toBe("?window=6h");
  });
});
