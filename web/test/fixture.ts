import type { Metrics } from "@/lib/metrics";
// The Go metrics test writes this file (go test ./internal/usage -update), so
// the TypeScript side is checked against the gateway's real JSON contract.
import sample from "../../internal/usage/testdata/metrics.sample.json";

export function sampleMetrics(): Metrics {
  return structuredClone(sample) as Metrics;
}
