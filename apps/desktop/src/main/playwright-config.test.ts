// @vitest-environment node

import { readFile } from "node:fs/promises";

import { expect, it } from "vitest";

it("retains failed output and the supported native packaged targets", async () => {
  const source = await readFile(new URL("../../playwright.config.ts", import.meta.url), "utf8");

  expect(source).toContain('preserveOutput: "failures-only"');
  expect(source).toContain("timeout: 120_000");
  expect(source).toContain('target !== "darwin-arm64" && target !== "win32-x64"');
  expect(source).toContain('throw new Error("UNSUPPORTED_PACKAGED_E2E_TARGET")');
  expect(source).toContain('testMatch: ["foundation.spec.ts", "current-workflows.spec.ts"]');
  expect(source).toContain('testMatch: ["sbr-readiness.spec.ts"]');
  expect(source).toContain('target === "darwin-arm64"');
});
