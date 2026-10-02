import { mkdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { expect, test } from "./fixtures";
import {
  authenticatedWorkspace,
  CURRENT_WORKFLOW_ADMIN_PASSWORD,
  CURRENT_WORKFLOW_PASSPHRASE,
  CURRENT_WORKFLOW_USERNAME,
  setupAndRunCurrentAccountingWorkflow,
} from "./support/current-accounting-workflow";
import {
  expectInvoiceJournal,
  readBusinessIncome,
  readInvoiceAccounting,
  recordInvoiceReceipt,
  regenerateInvoiceBas,
  showBusinessIncome,
} from "./support/invoice-income-workflow";

test("invoices/income-payments-bas: cash receipts, mixed GST and balances persist through restart", async ({
  electronHarness,
}, testInfo) => {
  const screenshotDirectory = fileURLToPath(
    new URL("../../../../.tmp/invoice-income-bas-e2e/", import.meta.url),
  );
  await mkdir(screenshotDirectory, { recursive: true });
  const workspace = await setupAndRunCurrentAccountingWorkflow(
    electronHarness.page,
    electronHarness,
    { gstBasis: "cash" },
  );
  let page = electronHarness.currentPage();
  await page.getByRole("link", { name: "Invoices", exact: true }).click();
  await page.getByLabel("Invoice number", { exact: true }).fill("CASH-2026-001");
  await page.getByLabel("Issue date", { exact: true }).fill("2026-09-30");
  await page.getByLabel("Due date", { exact: true }).fill("2026-10-14");
  await page.getByLabel("Customer name", { exact: true }).fill("Cash receipt customer");
  await page
    .getByRole("textbox", { name: "Customer address", exact: true })
    .fill("1 Receipt Road, Melbourne VIC 3000");
  await page
    .getByRole("textbox", { name: "Seller address", exact: true })
    .fill("27 Cambridge Road, Mooroolbark VIC 3138");
  await page.getByLabel("Registered for GST", { exact: true }).check();
  const lines = [
    { description: "Taxable service", price: "100.00", cost: "80.00", tax: "taxable" },
    { description: "GST-free service", price: "50.00", cost: "10.00", tax: "gst-free" },
    { description: "Out-of-scope service", price: "20.00", cost: "5.00", tax: "out-of-scope" },
  ];
  for (const [index, line] of lines.entries()) {
    if (index > 0) await page.getByRole("button", { name: "Add line item", exact: true }).click();
    await page.getByLabel(`Description ${index + 1}`, { exact: true }).fill(line.description);
    await page.getByLabel(`Unit price ${index + 1}`, { exact: true }).fill(line.price);
    await page.getByLabel(`Unit cost ${index + 1}`, { exact: true }).fill(line.cost);
    await page.getByLabel(`GST treatment ${index + 1}`, { exact: true }).selectOption(line.tax);
  }
  await page.getByRole("button", { name: "Save draft", exact: true }).click();
  await expect(page.getByText("Draft saved.", { exact: true })).toBeVisible();
  const draftIncome = await readBusinessIncome(
    page,
    workspace,
    "2026-07-01",
    "2026-09-30",
    "2026-09-30",
  );
  expect(draftIncome.businessIncome?.minorUnits).toBe(0n);
  expect(draftIncome.invoiceIncome?.minorUnits).toBe(0n);
  expect(draftIncome.outstanding?.minorUnits).toBe(0n);
  await page.getByRole("button", { name: "Issue invoice", exact: true }).click();
  await page.getByRole("button", { name: "Confirm issue", exact: true }).click();
  await expect(page.getByText("Invoice issued.", { exact: true })).toBeVisible();
  await expect(
    page.getByText("Cash basis · GST follows recorded payments", { exact: true }),
  ).toBeVisible();
  await expectInvoiceJournal(page, workspace, "CASH-2026-001", 17000n, 18000n, 1000n);
  await showBusinessIncome(page, "2026-07-01", "2026-09-30", "2026-09-30");
  await expect(page.getByTestId("business-income-total")).toContainText("$170.00");
  await expect(page.getByTestId("business-outstanding")).toContainText("$180.00");
  await regenerateInvoiceBas(page, "2026-07-01", "2026-09-30", "$0.00", "$0.00");
  await recordInvoiceReceipt(page, "CASH-2026-001", "2026-10-02", "90.00", "First half received");
  await expect(page.getByTestId("invoice-outstanding")).toContainText("$90.00");
  const received = await readInvoiceAccounting(page, workspace, "CASH-2026-001");
  expect(received.accounting.version).toBe(2n);
  expect(received.accounting.receivedMinor).toBe(9000n);
  expect(received.accounting.payments[0]?.allocatedGst?.minorUnits).toBe(500n);
  expect(received.accounting.payments[0]?.basSalesGross?.minorUnits).toBe(8000n);
  await regenerateInvoiceBas(page, "2026-07-01", "2026-09-30", "$0.00", "$0.00");
  await regenerateInvoiceBas(page, "2026-10-01", "2026-12-31", "$80.00", "$5.00");
  await expect(page.getByRole("table", { name: "BAS sales sources", exact: true })).toContainText(
    "Recorded receipt",
  );
  const firstReceiptScreenshot = path.join(screenshotDirectory, "cash-basis-first-receipt-bas.png");
  await page.screenshot({ animations: "disabled", path: firstReceiptScreenshot });
  await testInfo.attach("cash-basis-first-receipt-bas", {
    path: firstReceiptScreenshot,
    contentType: "image/png",
  });
  await recordInvoiceReceipt(page, "CASH-2026-001", "2026-10-02", "90.00", "Final half received");
  await expect(page.getByTestId("invoice-outstanding")).toContainText("$0.00");
  await page.getByRole("link", { name: "GST & BAS", exact: true }).click();
  await expect(page.getByText(/Sources changed since this workpaper was saved/)).toBeVisible();
  await expect(page.getByTestId("bas-sales-g1")).toContainText("$80.00");
  await regenerateInvoiceBas(page, "2026-10-01", "2026-12-31", "$160.00", "$10.00");
  await showBusinessIncome(page, "2026-10-01", "2026-12-31", "2026-10-02");
  await expect(page.getByTestId("business-income-total")).toContainText("$0.00");
  await expect(page.getByTestId("business-payments-received")).toContainText("$180.00");
  await expect(page.getByTestId("business-outstanding")).toContainText("$0.00");
  await page.getByRole("link", { name: "Trial balance", exact: true }).click();
  await expect(page.getByRole("row", { name: /Invoice sales/ })).toContainText("$170.00");
  await expect(page.getByRole("row", { name: /Business bank/ })).toContainText("$180.00");
  page = await electronHarness.restart("accounting-workflow-restart");
  await page.getByLabel("Workspace passphrase").fill(CURRENT_WORKFLOW_PASSPHRASE);
  await page.getByLabel("Email or username").fill(CURRENT_WORKFLOW_USERNAME);
  await page.getByLabel("Administrator password").fill(CURRENT_WORKFLOW_ADMIN_PASSWORD);
  await page.getByRole("button", { name: "Unlock workspace", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Invoices", exact: true }).click();
  await page.getByRole("button", { name: "Open CASH-2026-001", exact: true }).click();
  await expect(page.getByTestId("invoice-received")).toContainText("$180.00");
  await expect(page.getByTestId("invoice-outstanding")).toContainText("$0.00");
  await expect(
    page.getByRole("table", { name: "Recorded invoice payments", exact: true }),
  ).toContainText("Final half received");
  const retained = await readInvoiceAccounting(
    page,
    await authenticatedWorkspace(page),
    "CASH-2026-001",
  );
  expect(retained.accounting.paymentCount).toBe(2n);
  expect(
    retained.accounting.payments.reduce(
      (sum, payment) => sum + (payment.allocatedGst?.minorUnits ?? 0n),
      0n,
    ),
  ).toBe(1000n);
  await page.getByRole("link", { name: "GST & BAS", exact: true }).click();
  await expect(page.getByTestId("bas-sales-g1")).toContainText("$160.00");
  await expect(page.getByTestId("bas-gst-on-sales-1a")).toContainText("$10.00");
  const finalScreenshot = path.join(screenshotDirectory, "cash-basis-final-bas-after-restart.png");
  await page.screenshot({ animations: "disabled", path: finalScreenshot });
  await testInfo.attach("cash-basis-final-bas-after-restart", {
    path: finalScreenshot,
    contentType: "image/png",
  });
  expect(electronHarness.consoleErrors).toEqual([]);
  expect(electronHarness.pageErrors).toEqual([]);
});
