import { describe, expect, it } from "vitest";
import { formatClock, formatClockSeconds, formatCount, formatMs, formatPct, formatUSD, formatUptime, statusTone } from "./format";

describe("format", () => {
  it("formats counts", () => {
    expect(formatCount(0)).toBe("0");
    expect(formatCount(1284)).toBe("1,284");
    expect(formatCount(12_900)).toBe("12.9K");
    expect(formatCount(4_200_000)).toBe("4.2M");
    expect(formatCount(Number.NaN)).toBe("–");
  });

  it("formats milliseconds", () => {
    expect(formatMs(0)).toBe("–");
    expect(formatMs(0.214)).toBe("0.21 ms");
    expect(formatMs(0.004)).toBe("<0.01 ms");
    expect(formatMs(4.5)).toBe("4.5 ms");
    expect(formatMs(84.4)).toBe("84 ms");
    expect(formatMs(1240)).toBe("1.24 s");
  });

  it("formats ratios as percentages", () => {
    expect(formatPct(0)).toBe("0%");
    expect(formatPct(1)).toBe("100%");
    expect(formatPct(0.042)).toBe("4.2%");
    expect(formatPct(0.3333)).toBe("33%");
  });

  it("keeps tiny costs visible", () => {
    expect(formatUSD(0)).toBe("$0");
    expect(formatUSD(0.0001234)).toBe("$0.00012");
    expect(formatUSD(1.5)).toBe("$1.50");
  });

  it("formats clock times in a given zone", () => {
    expect(formatClock("2026-11-20T10:03:30Z", "UTC")).toBe("10:03");
    expect(formatClockSeconds("2026-11-20T10:03:30Z", "Asia/Kolkata")).toBe("15:33:30");
    expect(formatClock("nope")).toBe("–");
  });

  it("formats uptime", () => {
    expect(formatUptime("2026-11-20T10:00:00Z", "2026-11-20T10:00:00Z")).toBe("0m");
    expect(formatUptime("2026-11-20T10:00:00Z", "2026-11-20T10:00:20Z")).toBe("1m");
    expect(formatUptime("2026-11-20T09:00:30Z", "2026-11-20T10:03:30Z")).toBe("1h 3m");
    expect(formatUptime("2026-11-18T09:00:00Z", "2026-11-20T10:00:00Z")).toBe("2d 1h");
  });

  it("maps statuses to tones", () => {
    expect(statusTone(200)).toBe("good");
    expect(statusTone(429)).toBe("warning");
    expect(statusTone(401)).toBe("serious");
    expect(statusTone(499)).toBe("neutral");
    expect(statusTone(502)).toBe("critical");
    expect(statusTone(0)).toBe("neutral");
  });
});
