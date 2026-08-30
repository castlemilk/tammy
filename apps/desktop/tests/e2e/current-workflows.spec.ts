import {
  normalizeAccessibilitySnapshot,
  validateScreenshotFixture,
} from "../../../../scripts/check-app-store-screenshots.mjs";
import screenshotFixture from "../../release/macos/screenshots/fixture.json" with { type: "json" };
import { expect, test } from "./fixtures";
import { setupAndRunCurrentAccountingWorkflow } from "./support/current-accounting-workflow";

test("runs the current accounting workflows through the packaged app", async ({
  electronHarness,
}) => {
  await setupAndRunCurrentAccountingWorkflow(electronHarness.page, electronHarness, {
    fixedRendererClock: true,
  });
  const fixture = validateScreenshotFixture(screenshotFixture);
  const page = electronHarness.currentPage();
  const targets = [
    { heading: "Overview", link: "Overview", ready: "Nothing needs review" },
    {
      heading: "Documents",
      link: "Documents",
      ready: fixture.sourceDocument.sourceDisplayName,
    },
    { heading: "Trial balance", link: "Trial balance", ready: fixture.accounts[0]?.name },
    { heading: "Banking", link: "Banking", ready: "Reconciled" },
    { heading: "GST & BAS", link: "GST & BAS", ready: fixture.sourceDocument.supplierName },
  ] as const;
  for (const [index, target] of targets.entries()) {
    await page.getByRole("link", { name: target.link }).click();
    await expect(page.getByRole("heading", { level: 1, name: target.heading })).toBeVisible();
    if (!target.ready) throw new Error("APP_STORE_SCREENSHOT_FIXTURE_INVALID");
    await expect(page.getByText(target.ready, { exact: true }).first()).toBeVisible();
    if (target.heading === "Banking") {
      await page.getByLabel("Opening balance").fill("1000.00");
      await page
        .getByLabel("CSV rows")
        .fill("2024-05-12,HARBOUR OFFICE SUPPLIES WCS-2024Q4-001,-319.00");
    }
    await expect(page.getByText(/^(?:Loading|Checking)\b/iu)).toHaveCount(0);
    const definition = fixture.screenshots[index];
    if (!definition) throw new Error("APP_STORE_SCREENSHOT_FIXTURE_INVALID");
    expect(normalizeAccessibilitySnapshot(await page.locator("body").ariaSnapshot())).toEqual(
      definition.completeAccessibilityText,
    );
  }
  expect(electronHarness.consoleErrors).toEqual([]);
  expect(electronHarness.pageErrors).toEqual([]);
});
