// @vitest-environment node

import { readFile } from "node:fs/promises";

import { expect, it } from "vitest";

it("retains failed output and the supported native packaged targets", async () => {
  const source = await readFile(new URL("../../playwright.config.ts", import.meta.url), "utf8");

  expect(source).toContain('preserveOutput: "failures-only"');
  expect(source).toContain("timeout: 120_000");
  expect(source).toContain('target !== "darwin-arm64" && target !== "win32-x64"');
  expect(source).toContain('throw new Error("UNSUPPORTED_PACKAGED_E2E_TARGET")');
  expect(source).toMatch(
    /testMatch:\s*\[\s*"foundation\.spec\.ts",\s*"financial-workspace\.spec\.ts",\s*"current-workflows\.spec\.ts",\s*"bas-lifecycle\.spec\.ts",\s*"bas-preparation\.spec\.ts",\s*"bas-preparation-guards\.spec\.ts",\s*"invoice-income\.spec\.ts",\s*"reporting-exports\.spec\.ts",\s*"purchase-payments\.spec\.ts",?\s*\]/,
  );
  expect(source).toContain('testMatch: ["sbr-readiness.spec.ts"]');
  expect(source).toContain('target === "darwin-arm64"');
});
