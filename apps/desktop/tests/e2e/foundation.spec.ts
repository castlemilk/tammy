import { readFileSync } from "node:fs";

import { consumeExpectedCspViolations } from "./csp-console";
import { expect, test } from "./fixtures";

const expectedTarget = `${process.platform}-${process.arch}`;
const preloadMethods = JSON.parse(
  readFileSync(new URL("../../src/shared/preload-methods.json", import.meta.url), "utf8"),
) as readonly string[];
const probeUrls = [
  "https://example.com/tammy-csp-probe",
  "http://example.com/tammy-csp-probe",
] as const;

test("runs the packaged first-run journey offline and exits cleanly", async ({
  electronHarness,
}) => {
  expect(test.info().project.name).toBe(expectedTarget);

  const { consoleErrors, page, pageErrors } = electronHarness;
  await expect(page).toHaveTitle("Tammy");
  await expect(page.getByRole("heading", { name: "Create your local workspace" })).toBeVisible();
  await expect(
    page.getByText("Your accounting data stays encrypted on this device."),
  ).toBeVisible();
  await expect(page.getByLabel("Your name")).toBeVisible();
  await expect(page.getByLabel("Email or username")).toBeVisible();
  await expect(page.getByLabel("Business legal name")).toBeVisible();
  await expect(page.getByLabel("Business display name")).toBeVisible();
  await expect(page.getByLabel("ABN")).toBeVisible();
  await expect(page.getByLabel("Workspace passphrase")).toHaveAttribute("type", "password");
  await expect(page.getByLabel("Administrator password")).toHaveAttribute("type", "password");

  expect(await page.evaluate(() => Object.keys(window.tammy))).toEqual(preloadMethods);
  expect(
    await page.evaluate(() => ({
      Buffer: typeof (globalThis as { Buffer?: unknown }).Buffer,
      global: typeof (globalThis as { global?: unknown }).global,
      module: typeof (globalThis as { module?: unknown }).module,
      process: typeof (globalThis as { process?: unknown }).process,
      require: typeof (globalThis as { require?: unknown }).require,
    })),
  ).toEqual({
    Buffer: "undefined",
    global: "undefined",
    module: "undefined",
    process: "undefined",
    require: "undefined",
  });

  expect(await page.evaluate(() => navigator.onLine)).toBe(false);
  const fetchResults = await page.evaluate(async (urls) => {
    const attempt = async (url: string) => {
      try {
        await fetch(url);
        return "resolved";
      } catch {
        return "rejected";
      }
    };
    return Promise.all(urls.map(attempt));
  }, probeUrls);
  expect(fetchResults).toEqual(["rejected", "rejected"]);
  await expect
    .poll(() => {
      try {
        consumeExpectedCspViolations(consoleErrors, probeUrls);
        return true;
      } catch {
        return false;
      }
    })
    .toBe(true);

  await expect
    .poll(async () => windowDiagnostics(page))
    .toEqual({
      apiVersion: "tammy.v1",
      networkRequired: false,
      runtimeMode: "offline",
    });

  await page.getByRole("button", { name: "Privacy and support" }).click();
  await expect(page.getByRole("heading", { name: "Privacy and support" })).toBeVisible();
  await page.getByRole("button", { name: "Back to Tammy" }).click();
  await expect(page.getByRole("heading", { name: "Create your local workspace" })).toBeVisible();
  await page.keyboard.press("Tab");
  await expect(page.getByLabel("Your name")).toBeFocused();

  expect(consumeExpectedCspViolations(consoleErrors, probeUrls)).toEqual([]);
  expect(pageErrors).toEqual([]);
});

async function windowDiagnostics(page: import("@playwright/test").Page) {
  const diagnostics = await page.evaluate(() => window.tammy.getSystemDiagnostics());
  return {
    apiVersion: diagnostics.apiVersion,
    networkRequired: diagnostics.networkRequired,
    runtimeMode: diagnostics.runtimeMode,
  };
}
