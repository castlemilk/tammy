import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { expect, type Page } from "@playwright/test";
import type { ElectronHarness } from "../fixtures";

export const overviewCapturePhase = process.env.TAMMY_OVERVIEW_CAPTURE;
if (
  overviewCapturePhase !== undefined &&
  overviewCapturePhase !== "before" &&
  overviewCapturePhase !== "after"
) {
  throw new Error("OVERVIEW_CAPTURE_PHASE_INVALID");
}

export async function captureLocalOverview(page: Page) {
  await page.getByRole("link", { name: "Overview", exact: true }).click();
  await page.setViewportSize({ width: 1440, height: 1100 });
  await expect(page.getByTestId("business-income-total")).toBeVisible();
  await capture(page, "01-local-summary");
}

/** Existing local projections and native cancelled exports only; no mock RPC responses. */
export async function runOverviewReviewJourney(page: Page, harness: ElectronHarness) {
  await page.getByRole("link", { name: "Overview", exact: true }).click();
  await page.setViewportSize({ width: 1440, height: 1100 });
  // The existing workspace renderer clock is May 2024; this invoice fixture is Sep 2026.
  // Select its actual reporting window rather than changing application calculations.
  await selectFixturePeriod(page);
  await expect(page.getByTestId("business-invoice-income")).toContainText("$1,200.00");
  await expect(page.getByTestId("business-payments-received")).toContainText("$500.00");
  await expect(page.getByTestId("business-outstanding")).toContainText("$820.00");
  await capture(page, "02-income-and-attention");

  const start = page.getByLabel("Income period start", { exact: true });
  const end = page.getByLabel("Income period end", { exact: true });
  const originalStart = await start.inputValue();
  const originalEnd = await end.inputValue();
  await start.fill("2026-12-31");
  await end.fill("2026-07-01");
  await expect(page.getByRole("alert")).toContainText("Choose valid dates");
  await expect(page.getByTestId("business-income-total")).toHaveCount(0);
  await expect(
    page.getByRole("group", { name: "Income export", exact: true }).getByRole("button", {
      name: "Export CSV",
      exact: true,
    }),
  ).toBeDisabled();
  await capture(page, "03-invalid-period");
  await start.fill(originalStart);
  await end.fill(originalEnd);
  await expect(page.getByTestId("business-payments-received")).toContainText("$500.00");

  const refresh = page.getByRole("button", { name: "Refresh income", exact: true });
  await refresh.focus();
  await page.keyboard.press("Space");
  await expect(refresh).toBeEnabled();
  await expect(page.getByTestId("business-outstanding")).toContainText("$820.00");
  await harness.application.evaluate(({ dialog }) => {
    dialog.showSaveDialog = async () => ({ canceled: true, filePath: "" });
  });
  const csv = page
    .getByRole("group", { name: "Income export", exact: true })
    .getByRole("button", { name: "Export CSV", exact: true });
  await csv.focus();
  await page.keyboard.press("Space");
  await expect(page.getByText("Income export cancelled.", { exact: true })).toBeVisible();
  // The unchanged baseline loses focus while disabling this button for export.
  // Corrected source and normal CI must restore it after the native cancellation.
  if (overviewCapturePhase !== "before") await expect(csv).toBeFocused();
  await capture(page, "04-export-cancelled");

  await page.emulateMedia({ reducedMotion: "reduce" });
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 900 });
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
        ),
      )
      .toBe(true);
    await refresh.focus();
    await expect(refresh).toHaveCSS("outline-style", "solid");
    await capture(page, `05-income-focus-${width}`, false);
  }
  await page.setViewportSize({ width: 1440, height: 1100 });
  const bas = page
    .getByRole("link", { name: /^Open exact BAS preparation / })
    .or(page.getByRole("button", { name: "Open BAS preparation", exact: true }));
  await bas.focus();
  await expect(bas).toBeFocused();
  // Before uses the browser's native auto outline; the new composition uses the shared ring.
  if (overviewCapturePhase !== "before") await expect(bas).toHaveCSS("outline-style", "solid");
  await capture(page, "06-exact-bas-focus", false);
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/gst-bas(?:\?|$)/);
  await page.goBack();
  await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
  // This existing screen remounts with its default dates after route navigation.
  await selectFixturePeriod(page);
  await expect(page.getByTestId("business-outstanding")).toContainText("$820.00");
  await page.goForward();
  await expect(page).toHaveURL(/\/gst-bas(?:\?|$)/);
  await page.goBack();
  await page.goBack();
  await expect(page).toHaveURL(/\/invoices$/);
}

async function selectFixturePeriod(page: Page) {
  await page.getByLabel("Income period start", { exact: true }).fill("2026-07-01");
  await page.getByLabel("Income period end", { exact: true }).fill("2026-09-30");
  await page.getByLabel("Balances as of", { exact: true }).fill("2026-09-30");
}

async function capture(page: Page, name: string, anchor = true) {
  if (!overviewCapturePhase) return;
  const directory = path.resolve(
    import.meta.dirname,
    "../../../../../output/playwright/soft-precision-overview-2026-10-23",
    overviewCapturePhase,
  );
  await mkdir(directory, { recursive: true });
  if (anchor)
    await page.getByRole("heading", { name: "Overview", exact: true }).scrollIntoViewIfNeeded();
  await writeFile(
    path.join(directory, `${name}.png`),
    await page.screenshot({ animations: "disabled" }),
    {
      flag: "wx",
    },
  );
}
