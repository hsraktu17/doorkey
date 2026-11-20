import { describe, expect, it } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { sampleMetrics } from "../../test/fixture";
import { StatTiles, tilesFor } from "./StatTiles";
import { ProviderTable, RecentRequests, RoutesTable, TriedChain, providerSegments } from "./tables";
import { LatencyChart, TrafficChart } from "./charts";
import { WindowPicker } from "./WindowPicker";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";

const m = sampleMetrics();

describe("StatTiles", () => {
  it("shows headline numbers from the totals", () => {
    render(<StatTiles totals={m.totals} />);
    expect(screen.getByTestId("tile-requests")).toHaveTextContent("6");
    expect(screen.getByTestId("tile-requests")).toHaveTextContent("1.2 per min");
    expect(screen.getByTestId("tile-errors")).toHaveTextContent("50%");
    expect(screen.getByTestId("tile-errors")).toHaveTextContent("3 errors");
    expect(screen.getByTestId("tile-latency")).toHaveTextContent("900 ms");
    expect(screen.getByTestId("tile-ttft")).toHaveTextContent("50 ms");
    expect(screen.getByTestId("tile-fallbacks")).toHaveTextContent("33%");
    expect(screen.getByTestId("tile-tokens")).toHaveTextContent("44");
  });

  it("says when there are no streams", () => {
    const t = tilesFor({ ...m.totals, stream_requests: 0 }).find((x) => x.testId === "tile-ttft")!;
    expect(t.value).toBe("–");
    expect(t.detail).toBe("no streams yet");
  });
});

describe("ProviderTable", () => {
  it("shows every attempt outcome, including 429s on the first target", () => {
    render(<ProviderTable providers={m.providers} />);
    expect(screen.getByTestId("outcomes-mock-flaky")).toHaveTextContent("2 429");
    expect(screen.getByTestId("outcomes-gemini")).toHaveTextContent("2 429, 1 other 4xx");
    const groq = within(screen.getByTestId("provider-groq"));
    expect(groq.getByText("33%")).toBeInTheDocument(); // 1 failed attempt of 3
  });

  it("keeps segment order fixed", () => {
    expect(providerSegments(m.providers[0]).map((s) => s.key)).toEqual(["2xx", "429", "4xx", "5xx", "net"]);
  });

  it("has an empty state", () => {
    render(<ProviderTable providers={[]} />);
    expect(screen.getByText("No provider attempts in this window.")).toBeInTheDocument();
  });
});

describe("RoutesTable", () => {
  it("lists routes", () => {
    render(<RoutesTable routes={m.routes} />);
    expect(within(screen.getByTestId("route-demo")).getAllByText("2")).toHaveLength(2); // requests, fallbacks
  });
});

describe("RecentRequests", () => {
  it("shows the attempt chain and who served it", () => {
    render(<RecentRequests recent={m.recent} timeZone="UTC" />);
    const rows = screen.getAllByTestId("recent-row");
    expect(rows).toHaveLength(6);
    const r1 = rows[rows.length - 1];
    expect(r1).toHaveTextContent("mock/mock-2");
    expect(within(r1).getByLabelText("Attempts in order")).toHaveTextContent("mock-flaky429→mock200");
    expect(rows[3]).toHaveTextContent("none"); // r3: every provider failed
  });

  it("renders a network error step", () => {
    render(<TriedChain tried="groq:0 gemini:200" />);
    expect(screen.getByText("network")).toBeInTheDocument();
  });
});

describe("charts", () => {
  it("offer a table view of every value", () => {
    render(<TrafficChart series={m.series} />);
    render(<LatencyChart series={m.series} />);
    expect(screen.getByRole("img", { name: /Requests per minute over 6 minutes/ })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: /latency per minute over 6 minutes/ })).toBeInTheDocument();
    expect(screen.getAllByText("Show as table")).toHaveLength(2);
    expect(screen.getByText("120 ms")).toBeInTheDocument(); // p95 at 10:01 in the latency table
  });
});

describe("WindowPicker", () => {
  it("marks the active window and reports changes", async () => {
    const onChange = vi.fn();
    render(<WindowPicker value="15m" onChange={onChange} />);
    expect(screen.getByRole("radio", { name: "15 min" })).toHaveAttribute("aria-checked", "true");
    await userEvent.click(screen.getByRole("radio", { name: "1 hour" }));
    expect(onChange).toHaveBeenCalledWith("1h");
  });
});
