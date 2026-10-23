import { mkdir, readFile, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import type { Page } from "@playwright/test";
import { expect, test } from "./fixtures";
import { setupAndRunCurrentAccountingWorkflow } from "./support/current-accounting-workflow";
import { readInvoiceAccounting } from "./support/invoice-income-workflow";

const phase = process.env.TAMMY_FINANCIAL_UI_CAPTURE;
if (phase !== undefined && phase !== "before" && phase !== "after" && phase !== "integration") {
  throw new Error("FINANCIAL_UI_CAPTURE_PHASE_INVALID");
}

test.describe("Soft Precision financial workspace", () => {
  // Setup responses contain synthetic authentication material. Never create a retry trace.
  test.describe.configure({ retries: 0 });

  test("invoices/soft-precision-workspace: edit, issue, record one receipt and export with clear recovery", async ({
    electronHarness,
  }) => {
    const workspace = await setupAndRunCurrentAccountingWorkflow(
      electronHarness.page,
      electronHarness,
      { fixedRendererClock: true },
    );
    const page = electronHarness.currentPage();
    await page.setViewportSize({ width: 1440, height: 1100 });
    await page.getByRole("link", { name: "Invoices", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Invoices", exact: true })).toBeVisible();
    await expect(page.getByText("No saved invoices yet.", { exact: true })).toBeVisible();
    await capture(page, "01-new-invoice");

    await page.getByLabel("Invoice number", { exact: true }).fill("INV-UI-001");
    await page.getByLabel("Issue date", { exact: true }).fill("2026-09-30");
    await page.getByLabel("Due date", { exact: true }).fill("2026-10-30");
    await page.getByLabel("Customer name", { exact: true }).fill("Merri Studio — synthetic");
    await page
      .getByLabel("Customer address", { exact: true })
      .fill("1 Example Lane, Melbourne VIC 3000 — synthetic");
    await page
      .getByLabel("Seller address", { exact: true })
      .fill("2 Example Road, Melbourne VIC 3000 — synthetic");
    await page.getByLabel("Registered for GST", { exact: true }).check();
    await page.getByLabel("Description 1", { exact: true }).fill("Studio strategy workshop");
    await page.getByLabel("Quantity 1", { exact: true }).fill("1");
    await page.getByLabel("Unit cost 1", { exact: true }).fill("500.00");
    await page.getByLabel("Unit price 1", { exact: true }).fill("1200.001");
    await page.getByLabel("GST treatment 1", { exact: true }).selectOption("taxable");
    await page.getByRole("button", { name: "Save draft", exact: true }).click();
    await expect(page.getByRole("alert").last()).toBeVisible();
    await expect(page.getByLabel("Unit price 1", { exact: true })).toHaveValue("1200.001");
    await capture(page, "02-validation-preserves-edit");

    await page.getByLabel("Unit price 1", { exact: true }).fill("1200.00");
    await expect(page.getByTestId("invoice-total")).toContainText("$1,320.00");
    await page.getByRole("button", { name: "Save draft", exact: true }).dblclick();
    await expect(page.getByText("Draft saved.", { exact: true })).toBeVisible();
    expect((await readInvoiceAccounting(page, workspace, "INV-UI-001")).invoice.version).toBe(1n);
    await capture(page, "03-saved-draft");

    await page.getByRole("textbox", { name: /^Notes/ }).fill("Unsaved synthetic note");
    await page.getByRole("button", { name: "New invoice", exact: true }).click();
    await expect(page.getByRole("button", { name: "Keep editing", exact: true })).toBeVisible();
    await capture(page, "03b-unsaved-replacement");
    await page.getByRole("button", { name: "Keep editing", exact: true }).click();
    await expect(page.getByRole("textbox", { name: /^Notes/ })).toHaveValue(
      "Unsaved synthetic note",
    );
    await page.getByRole("button", { name: "Open INV-UI-001", exact: true }).click();
    await page.getByRole("button", { name: "Discard changes and continue", exact: true }).click();
    await expect(page.getByRole("textbox", { name: /^Notes/ })).toHaveValue("");

    await page.getByRole("button", { name: "Manage invoice template", exact: true }).click();
    await expect(page.getByLabel("Template seller address", { exact: true })).toBeVisible();
    await capture(page, "04-template-editor");
    await page.getByRole("button", { name: "Close template editor", exact: true }).click();
    await page.getByRole("button", { name: "Issue invoice", exact: true }).click();
    await expect(page.getByRole("button", { name: "Confirm issue", exact: true })).toBeVisible();
    await capture(page, "05-issue-confirmation");
    await page.getByRole("button", { name: "Keep as draft", exact: true }).click();
    expect((await readInvoiceAccounting(page, workspace, "INV-UI-001")).invoice.version).toBe(1n);
    await page.getByRole("button", { name: "Issue invoice", exact: true }).click();
    await page.getByRole("button", { name: "Confirm issue", exact: true }).click();
    await expect(page.getByText("Invoice issued.", { exact: true })).toBeVisible();
    await expect(page.getByLabel("Customer name", { exact: true })).toBeDisabled();
    await expect(page.getByTestId("invoice-outstanding")).toContainText("$1,320.00");
    await capture(page, "06-issued-read-only");

    await page.getByRole("button", { name: "Record payment", exact: true }).click();
    await page
      .getByRole("combobox", { name: "Bank account", exact: true })
      .selectOption({ label: "1000 · Business bank" });
    await page.getByLabel("Amount received (AUD)", { exact: true }).fill("500.00");
    await page
      .getByLabel("Payment reference (optional)", { exact: true })
      .fill("SYNTHETIC-RECEIPT");
    await capture(page, "07-payment-review");
    await page.getByRole("button", { name: "Cancel payment", exact: true }).click();
    expect(
      (await readInvoiceAccounting(page, workspace, "INV-UI-001")).accounting.payments,
    ).toHaveLength(0);
    await page.getByRole("button", { name: "Record payment", exact: true }).click();
    await page.getByLabel("Amount received (AUD)", { exact: true }).fill("500.00");
    await page.getByRole("button", { name: "Save payment", exact: true }).dblclick();
    await expect(page.getByText("Payment recorded.", { exact: true })).toBeVisible();
    const paid = await readInvoiceAccounting(page, workspace, "INV-UI-001");
    expect(paid.accounting.receivedMinor).toBe(50_000n);
    expect(paid.accounting.outstandingMinor).toBe(82_000n);
    expect(paid.accounting.payments).toHaveLength(1);
    await capture(page, "08-recorded-receipt");

    await electronHarness.application.evaluate(({ dialog }) => {
      dialog.showSaveDialog = async () => ({ canceled: true, filePath: "" });
    });
    await page.getByRole("button", { name: "Export PDF", exact: true }).click();
    await expect(
      page.getByText("PDF export cancelled. No file was saved.", { exact: true }),
    ).toBeVisible();
    const userData = await electronHarness.application.evaluate(({ app }) =>
      app.getPath("userData"),
    );
    const exportedPath = path.join(userData, "synthetic-invoice-ui.pdf");
    await electronHarness.application.evaluate(({ dialog }, filePath) => {
      dialog.showSaveDialog = async () => ({ canceled: false, filePath });
    }, exportedPath);
    await page.getByRole("button", { name: "Export PDF", exact: true }).click();
    await expect(page.getByText("Invoice PDF saved.", { exact: true })).toBeVisible();
    expect((await readFile(exportedPath)).subarray(0, 5).toString()).toBe("%PDF-");
    if (process.platform !== "win32") expect((await stat(exportedPath)).mode & 0o777).toBe(0o600);

    await page.getByRole("link", { name: "Overview", exact: true }).click();
    await page.getByRole("link", { name: "Invoices", exact: true }).click();
    await page.goBack();
    await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
    await page.goForward();
    await page.getByRole("button", { name: "Open INV-UI-001", exact: true }).click();
    await expect(page.getByTestId("invoice-outstanding")).toContainText("$820.00");
    await expect(page.getByLabel("Customer name", { exact: true })).toBeDisabled();

    if (phase !== "before") {
      const search = page.getByLabel("Search saved invoices", { exact: true });
      await search.fill("no-match-synthetic");
      await expect(page.getByText("No matching invoices", { exact: true })).toBeVisible();
      await capture(page, "09-invoice-no-results");
      await page.getByRole("button", { name: "Clear invoice search", exact: true }).click();
      await expect(search).toBeFocused();
      await expect(
        page.getByRole("button", { name: "Open INV-UI-001", exact: true }),
      ).toBeVisible();
      await expect
        .poll(() => search.evaluate((input) => getComputedStyle(input).outlineStyle))
        .toBe("solid");
      await capture(page, "10-keyboard-focus");
      await expect(page.getByRole("textbox", { name: /^Notes/ })).toHaveCSS("resize", "none");
      await expect(page.getByRole("textbox", { name: /^Notes/ })).toHaveCSS(
        "field-sizing",
        "content",
      );
    }
    await page.emulateMedia({ reducedMotion: "reduce" });
    await expect(page.getByRole("button", { name: "New invoice", exact: true })).toHaveCSS(
      "transition-duration",
      "0s",
    );
    for (const width of [390, 320]) {
      await page.setViewportSize({ width, height: 900 });
      await expect
        .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth))
        .toBe(true);
      await page.getByRole("heading", { name: "Invoices", exact: true }).scrollIntoViewIfNeeded();
      await capture(page, `11-invoices-${width}`);
      await page
        .getByRole("table", { name: "Invoice line items", exact: true })
        .scrollIntoViewIfNeeded();
      await capture(page, `12-lines-${width}`);
      if (phase !== "before") {
        const scrollArea = page
          .getByRole("table", { name: "Invoice line items", exact: true })
          .locator("..");
        await scrollArea.evaluate((element) => {
          element.scrollLeft = 0;
        });
        await scrollArea.focus();
        await expect(scrollArea).toBeFocused();
        await page.keyboard.press("ArrowRight");
        await expect
          .poll(() => scrollArea.evaluate((element) => element.scrollLeft))
          .toBeGreaterThan(0);
        await scrollArea.evaluate((element) => {
          element.scrollLeft = element.scrollWidth;
        });
        await capture(page, `12b-line-amounts-${width}`);
        const dimensions = await page.evaluate(() => ({
          viewport: innerWidth,
          available: document.documentElement.clientWidth,
          document: document.documentElement.scrollWidth,
        }));
        expect(dimensions.document, JSON.stringify(dimensions)).toBeLessThanOrEqual(
          dimensions.available,
        );
      }
    }
    expect(electronHarness.consoleErrors).toEqual([]);
    expect(electronHarness.pageErrors).toEqual([]);
  });
});

async function capture(page: Page, name: string) {
  if (!phase) return;
  await expect(page.getByRole("heading", { name: "Invoices", exact: true })).toBeAttached();
  if (/^0[1239]|^10/.test(name)) {
    await page.getByRole("heading", { name: "Invoices", exact: true }).scrollIntoViewIfNeeded();
  } else if (name.startsWith("04")) {
    await page.getByLabel("Template seller address", { exact: true }).scrollIntoViewIfNeeded();
  } else if (name.startsWith("05")) {
    await page.getByRole("button", { name: "Confirm issue", exact: true }).scrollIntoViewIfNeeded();
  } else if (/^0[678]/.test(name)) {
    await page
      .getByRole("heading", { name: "Income & payments", exact: true })
      .scrollIntoViewIfNeeded();
  }
  const directory = path.resolve(
    path.dirname(fileURLToPath(import.meta.url)),
    phase === "integration"
      ? "../../../../output/playwright/soft-precision-integration-2026-10-23"
      : "../../../../output/playwright/soft-precision-financial-2026-10-23",
    phase === "integration" ? "after" : phase,
  );
  await mkdir(directory, { recursive: true });
  const bytes = await page.screenshot({
    animations: "disabled",
    mask: [page.locator("input[type=password]")],
  });
  await writeFile(path.join(directory, `${name}.png`), bytes, { flag: "wx" });
}
