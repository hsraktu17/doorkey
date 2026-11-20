import { expect, test, type APIRequestContext } from "@playwright/test";
import { E2E_KEY, GATEWAY_URL } from "./env";

async function chat(request: APIRequestContext, model: string, stream = false) {
  const res = await request.post(`${GATEWAY_URL}/v1/chat/completions`, {
    headers: { Authorization: `Bearer ${E2E_KEY}` },
    data: { model, stream, messages: [{ role: "user", content: "hi" }] },
  });
  await res.body();
  return res.status();
}

test.describe.serial("dashboard", () => {
  test("starts with an empty state", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByTestId("empty-state")).toBeVisible();
    await expect(page.getByTestId("tile-requests")).toContainText("0");
  });

  test("shows traffic, fallbacks and provider outcomes", async ({ page, request }) => {
    expect(await chat(request, "demo")).toBe(200); // mock-flaky 429 → mock 200
    expect(await chat(request, "demo", true)).toBe(200);
    expect(await chat(request, "default")).toBe(200);
    expect(await chat(request, "broken")).toBe(502);

    await page.goto("/");
    await expect(page.getByTestId("tile-requests")).toContainText("4");
    await expect(page.getByTestId("tile-errors")).toContainText("25%");
    await expect(page.getByTestId("tile-fallbacks")).toContainText("2 served after a failed attempt");

    await expect(page.getByTestId("outcomes-mock-flaky")).toHaveText("2 429");
    await expect(page.getByTestId("outcomes-mock-down")).toHaveText("1 5xx");
    await expect(page.getByTestId("route-demo")).toBeVisible();

    const rows = page.getByTestId("recent-row");
    await expect(rows).toHaveCount(4);
    const demoRow = rows.filter({ hasText: "mock/mock-2" }).first();
    await expect(demoRow.getByLabel("Attempts in order")).toHaveText(/mock-flaky\s*429\s*→\s*mock\s*200/);

    await expect(page.getByRole("img", { name: /Requests per minute/ })).toBeVisible();
    await expect(page.getByRole("img", { name: /latency per minute/ })).toBeVisible();
  });

  test("picks up new traffic by polling", async ({ page, request }) => {
    await page.goto("/");
    await expect(page.getByTestId("tile-requests")).toContainText("4");
    await chat(request, "slow");
    await expect(page.getByTestId("tile-requests")).toContainText("5", { timeout: 12_000 });
  });

  test("the window picker updates the URL and query", async ({ page }) => {
    await page.goto("/?window=1h");
    await expect(page.getByRole("radio", { name: "1 hour" })).toHaveAttribute("aria-checked", "true");
    const req = page.waitForRequest((r) => r.url().includes("/api/metrics?window=6h"));
    await page.getByRole("radio", { name: "6 hours" }).click();
    await req;
    await expect(page).toHaveURL(/window=6h/);
  });

  test("explains when the gateway is down", async ({ page }) => {
    await page.route("**/api/metrics**", (route) =>
      route.fulfill({
        status: 502,
        json: { error: { type: "gateway_unreachable", message: "Can't reach the gateway at http://127.0.0.1:1" } },
      }),
    );
    await page.goto("/");
    await expect(page.getByTestId("gateway-down")).toContainText("Gateway unreachable");
  });

  test("the API route rejects unknown windows", async ({ request }) => {
    const res = await request.get("/api/metrics?window=7d");
    expect(res.status()).toBe(400);
  });
});
