import { defineConfig } from "@playwright/test";

if (`${process.platform}-${process.arch}` !== "darwin-arm64") {
  throw new Error("UNSUPPORTED_APP_STORE_PRIVACY_TARGET");
}

export default defineConfig({
  expect: { timeout: 15_000 },
  fullyParallel: false,
  outputDir: "test-results/app-store-privacy",
  preserveOutput: "failures-only",
  projects: [
    {
      name: "darwin-arm64-app-store-privacy",
      testMatch: ["app-store-privacy.spec.ts"],
    },
  ],
  reporter: "list",
  retries: 0,
  testDir: "tests/e2e",
  timeout: 180_000,
  workers: 1,
});
