#!/usr/bin/env node
// Sends a steady mix of requests through a doorkey gateway so the dashboard has
// something to show. Use it against a mocks-only config so it costs nothing:
//
//   go run ./cmd/doorkey -config web/e2e/doorkey.e2e.json      (from the repo root)
//   DOORKEY_URL=http://127.0.0.1:18089 npm run traffic          (from web/)
//
// Flags: --count N (default: run until Ctrl-C), --rate N per second (default 2),
// --routes a,b,c (default demo,default,slow,broken).

const args = Object.fromEntries(
  process.argv.slice(2).reduce((acc, a, i, all) => (a.startsWith("--") ? [...acc, [a.slice(2), all[i + 1]]] : acc), []),
);
const url = (process.env.DOORKEY_URL || "http://127.0.0.1:18089").replace(/\/+$/, "");
const key = process.env.DOORKEY_API_KEY || "";
const count = args.count ? Number(args.count) : Infinity;
const rate = Number(args.rate || 2);
const routes = (args.routes || "demo,default,slow,broken").split(",");
// Weighted so most traffic succeeds: demo and default often, broken rarely.
const weights = { demo: 4, default: 4, slow: 2, broken: 1 };
const bag = routes.flatMap((r) => Array(weights[r] ?? 1).fill(r));

async function send(i) {
  const model = bag[Math.floor(Math.random() * bag.length)];
  const stream = Math.random() < 0.3;
  const started = performance.now();
  try {
    const res = await fetch(`${url}/v1/chat/completions`, {
      method: "POST",
      headers: { "Content-Type": "application/json", ...(key ? { Authorization: `Bearer ${key}` } : {}) },
      body: JSON.stringify({ model, stream, messages: [{ role: "user", content: `traffic #${i}` }] }),
    });
    await res.text(); // drain (streams included)
    const tried = res.headers.get("x-doorkey-attempts") ?? "?";
    console.log(`#${i} ${model}${stream ? " (stream)" : ""} → ${res.status} in ${Math.round(performance.now() - started)} ms, attempts ${tried}`);
    return res.status;
  } catch (err) {
    console.error(`#${i} ${model} → ${err.cause?.code ?? err.message} (is the gateway running at ${url}?)`);
    return 0;
  }
}

const pending = [];
for (let i = 1; i <= count; i++) {
  pending.push(send(i));
  await new Promise((r) => setTimeout(r, 1000 / rate));
}
const statuses = await Promise.all(pending);
if (statuses.every((s) => s === 0)) process.exit(1);
