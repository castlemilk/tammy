import { defineConfig } from "@playwright/test";

export default defineConfig({
  outputDir: "test-results/electron-vite-development",
  reporter: "list",
  retries: 0,
  testDir: "tests/e2e",
  testMatch: "electron-vite-development.spec.ts",
  timeout: 30_000,
  workers: 1,
});
