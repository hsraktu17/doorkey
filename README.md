# doorkey

A fast LLM gateway written in Go. Point any OpenAI SDK at doorkey and it routes
requests across free-tier LLM APIs (Gemini, Groq, OpenRouter), with retries, fallback, streaming and usage tracking.

> Status: early development (Phase 1). Text chat only: `tools`, `response_format`
> (JSON mode), `n`, `seed` and image inputs are not forwarded to providers yet.

## How it works

```
Client (OpenAI SDK, change base_url)
        │  POST /v1/chat/completions  (+ optional x-doorkey-config header)
        ▼
handler → router (retry / backoff / fallback) → provider adapter → LLM API
        └→ usage (tokens, latency, cost)
```

- Every request and response uses the OpenAI chat format.
- Each provider has an adapter that translates requests, responses and SSE streams.
- Fallback only happens before the first chunk reaches the client.

## Project layout

```
cmd/doorkey/          entry point
internal/api/         OpenAI-format types
internal/handler/     HTTP handlers, auth, route selection, SSE write loop
internal/router/      retries, backoff, fallback
internal/providers/   provider interface + adapters:
                        openai    OpenAI-compatible APIs (Groq, OpenRouter, Hugging Face)
                        gemini    Google Gemini API
                        mock      fake provider for tests and demos
internal/sse/         SSE reader and writer
internal/config/      config file loading and provider setup
internal/usage/       pricing, cost, in-memory metrics for the dashboard
internal/logger/      slog setup and the per-request log line
web/                  Next.js APM dashboard (see below)
```

Planned, not created yet: `internal/dal/` (Postgres, Phase 2) and an Anthropic adapter (paid only).

## Free providers

| Provider | Get a key | Free limits (check the console, they change) | Note |
|---|---|---|---|
| Gemini | [aistudio.google.com/apikey](https://aistudio.google.com/apikey) | Per model, shown in AI Studio | Free-tier prompts may be used to improve Google products |
| Groq | [console.groq.com/keys](https://console.groq.com/keys) | ~30 req/min, 1,000 req/day per model | Very fast; low tokens per minute |
| OpenRouter | [openrouter.ai/keys](https://openrouter.ai/keys) | 20 req/min, 50 req/day on `:free` models | Free model IDs come and go |

None of these need a card. Hugging Face is also configured (`huggingface`, same
OpenAI-compatible adapter), but it is not free: free accounts get no Inference
Providers credit, so it is left out of the routes. Consumer chat subscriptions (Google AI Plus, ChatGPT
Plus/Go) do not include API access, so keys come from the consoles above.

## Running

```bash
cp doorkey.example.json doorkey.json
cp .env.example .env        # fill in the keys you have
make run                    # listens on 127.0.0.1:8080
```

Try it with no keys at all (the `demo` route uses mock providers):

```bash
curl -N localhost:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"demo","stream":true,"messages":[{"role":"user","content":"hello"}]}'
```

Or point any OpenAI SDK at it:

```python
from openai import OpenAI
client = OpenAI(base_url="http://localhost:8080/v1", api_key="anything")
client.chat.completions.create(model="default", messages=[{"role": "user", "content": "hi"}])
```

### Choosing a route

The `model` field picks the route:

| `model` value | What runs |
|---|---|
| A route name: `default`, `fast`, `demo` | That route from the config |
| `provider/model`, e.g. `groq/openai/gpt-oss-20b` | That provider and model only |
| Anything else | The `default` route |

An `X-Doorkey-Config` header with a JSON route overrides all of these for one request:

```bash
-H 'X-Doorkey-Config: {"targets":[{"provider":"groq","model":"openai/gpt-oss-20b"},{"provider":"gemini","model":"gemini-2.5-flash"}],"retries":1}'
```

Routes in the example config:

- `default`: Gemini → Groq → OpenRouter.
- `fast`: Groq first, then Gemini Flash-Lite.
- `demo`: mock providers, works with no keys and no internet.

Free model IDs change often. Check them against each provider's model list.

### Fallback rules

| Upstream error | What doorkey does |
|---|---|
| 5xx, 408, timeout, network error | Retry with backoff, then the next target |
| 429 | Next target at once (free tiers limit per minute); the last target retries |
| 401, 402, 403, 404, 413 | Next target, no retry |
| 400 | Stop: the request itself is invalid |

Streams only fall back before the first chunk reaches the client.

### Response headers and logs

Every response has `X-Doorkey-Request-Id`, `X-Doorkey-Provider`, `X-Doorkey-Model` and
`X-Doorkey-Attempts`. Each request also writes one log line to stdout with the route,
provider, status, latency, time to first token, tokens, cost, and what was tried
(for example `"tried":"gemini:429 groq:200"`).

### Settings

| Env var / flag | Purpose |
|---|---|
| `-config`, `DOORKEY_CONFIG` | Config file (default `doorkey.json`, falling back to the example) |
| `-addr`, `DOORKEY_ADDR` | Listen address, overrides `listen` in the config |
| `DOORKEY_API_KEY` | If set, clients must send `Authorization: Bearer <key>` (also guards `/admin/metrics`) |
| `DOORKEY_LOG_FORMAT` | `json` (default) or `text` |
| `DOORKEY_LOG_LEVEL` | `debug`, `info` (default), `warn` or `error` |

A provider whose key env var is empty is disabled: it answers 401, so routes skip past it.

## Dashboard

`web/` is a Next.js 16 APM dashboard for the gateway: requests, error rate, latency
p50/p95/p99, time to first token, fallback rate, tokens and cost; requests and latency
per minute; per-provider attempt outcomes (2xx / 429 / 4xx / 5xx / network), so a
first-choice provider that keeps getting rate-limited shows up even when fallback
hides it from clients; routes; and the last 50 requests with their attempt chains.
It polls every 5 seconds and pauses while the tab is hidden.

The data comes from `GET /admin/metrics?window=15m` (1m to 24h), which the gateway
keeps in memory for the last 10,000 requests. It resets when the gateway restarts.
The browser only talks to the dashboard's own `/api/metrics`; the gateway URL and key
stay on the Next.js server.

Try it with mock providers only (nothing reaches a real API):

```bash
make web-install          # npm ci + Playwright's Chromium (~100 MB)
make demo-gateway         # terminal 1: mocks-only gateway on :18089
make traffic              # terminal 2: steady demo traffic
cd web && DOORKEY_URL=http://127.0.0.1:18089 npm run dev   # terminal 3
```

To watch your real gateway instead, copy `web/.env.example` to `web/.env.local` and set
`DOORKEY_URL` and `DOORKEY_API_KEY`, then `make web-dev`.

Tests:

- `make web-test`: ESLint, type check, and Vitest (formatters, the API route, components,
  the dashboard's polling and error states). The TypeScript types are checked against
  `internal/usage/testdata/metrics.sample.json`, which the Go tests generate
  (`go test ./internal/usage -run TestGolden -update`), so the two sides can't drift.
- `make web-e2e`: Playwright starts the mocks-only gateway and a production build,
  sends traffic, and checks the page.

## Roadmap

- **Phase 1:** `/v1/chat/completions`, OpenAI-compatible and Gemini adapters, streaming,
  retries, timeouts, fallback, request logs with tokens and cost, APM dashboard.
- **Phase 2:** virtual keys, per-key rate limits and budgets (Redis), weighted load
  balancing, exact-match cache.
- **Phase 3:** semantic cache (pgvector), guardrails, OpenTelemetry,
  Docker and Helm.

## Benchmarks

_To be measured._ Gateway overhead (p50/p99) at fixed RPS, and fallback recovery rate
under injected provider errors.
