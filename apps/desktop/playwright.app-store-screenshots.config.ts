import { defineConfig } from "@playwright/test";

if (`${process.platform}-${process.arch}` !== "darwin-arm64") {
  throw new Error("UNSUPPORTED_APP_STORE_SCREENSHOT_TARGET");
}

export default defineConfig({
  expect: { timeout: 15_000 },
  outputDir: "test-results/app-store-screenshots",
  preserveOutput: "failures-only",
  projects: [
    {
      name: "darwin-arm64-app-store-screenshots",
      testMatch: ["app-store-screenshots.spec.ts"],
    },
  ],
  reporter: "list",
  retries: 0,
  testDir: "tests/e2e",
  timeout: 180_000,
  workers: 1,
});
