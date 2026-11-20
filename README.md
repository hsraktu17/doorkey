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
internal/usage/       pricing, cost, request logs
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
`X-Doorkey-Attempts`. Each request also writes one JSON log line to stdout with the route,
provider, status, latency, time to first token, tokens, cost, and what was tried
(for example `"tried":"gemini:429 groq:200"`).

### Settings

| Env var / flag | Purpose |
|---|---|
| `-config`, `DOORKEY_CONFIG` | Config file (default `doorkey.json`, falling back to the example) |
| `-addr`, `DOORKEY_ADDR` | Listen address, overrides `listen` in the config |
| `DOORKEY_API_KEY` | If set, clients must send `Authorization: Bearer <key>` |

A provider whose key env var is empty is disabled: it answers 401, so routes skip past it.

## Roadmap

- **Phase 1:** `/v1/chat/completions`, OpenAI-compatible and Gemini adapters, streaming,
  retries, timeouts, fallback, request logs with tokens and cost.
- **Phase 2:** virtual keys, per-key rate limits and budgets (Redis), weighted load
  balancing, exact-match cache.
- **Phase 3:** semantic cache (pgvector), guardrails, dashboard, OpenTelemetry,
  Docker and Helm.

## Benchmarks

_To be measured._ Gateway overhead (p50/p99) at fixed RPS, and fallback recovery rate
under injected provider errors.
