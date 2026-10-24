import { mkdir, readFile, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import type { Page } from "@playwright/test";
import { expect, test } from "./fixtures";
import { auditCapturePhase, runAuditJourney } from "./support/audit-journey";
import { bankingCapturePhase, runBankingReviewJourney } from "./support/banking-journey";
import { chartCapturePhase, runChartJourney } from "./support/chart-journey";
import { setupAndRunCurrentAccountingWorkflow } from "./support/current-accounting-workflow";
import { documentsCapturePhase, runDocumentsReviewJourney } from "./support/documents-journey";
import { readInvoiceAccounting } from "./support/invoice-income-workflow";
import { journalsCapturePhase, runJournalsJourney } from "./support/journals-journey";
import {
  captureLocalOverview,
  overviewCapturePhase,
  runOverviewReviewJourney,
} from "./support/overview-journey";
import { runTrialBalanceJourney, trialBalanceCapturePhase } from "./support/trial-balance-journey";

const phase = process.env.TAMMY_FINANCIAL_UI_CAPTURE;
const referencePhase = process.env.TAMMY_INVOICE_REFERENCE_CAPTURE;
const detailPhase = process.env.TAMMY_INVOICE_DETAIL_CAPTURE;
const draftPhase = process.env.TAMMY_INVOICE_DRAFT_CAPTURE;
if (draftPhase !== undefined && draftPhase !== "before" && draftPhase !== "after") {
  throw new Error("INVOICE_DRAFT_CAPTURE_PHASE_INVALID");
}
if (detailPhase !== undefined && detailPhase !== "before" && detailPhase !== "after") {
  throw new Error("INVOICE_DETAIL_CAPTURE_PHASE_INVALID");
}
if (referencePhase !== undefined && referencePhase !== "before" && referencePhase !== "after") {
  throw new Error("INVOICE_REFERENCE_CAPTURE_PHASE_INVALID");
}
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
    if (auditCapturePhase) {
      await runAuditJourney(page);
      expect(electronHarness.consoleErrors).toEqual([]);
      expect(electronHarness.pageErrors).toEqual([]);
      return;
    }
    if (trialBalanceCapturePhase) {
      await runTrialBalanceJourney(page, workspace);
      expect(electronHarness.consoleErrors).toEqual([]);
      expect(electronHarness.pageErrors).toEqual([]);
      return;
    }
    if (chartCapturePhase) {
      await runChartJourney(page, workspace);
      expect(electronHarness.consoleErrors).toEqual([]);
      expect(electronHarness.pageErrors).toEqual([]);
      return;
    }
    if (journalsCapturePhase) {
      await runJournalsJourney(page, workspace);
      expect(electronHarness.consoleErrors).toEqual([]);
      expect(electronHarness.pageErrors).toEqual([]);
      return;
    }
    if (bankingCapturePhase) {
      await runBankingReviewJourney(page, workspace);
      expect(electronHarness.consoleErrors).toEqual([]);
      expect(electronHarness.pageErrors).toEqual([]);
      return;
    }
    if (documentsCapturePhase) {
      await runDocumentsReviewJourney(page, workspace);
      expect(electronHarness.consoleErrors).toEqual([]);
      expect(electronHarness.pageErrors).toEqual([]);
      return;
    }
    if (overviewCapturePhase) await captureLocalOverview(page);
    await page.getByRole("link", { name: "Invoices", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Invoices", exact: true })).toBeVisible();
    await expect(page.getByText("No saved invoices yet.", { exact: true })).toBeVisible();
    if (referencePhase) {
      await captureReferenceList(page);
      expect(electronHarness.consoleErrors).toEqual([]);
      expect(electronHarness.pageErrors).toEqual([]);
      return;
    }
    await capture(page, "01-new-invoice");
    await page.getByRole("button", { name: "New invoice", exact: true }).click();
    await capture(page, "01-draft-editor-empty");

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
    if (draftPhase !== "before") {
      await expect(page.getByTestId("invoice-total")).toContainText("—");
      await expect(
        page.getByRole("region", { name: "Draft invoice preview", exact: true }),
      ).toContainText("Check the invoice details");
    }
    await capture(page, "02-validation-preserves-edit");

    await page.getByLabel("Unit price 1", { exact: true }).fill("1200.00");
    await expect(page.getByTestId("invoice-total")).toContainText("$1,320.00");
    await page.getByRole("button", { name: "Save draft", exact: true }).dblclick();
    await expect(page.getByText("Draft saved.", { exact: true })).toBeVisible();
    expect((await readInvoiceAccounting(page, workspace, "INV-UI-001")).invoice.version).toBe(1n);
    if (draftPhase !== "before") {
      const preview = page.getByRole("region", { name: "Draft invoice preview", exact: true });
      await expect(preview).toContainText("Merri Studio — synthetic");
      await expect(preview).toContainText("$1,320.00");
      await expect(preview).not.toContainText("$500.00");
      const fields = await page
        .getByRole("group", { name: "Invoice details", exact: true })
        .boundingBox();
      const paper = await preview.boundingBox();
      expect(fields).not.toBeNull();
      expect(paper).not.toBeNull();
      if (!fields || !paper) throw new Error("DRAFT_COMPOSITION_BOUNDS_MISSING");
      expect(paper.x).toBeGreaterThan(fields.x + fields.width);
      await expect
        .poll(() =>
          page
            .getByRole("region", { name: "Invoice line items table", exact: true })
            .evaluate((element) => element.scrollWidth <= element.clientWidth),
        )
        .toBe(true);
    }
    await capture(page, "03-saved-draft");
    if (draftPhase || (!phase && !detailPhase)) {
      for (const width of [390, 320]) {
        await page.setViewportSize({ width, height: 900 });
        await expect
          .poll(() =>
            page.evaluate(
              () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
            ),
          )
          .toBe(true);
        await capture(page, `03c-draft-${width}`);
        const lines = page.getByRole("region", { name: "Invoice line items table", exact: true });
        await lines.evaluate((element) => {
          element.scrollLeft = 0;
        });
        await lines.focus();
        await page.keyboard.press("ArrowRight");
        await expect.poll(() => lines.evaluate((element) => element.scrollLeft)).toBeGreaterThan(0);
        if (draftPhase !== "before") {
          const url = page.url();
          const view = page.getByRole("button", { name: "View preview", exact: true });
          await view.focus();
          await page.keyboard.press("Space");
          const heading = page.getByRole("heading", { name: "Draft preview", exact: true });
          await expect(heading).toBeFocused();
          await expect(heading).toHaveCSS("outline-style", "solid");
          expect(page.url()).toBe(url);
          await capture(page, `04-preview-${width}`);
          const previewLines = page.getByRole("region", {
            name: "Draft preview line items",
            exact: true,
          });
          await previewLines.evaluate((element) => {
            element.scrollLeft = 0;
          });
          await previewLines.focus();
          await page.keyboard.press("ArrowRight");
          await expect
            .poll(() => previewLines.evaluate((element) => element.scrollLeft))
            .toBeGreaterThan(0);
          await page.getByRole("button", { name: "Back to invoice fields", exact: true }).click();
          await expect(
            page.locator("legend").filter({ hasText: /^Invoice details$/ }),
          ).toBeFocused();
          if (width === 320) {
            const customer = page.getByLabel("Customer name", { exact: true });
            const price = page.getByLabel("Unit price 1", { exact: true });
            const originalCustomer = await customer.inputValue();
            const originalPrice = await price.inputValue();
            await customer.fill(`Synthetic-${"x".repeat(120)}`);
            await price.fill("1000000.00");
            await view.focus();
            await page.keyboard.press("Space");
            await expect(page.getByTestId("invoice-total")).toContainText("$1,100,000.00");
            await expect
              .poll(() =>
                page.evaluate(
                  () =>
                    document.documentElement.scrollWidth <= document.documentElement.clientWidth,
                ),
              )
              .toBe(true);
            await capture(page, "04b-preview-large-320");
            await customer.fill(originalCustomer);
            await price.fill(originalPrice);
            await expect(page.getByTestId("invoice-total")).toContainText("$1,320.00");
            expect(
              (await readInvoiceAccounting(page, workspace, "INV-UI-001")).invoice.version,
            ).toBe(1n);
          }
        }
      }
      await page.setViewportSize({ width: 1440, height: 1100 });
    }

    await page.getByRole("textbox", { name: /^Notes/ }).fill("Unsaved synthetic note");
    await page.getByRole("button", { name: "New invoice", exact: true }).click();
    await expect(page.getByRole("button", { name: "Keep editing", exact: true })).toBeVisible();
    await capture(page, "03b-unsaved-replacement");
    await page.getByRole("button", { name: "Keep editing", exact: true }).click();
    await expect(page.getByRole("textbox", { name: /^Notes/ })).toHaveValue(
      "Unsaved synthetic note",
    );
    await page.getByRole("button", { name: "Back to invoices", exact: true }).click();
    await page.getByRole("button", { name: "Discard changes and continue", exact: true }).click();
    await page.getByRole("button", { name: "Open INV-UI-001", exact: true }).click();
    await expect(page.getByRole("textbox", { name: /^Notes/ })).toHaveValue("");

    await page.getByRole("button", { name: "Manage invoice template", exact: true }).click();
    await expect(page.getByLabel("Template seller address", { exact: true })).toBeVisible();
    await capture(page, "04-template-editor");
    await page.getByRole("button", { name: "Close template editor", exact: true }).click();
    if (draftPhase === "before")
      await page.getByRole("button", { name: "Issue invoice", exact: true }).click();
    else {
      await page.getByRole("button", { name: "Issue invoice", exact: true }).focus();
      await page.keyboard.press("Space");
      await expect(page.getByRole("button", { name: "Keep as draft", exact: true })).toBeFocused();
      await expect(page.getByRole("button", { name: "Keep as draft", exact: true })).toHaveCSS(
        "outline-style",
        "solid",
      );
    }
    await expect(page.getByRole("button", { name: "Confirm issue", exact: true })).toBeVisible();
    await capture(page, "05-issue-confirmation");
    if (draftPhase !== "before") await capture(page, "05b-issue-confirmation-focused");
    await page.getByRole("button", { name: "Keep as draft", exact: true }).click();
    if (draftPhase !== "before")
      await expect(page.getByRole("button", { name: "Issue invoice", exact: true })).toBeFocused();
    expect((await readInvoiceAccounting(page, workspace, "INV-UI-001")).invoice.version).toBe(1n);
    await page.getByRole("button", { name: "Issue invoice", exact: true }).click();
    await page.getByRole("button", { name: "Confirm issue", exact: true }).click();
    await expect(page.getByText("Invoice issued.", { exact: true })).toBeVisible();
    await expect(
      page.getByRole("region", { name: "Issued invoice items", exact: true }),
    ).toBeVisible();
    await expect(page.getByLabel("Customer name", { exact: true })).toHaveCount(0);
    await expect(page.getByTestId("invoice-outstanding")).toContainText("$1,320.00");
    await capture(page, "06-issued-read-only");

    const paymentView = page.getByRole("button", { name: "Payments", exact: true });
    await paymentView.focus();
    await page.keyboard.press("Space");
    await expect(paymentView).toHaveAttribute("aria-pressed", "true");
    await expect(paymentView).toHaveCSS("border-bottom-color", "rgb(35, 92, 75)");
    await expect(paymentView).toHaveCSS("outline-style", "solid");
    await page.getByRole("button", { name: "Record payment", exact: true }).click();
    await expect(page.getByLabel("Payment date", { exact: true })).toBeFocused();
    await expect(page.getByRole("form", { name: "Review payment", exact: true })).toHaveCSS(
      "background-color",
      "rgb(247, 243, 234)",
    );
    await page
      .getByRole("combobox", { name: "Bank account", exact: true })
      .selectOption({ label: "1000 · Business bank" });
    await page.getByLabel("Amount received (AUD)", { exact: true }).fill("500.00");
    await page
      .getByLabel("Payment reference (optional)", { exact: true })
      .fill("SYNTHETIC-RECEIPT");
    await page.getByRole("button", { name: "Items", exact: true }).click();
    await expect(page.getByRole("button", { name: "Save payment", exact: true })).toBeHidden();
    await paymentView.click();
    await expect(page.getByLabel("Amount received (AUD)", { exact: true })).toHaveValue("500.00");
    await expect(page.getByLabel("Payment reference (optional)", { exact: true })).toHaveValue(
      "SYNTHETIC-RECEIPT",
    );
    await capture(page, "07-payment-review");
    await page.getByRole("button", { name: "Cancel payment", exact: true }).click();
    await expect(page.getByRole("button", { name: "Record payment", exact: true })).toBeFocused();
    expect(
      (await readInvoiceAccounting(page, workspace, "INV-UI-001")).accounting.payments,
    ).toHaveLength(0);
    await page.getByRole("button", { name: "Record payment", exact: true }).click();
    await page.getByLabel("Amount received (AUD)", { exact: true }).fill("500.00");
    if (overviewCapturePhase || (!phase && !detailPhase && !draftPhase && !referencePhase))
      await page.getByLabel("Payment date", { exact: true }).fill("2026-09-30");
    await page.getByRole("button", { name: "Save payment", exact: true }).dblclick();
    await expect(page.getByText("Payment recorded.", { exact: true })).toBeVisible();
    const paid = await readInvoiceAccounting(page, workspace, "INV-UI-001");
    expect(paid.accounting.receivedMinor).toBe(50_000n);
    expect(paid.accounting.outstandingMinor).toBe(82_000n);
    expect(paid.accounting.payments).toHaveLength(1);
    await capture(page, "08-recorded-receipt");

    if (overviewCapturePhase || (!phase && !detailPhase && !draftPhase && !referencePhase)) {
      await runOverviewReviewJourney(page, electronHarness);
      const retained = await readInvoiceAccounting(page, workspace, "INV-UI-001");
      expect(retained.accounting.receivedMinor).toBe(50_000n);
      expect(retained.accounting.outstandingMinor).toBe(82_000n);
      expect(retained.accounting.payments).toHaveLength(1);
      if (overviewCapturePhase) {
        expect(electronHarness.consoleErrors).toEqual([]);
        expect(electronHarness.pageErrors).toEqual([]);
        return;
      }
    }

    if (detailPhase) {
      await page.getByRole("button", { name: "Record payment", exact: true }).click();
      await page.getByLabel("Amount received (AUD)", { exact: true }).fill("200.00");
      await page
        .getByLabel("Payment reference (optional)", { exact: true })
        .fill("SYNTHETIC-REVIEW-ONLY");
      await capture(page, "09-review-existing-receipt");
      await page.getByRole("button", { name: "Cancel payment", exact: true }).click();
      const unchanged = await readInvoiceAccounting(page, workspace, "INV-UI-001");
      expect(unchanged.accounting.payments).toHaveLength(1);
      expect(unchanged.accounting.receivedMinor).toBe(50_000n);
      expect(unchanged.accounting.outstandingMinor).toBe(82_000n);
      if (detailPhase === "after")
        await page.getByRole("button", { name: "Items", exact: true }).click();
      await capture(page, "10-issued-items-with-receipt");
    }

    await page.getByRole("button", { name: "Items", exact: true }).click();

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
    await page.getByRole("button", { name: "Back to invoices", exact: true }).click();
    await page.getByRole("button", { name: "Open INV-UI-001", exact: true }).click();
    await expect(page.getByTestId("invoice-outstanding")).toContainText("$820.00");
    await page.getByRole("button", { name: "Items", exact: true }).click();
    await expect(
      page.getByRole("region", { name: "Issued invoice items", exact: true }),
    ).toBeVisible();
    await expect(page.getByLabel("Customer name", { exact: true })).toHaveCount(0);

    if (phase !== "before") {
      await page.getByRole("button", { name: "Back to invoices", exact: true }).click();
      const search = page.getByLabel("Search saved invoices", { exact: true });
      await search.fill("no-match-synthetic");
      await expect(page.getByText("No matching invoices", { exact: true })).toBeVisible();
      await capture(page, "09-invoice-no-results");
      await page.getByRole("button", { name: "Clear invoice search", exact: true }).click();
      await expect(search).toBeFocused();
      await expect(
        page.getByRole("button", { name: "Open INV-UI-001", exact: true }),
      ).toBeVisible();
      await expect(
        page
          .getByRole("group", { name: "Invoice status", exact: true })
          .getByRole("button", { name: /^All/ }),
      ).toHaveCSS("background-color", "rgb(35, 92, 75)");
      await expect
        .poll(() => search.evaluate((input) => getComputedStyle(input).outlineStyle))
        .toBe("solid");
      await capture(page, "10-keyboard-focus");
      await page.getByRole("button", { name: "Open INV-UI-001", exact: true }).click();
      const issuedItems = page.getByRole("region", { name: "Issued invoice items", exact: true });
      await expect(issuedItems.locator("input, textarea, select")).toHaveCount(0);
      await expect(
        issuedItems.getByRole("table", { name: "Invoice line items", exact: true }),
      ).toContainText("Studio strategy workshop");
    }
    await page.emulateMedia({ reducedMotion: "reduce" });
    await expect(page.getByRole("button", { name: "New invoice", exact: true })).toHaveCSS(
      "transition-duration",
      "0s",
    );
    for (const width of [390, 320]) {
      await page.setViewportSize({ width, height: 900 });
      await page.getByRole("button", { name: "Back to invoices", exact: true }).click();
      await expect
        .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth))
        .toBe(true);
      await page.locator("h1").scrollIntoViewIfNeeded();
      await capture(page, `11-invoices-${width}`);
      const savedScroll = page.getByRole("region", {
        name: "Scrollable saved invoices",
        exact: true,
      });
      await savedScroll.evaluate((element) => {
        element.scrollLeft = 0;
      });
      await savedScroll.focus();
      await page.keyboard.press("ArrowRight");
      await expect
        .poll(() => savedScroll.evaluate((element) => element.scrollLeft))
        .toBeGreaterThan(0);
      await page.getByRole("button", { name: "Open INV-UI-001", exact: true }).click();
      await page
        .getByRole("region", { name: "Invoice line items table", exact: true })
        .evaluate((element) => {
          element.scrollLeft = 0;
        });
      await capture(page, `11-detail-${width}`);
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
      if (width === 320) {
        await paymentView.click();
        await page.getByRole("button", { name: "Record payment", exact: true }).click();
        await page.getByLabel("Amount received (AUD)", { exact: true }).fill("200.00");
        await page
          .getByLabel("Payment reference (optional)", { exact: true })
          .fill("SYNTHETIC-NARROW-REVIEW");
        await expect
          .poll(() =>
            page.evaluate(
              () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
            ),
          )
          .toBe(true);
        await capture(page, "13-payment-320");
        await page.getByRole("button", { name: "Cancel payment", exact: true }).click();
        await expect(
          page.getByRole("button", { name: "Record payment", exact: true }),
        ).toBeFocused();
        const retained = await readInvoiceAccounting(page, workspace, "INV-UI-001");
        expect(retained.accounting.payments).toHaveLength(1);
        expect(retained.accounting.receivedMinor).toBe(50_000n);
        expect(retained.accounting.outstandingMinor).toBe(82_000n);
      }
    }
    if (!phase && !referencePhase && !detailPhase && !draftPhase && !overviewCapturePhase) {
      await runDocumentsReviewJourney(page, workspace);
      await runBankingReviewJourney(page, workspace);
      await runJournalsJourney(page, workspace);
      await runChartJourney(page, workspace);
      await runTrialBalanceJourney(page, workspace);
      await runAuditJourney(page);
    }
    expect(electronHarness.consoleErrors).toEqual([]);
    expect(electronHarness.pageErrors).toEqual([]);
  });
});

async function captureReferenceList(page: Page) {
  const customers = [
    "Grove Design",
    "Wattle Studio",
    "Bower Co",
    "Elm & Oak",
    "Kin Studio",
    "Maple House",
    "Larkin & Co",
    "Field Notes",
  ];
  const prices = [400, 1200, 600, 900, 500, 2000, 700, 350];
  for (const [index, customer] of customers.entries()) {
    if (index > 0 || !(await page.getByLabel("Invoice number", { exact: true }).isVisible())) {
      await page.getByRole("button", { name: "New invoice", exact: true }).click();
    }
    await page.getByLabel("Invoice number", { exact: true }).fill(`INV-SYN-${1043 - index}`);
    await page.getByLabel("Issue date", { exact: true }).fill("2026-09-30");
    await page
      .getByLabel("Due date", { exact: true })
      .fill(`2026-10-${String(21 - index).padStart(2, "0")}`);
    await page.getByLabel("Customer name", { exact: true }).fill(customer);
    await page
      .getByLabel("Customer address", { exact: true })
      .fill("1 Example Lane — synthetic fixture");
    await page
      .getByLabel("Seller address", { exact: true })
      .fill("2 Example Road — synthetic fixture");
    await page.getByLabel("Registered for GST", { exact: true }).check();
    await page.getByLabel("Description 1", { exact: true }).fill("Synthetic design services");
    await page.getByLabel("Quantity 1", { exact: true }).fill("1");
    await page.getByLabel("Unit price 1", { exact: true }).fill(String(prices[index]));
    await page.getByLabel("GST treatment 1", { exact: true }).selectOption("taxable");
    await page.getByRole("button", { name: "Save draft", exact: true }).click();
    await expect(page.getByText("Draft saved.", { exact: true })).toBeVisible();
    if (index % 2 === 0) {
      await page.getByRole("button", { name: "Issue invoice", exact: true }).click();
      await page.getByRole("button", { name: "Confirm issue", exact: true }).click();
      await expect(page.getByText("Invoice issued.", { exact: true })).toBeVisible();
    }
  }
  const back = page.getByRole("button", { name: "Back to invoices", exact: true });
  if (await back.isVisible()) await back.click();
  await page.getByRole("heading", { name: "Invoices", exact: true }).scrollIntoViewIfNeeded();
  await expect(
    page.getByRole("table", { name: "Saved invoices", exact: true }).getByRole("row"),
  ).toHaveCount(9);
  const directory = path.resolve(
    import.meta.dirname,
    "../../../../output/playwright/soft-precision-fidelity-2026-10-23",
    referencePhase as string,
  );
  await mkdir(directory, { recursive: true });
  if (referencePhase === "after") {
    await expect(
      page
        .getByRole("group", { name: "Invoice status", exact: true })
        .getByRole("button", { name: /^All/ }),
    ).toHaveCSS("background-color", "rgb(35, 92, 75)");
    const measurements = await page
      .getByRole("table", { name: "Saved invoices", exact: true })
      .locator("tbody tr")
      .evaluateAll((elements) =>
        elements.map((element) => {
          const bounds = element.getBoundingClientRect();
          return { height: bounds.height, bottom: bounds.bottom };
        }),
      );
    expect(measurements).toHaveLength(8);
    for (const row of measurements) {
      expect(row.height).toBeGreaterThanOrEqual(44);
      expect(row.height).toBeLessThanOrEqual(60);
      expect(row.bottom).toBeLessThanOrEqual(1100);
    }
    await writeFile(
      path.join(directory, "measurements-1440.json"),
      JSON.stringify({ viewport: { width: 1440, height: 1100 }, rows: measurements }, null, 2) +
        "\n",
      { flag: "wx" },
    );
  }
  await writeFile(
    path.join(directory, "01-invoice-list-1440.png"),
    await page.screenshot({ animations: "disabled" }),
    { flag: "wx" },
  );
  if (referencePhase === "after") {
    await expect(page.getByRole("button", { name: "Save draft", exact: true })).toHaveCount(0);
    const search = page.getByLabel("Search saved invoices", { exact: true });
    await search.fill("Wattle");
    await page.getByRole("button", { name: "Open INV-SYN-1042", exact: true }).click();
    await page.getByLabel("Customer name", { exact: true }).fill("Unsaved synthetic customer");
    await page.getByRole("button", { name: "Back to invoices", exact: true }).click();
    await page.getByRole("button", { name: "Keep editing", exact: true }).click();
    await expect(page.getByLabel("Customer name", { exact: true })).toHaveValue(
      "Unsaved synthetic customer",
    );
    await page.getByRole("button", { name: "Back to invoices", exact: true }).click();
    await page.getByRole("button", { name: "Discard changes and continue", exact: true }).click();
    await expect(search).toHaveValue("Wattle");
    await expect(
      page.getByRole("button", { name: "Open INV-SYN-1042", exact: true }),
    ).toBeFocused();
    await writeFile(
      path.join(directory, "02-filtered-return-1440.png"),
      await page.screenshot({ animations: "disabled" }),
      { flag: "wx" },
    );
    await page.getByRole("button", { name: "Clear invoice search", exact: true }).click();
    await expect(search).toBeFocused();
    await page
      .getByRole("group", { name: "Invoice status", exact: true })
      .getByRole("button", { name: /^Issued/ })
      .click();
    await expect(
      page.getByRole("table", { name: "Saved invoices", exact: true }).getByRole("row"),
    ).toHaveCount(5);
    await page
      .getByRole("group", { name: "Invoice status", exact: true })
      .getByRole("button", { name: /^All/ })
      .click();
    await page.emulateMedia({ reducedMotion: "reduce" });
    for (const width of [390, 320]) {
      await page.setViewportSize({ width, height: 900 });
      const region = page.getByRole("region", { name: "Scrollable saved invoices", exact: true });
      await expect
        .poll(() => region.evaluate((node) => node.clientWidth < node.scrollWidth))
        .toBe(true);
      await region.focus();
      await page.keyboard.press("ArrowRight");
      await expect.poll(() => region.evaluate((node) => node.scrollLeft)).toBeGreaterThan(0);
      await expect
        .poll(() =>
          page.evaluate(
            () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
          ),
        )
        .toBe(true);
      await writeFile(
        path.join(directory, `03-invoice-list-${width}.png`),
        await page.screenshot({ animations: "disabled" }),
        { flag: "wx" },
      );
    }
  }
}

async function capture(page: Page, name: string) {
  if (!phase && !detailPhase && !draftPhase) return;
  if (
    draftPhase &&
    ![
      "01-draft-editor-empty",
      "02-validation-preserves-edit",
      "03-saved-draft",
      "03b-unsaved-replacement",
      "03c-draft-390",
      "03c-draft-320",
      "04-preview-390",
      "04-preview-320",
      "04b-preview-large-320",
      "05-issue-confirmation",
      "05b-issue-confirmation-focused",
    ].includes(name)
  )
    return;
  if (
    detailPhase &&
    ![
      "06-issued-read-only",
      "07-payment-review",
      "08-recorded-receipt",
      "09-review-existing-receipt",
      "10-issued-items-with-receipt",
      "11-detail-390",
      "11-detail-320",
      "13-payment-320",
    ].includes(name)
  )
    return;
  await expect(page.locator("h1")).toBeAttached();
  if (draftPhase && name === "04b-preview-large-320") {
    await page.getByTestId("invoice-total").scrollIntoViewIfNeeded();
  } else if (draftPhase && name === "05b-issue-confirmation-focused") {
    await page.getByRole("button", { name: "Keep as draft", exact: true }).scrollIntoViewIfNeeded();
  } else if (draftPhase && name.startsWith("04-preview-")) {
    await page
      .getByRole("heading", { name: "Draft preview", exact: true })
      .scrollIntoViewIfNeeded();
  } else if (detailPhase && name === "13-payment-320") {
    await page.getByRole("form", { name: "Review payment", exact: true }).scrollIntoViewIfNeeded();
  } else if (detailPhase || draftPhase) {
    await page.locator("h1").scrollIntoViewIfNeeded();
  } else if (/^0[1239]|^10/.test(name)) {
    await page.locator("h1").scrollIntoViewIfNeeded();
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
    draftPhase
      ? "../../../../output/playwright/soft-precision-invoice-draft-2026-10-23"
      : detailPhase
        ? "../../../../output/playwright/soft-precision-invoice-detail-2026-10-23"
        : phase === "integration"
          ? "../../../../output/playwright/soft-precision-integration-2026-10-23"
          : "../../../../output/playwright/soft-precision-financial-2026-10-23",
    draftPhase ?? detailPhase ?? (phase === "integration" ? "after" : (phase as string)),
  );
  await mkdir(directory, { recursive: true });
  const bytes = await page.screenshot({
    animations: "disabled",
    mask: [page.locator("input[type=password]")],
  });
  await writeFile(path.join(directory, `${name}.png`), bytes, { flag: "wx" });
}
