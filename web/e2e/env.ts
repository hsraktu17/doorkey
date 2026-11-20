// Shared by playwright.config.ts and the specs.
export const GATEWAY_PORT = 18090; // 18089 is left for make demo-gateway
export const WEB_PORT = 13089;
export const GATEWAY_URL = `http://127.0.0.1:${GATEWAY_PORT}`;
export const E2E_KEY = "e2e-test-key"; // test-only key for the mocks-only gateway
