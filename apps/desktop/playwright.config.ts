import { defineConfig } from "@playwright/test";

const target = `${process.platform}-${process.arch}`;
if (target !== "darwin-arm64" && target !== "win32-x64") {
  throw new Error("UNSUPPORTED_PACKAGED_E2E_TARGET");
}

export default defineConfig({
  expect: { timeout: 10_000 },
  outputDir: "test-results",
  preserveOutput: "failures-only",
  projects: [{ name: target, testMatch: "foundation.spec.ts" }],
  reporter: "list",
  retries: process.env.CI ? 2 : 0,
  testDir: "tests/e2e",
  timeout: 30_000,
  workers: 1,
});
