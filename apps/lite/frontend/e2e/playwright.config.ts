import { defineConfig } from "@playwright/test";
import { fileURLToPath } from "node:url";

// L8 D-L8.1–D-L8.4: the real built frontend against the real Go graph (cmd/lite-e2e), in the browser already on the machine —
// Chrome on a Mac, Edge (WebView2's engine) on Windows with LITE_E2E_CHANNEL=msedge. No browser is downloaded.
const port = Number(process.env.LITE_E2E_PORT ?? 34199);
const frontend = fileURLToPath(new URL("..", import.meta.url));

export default defineConfig({
  testDir: ".",
  testMatch: /.*\.spec\.ts$/,
  timeout: 90_000,
  expect: { timeout: 15_000 },
  workers: 1,
  fullyParallel: false,
  retries: 0,
  reporter: [["list"]],
  outputDir: fileURLToPath(new URL("../../../../build/lite-e2e/results", import.meta.url)),
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    channel: process.env.LITE_E2E_CHANNEL ?? "chrome",
    viewport: { width: 1366, height: 768 },
    trace: "retain-on-failure",
  },
  webServer: {
    command: `go run ../../../cmd/lite-e2e -dist dist -addr 127.0.0.1:${port}`,
    cwd: frontend,
    url: `http://127.0.0.1:${port}/`,
    timeout: 240_000,
    reuseExistingServer: false,
    stdout: "pipe",
    stderr: "pipe",
  },
});
