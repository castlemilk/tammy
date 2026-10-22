import type { Page } from "@playwright/test";
import { expect, test } from "./fixtures";
import { setupAndRunCurrentAccountingWorkflow } from "./support/current-accounting-workflow";

async function expectLifecycleStatus(page: Page, message: string) {
  const lifecycle = page.getByRole("region", {
    name: "BAS preparation lifecycle",
    exact: true,
  });
  await expect(lifecycle.getByRole("status").filter({ hasText: message })).toBeVisible();
}

test("prepares and reviews an exact BAS period in the packaged app", async ({
  electronHarness,
}) => {
  test.setTimeout(240_000);
  await setupAndRunCurrentAccountingWorkflow(electronHarness.page, electronHarness);
  const page = electronHarness.currentPage();

  await page.getByRole("link", { name: "GST & BAS", exact: true }).click();
  await expect(page.getByRole("heading", { name: "BAS preparation", exact: true })).toBeVisible();
  await expect(
    page.getByText("Workspace admin · prepare and read only", { exact: true }),
  ).toBeVisible();

  const lifecycle = page.getByRole("navigation", { name: "BAS lifecycle stages" });
  for (const stage of ["Prepare", "Review", "Freeze", "Export", "External record"] as const) {
    await expect(lifecycle.getByRole("button", { name: stage, exact: true })).toBeVisible();
  }

  await page
    .getByLabel("Profile decision reason", { exact: true })
    .fill("Packaged lifecycle acceptance; unresolved obligations remain explicitly unknown.");
  await page.getByRole("button", { name: "Save immutable profile revision" }).click();
  await expectLifecycleStatus(page, "Saved BAS obligations profile revision");

  const lifecycleRegion = page.getByRole("region", {
    name: "BAS preparation lifecycle",
    exact: true,
  });
  const periodSelect = lifecycleRegion.getByRole("combobox", {
    name: "Confirmed period history",
    exact: true,
  });
  await expect
    .poll(async () =>
      (await periodSelect.locator("option").allTextContents()).some((label) =>
        label.startsWith("Suggestion ·"),
      ),
    )
    .toBe(true);
  const options = await periodSelect.locator("option").allTextContents();
  const suggestionIndex = options.findIndex((label) => label.startsWith("Suggestion ·"));
  expect(suggestionIndex).toBeGreaterThan(0);
  await periodSelect.selectOption({ index: suggestionIndex });
  await page
    .getByLabel("Period confirmation reason", { exact: true })
    .fill("Acceptance fixture uses the displayed BAS calendar suggestion.");
  await page.getByRole("button", { name: "Confirm selected period" }).click();
  await expectLifecycleStatus(page, "Confirmed BAS period");

  await page.getByRole("button", { name: "Regenerate exact calculation" }).click();
  await expectLifecycleStatus(page, "Prepared exact BAS version");
  await expect(page.getByRole("button", { name: "Save exact PAYG inputs" })).toBeVisible();
  await page
    .getByLabel("PAYG review reason", { exact: true })
    .fill(
      "No verified payroll or instalment amount entered; each unresolved label remains unknown.",
    );
  await page.getByRole("button", { name: "Save exact PAYG inputs" }).click();
  await expectLifecycleStatus(page, "Saved PAYG inputs against the displayed preparation revision");
  // PAYG input changes create a new immutable preparation with no calculation pointer.
  await page.getByRole("button", { name: "Regenerate exact calculation" }).click();
  await expectLifecycleStatus(page, "Prepared exact BAS version");

  await lifecycle.getByRole("button", { name: "Review", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Exact review snapshot", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Run validation on this exact calculation" }).click();
  await expectLifecycleStatus(page, "Created review validation");

  await lifecycle.getByRole("button", { name: "Freeze", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Freeze for manual handoff", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText(
      "Workspace admins can prepare and read BAS records, but cannot use approval actions.",
    ),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Approve and freeze exact snapshot" }),
  ).toBeDisabled();

  await lifecycle.getByRole("button", { name: "Export", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Export an exact retained BAS version", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("region", { name: "BAS preparation lifecycle", exact: true })
    .getByRole("combobox", { name: "Exact report source", exact: true })
    .selectOption({ label: "Retained calculation" });
  const exportButton = page.getByRole("button", { name: "Export PDF", exact: true });
  await expect(exportButton).toBeEnabled();
  await electronHarness.application.evaluate(({ dialog }) => {
    dialog.showSaveDialog = async () => ({ canceled: true, filePath: "" });
  });
  await exportButton.click();
  await expect(
    page
      .getByRole("region", { name: "BAS preparation lifecycle", exact: true })
      .getByRole("status")
      .filter({ hasText: "BAS export cancelled." }),
  ).toHaveText("BAS export cancelled.");

  await lifecycle.getByRole("button", { name: "External record", exact: true }).click();
  await expect(
    page.getByRole("heading", {
      name: "Record what the user says happened outside Tammy",
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Record user-reported event as unverified" }),
  ).toBeDisabled();
  await expect(
    page.getByText(/Tammy cannot confirm that the ATO received, processed or accepted anything/),
  ).toBeVisible();

  expect(electronHarness.consoleErrors).toEqual([]);
  expect(electronHarness.pageErrors).toEqual([]);
});
