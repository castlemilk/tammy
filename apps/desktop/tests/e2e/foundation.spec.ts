import { expect, test } from "./fixtures";

const expectedTarget = `${process.platform}-${process.arch}`;
const futureModules = ["Accounts", "Journal", "BAS", "Submissions", "Audit"] as const;

test("runs the packaged desktop foundation offline and exits cleanly", async ({
  electronHarness,
}) => {
  expect(test.info().project.name).toBe(expectedTarget);

  const { consoleErrors, page, pageErrors, startupObserved } = electronHarness;
  await expect(page).toHaveTitle("Tammy");
  await startupObserved;
  await expect(page.getByText("Local engine ready", { exact: true })).toBeVisible();
  await expect(page.getByText("Offline", { exact: true })).toBeVisible();
  await expect(page.getByText("No cloud required", { exact: true })).toBeVisible();

  const apiVersion = page.locator("dt", { hasText: "API version" }).locator("+ dd");
  const coreVersion = page.locator("dt", { hasText: "Core version" }).locator("+ dd");
  await expect(apiVersion).toHaveText("tammy.v1");
  await expect(coreVersion).not.toBeEmpty();
  for (const moduleName of futureModules) {
    await expect(page.getByText(moduleName, { exact: true })).toHaveCount(0);
  }

  expect(await page.evaluate(() => Object.keys(window.tammy))).toEqual(["getSystemDiagnostics"]);
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

  const fetchResults = await page.evaluate(async () => {
    const attempt = async (url: string) => {
      try {
        await fetch(url);
        return "resolved";
      } catch {
        return "rejected";
      }
    };
    return Promise.all([
      attempt("https://example.com/tammy-csp-probe"),
      attempt("http://example.com/tammy-csp-probe"),
    ]);
  });
  expect(fetchResults).toEqual(["rejected", "rejected"]);
  await expect
    .poll(async () => windowDiagnostics(page))
    .toEqual({
      apiVersion: "tammy.v1",
      networkRequired: false,
      runtimeMode: "offline",
    });

  const retry = page.getByRole("button", { name: "Retry local engine" });
  const workspace = page.getByRole("button", { name: "Workspace setup comes next" });
  await expect(retry).toHaveCount(0);
  await expect(workspace).toBeDisabled();
  await page.keyboard.press("Tab");
  await expect(page.getByRole("link", { name: "Overview" })).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(workspace).not.toBeFocused();

  expect(pageErrors).toEqual([]);
  expect(
    consoleErrors.filter(
      (message) =>
        !message.includes("Content Security Policy") &&
        !message.includes("Refused to connect") &&
        !message.includes("ERR_INTERNET_DISCONNECTED"),
    ),
  ).toEqual([]);
});

async function windowDiagnostics(page: import("@playwright/test").Page) {
  const diagnostics = await page.evaluate(() => window.tammy.getSystemDiagnostics());
  return {
    apiVersion: diagnostics.apiVersion,
    networkRequired: diagnostics.networkRequired,
    runtimeMode: diagnostics.runtimeMode,
  };
}
