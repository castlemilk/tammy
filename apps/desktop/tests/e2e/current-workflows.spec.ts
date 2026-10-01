import { readFile } from "node:fs/promises";
import path from "node:path";
import { pdfStreams } from "../../../../packages/invoices/test-support/pdf-text.ts";
import {
  normalizeAccessibilitySnapshot,
  validateScreenshotFixture,
} from "../../../../scripts/check-app-store-screenshots.mjs";
import screenshotFixture from "../../release/macos/screenshots/fixture.json" with { type: "json" };
import { expect, test } from "./fixtures";
import {
  CURRENT_WORKFLOW_ADMIN_PASSWORD,
  CURRENT_WORKFLOW_PASSPHRASE,
  CURRENT_WORKFLOW_USERNAME,
  setupAndRunCurrentAccountingWorkflow,
} from "./support/current-accounting-workflow";
import { callPairedApi, runPairedMcpSmoke } from "./support/local-api";

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
  const userData = await electronHarness.application.evaluate(({ app, dialog, Menu }) => {
    dialog.showMessageBox = async () => ({ response: 0, checkboxChecked: false });
    const menu = Menu.getApplicationMenu();
    const item = menu?.getMenuItemById("tammy-integrations-pair");
    if (!item) throw new Error("PAIRED_API_MENU_MISSING");
    item.click(item, null, {} as KeyboardEvent);
    return app.getPath("userData");
  });
  const connectionPath = path.join(userData, "local-integrations/connection.json");
  await expect
    .poll(async () =>
      readFile(connectionPath).then(
        () => true,
        () => false,
      ),
    )
    .toBe(true);
  const accounts = await callPairedApi(connectionPath, "tammy.accounting.list_accounts");
  expect(accounts).toMatchObject({
    ok: true,
    data: {
      accounts: expect.arrayContaining([
        expect.objectContaining({ name: fixture.accounts[0]?.name }),
      ]),
    },
  });
  const documents = await callPairedApi(connectionPath, "tammy.documents.list_metadata");
  expect(documents).toMatchObject({
    ok: true,
    data: {
      documents: [
        expect.objectContaining({ sourceDisplayName: fixture.sourceDocument.sourceDisplayName }),
      ],
    },
  });
  expect(JSON.stringify(documents)).not.toContain("extractedText");
  const costSources = await callPairedApi(connectionPath, "tammy.invoices.list_cost_sources");
  expect(costSources).toMatchObject({
    ok: true,
    data: { expenses: [expect.objectContaining({ subtotalMinor: 29000, currency: "AUD" })] },
  });
  expect(JSON.stringify(costSources)).not.toMatch(/extractedText|candidate/);
  const expense = costSources.data.expenses[0];
  const mcp = await runPairedMcpSmoke(connectionPath, {
    number: "INV-MCP-001",
    issueDate: "2026-09-30",
    dueDate: "2026-10-14",
    customerName: "Invoice acceptance customer",
    customerAddress: "1 Customer Road, Melbourne VIC 3000",
    sellerAddress: "27 Cambridge Road, Mooroolbark VIC 3138",
    gstRegistered: true,
    notes: "Invoice acceptance",
    paymentInstructions: "Bank transfer",
    lines: [
      {
        description: "MCP accounting services",
        quantityMilli: 1500,
        unitPriceMinor: 10005,
        unitCostMinor: 4000,
        taxTreatment: "taxable",
      },
      {
        description: expense.description,
        quantityMilli: 1000,
        unitPriceMinor: expense.subtotalMinor,
        unitCostMinor: expense.subtotalMinor,
        taxTreatment: "gst-free",
        sourceDocumentId: expense.id,
      },
    ],
  });
  expect(mcp.tools).toMatchObject({
    tools: expect.arrayContaining([
      expect.objectContaining({ name: "tammy.accounting.list_accounts" }),
    ]),
  });
  expect(mcp.result).not.toHaveProperty("isError", true);
  expect(JSON.stringify(mcp.result)).toContain(fixture.accounts[0]?.name);
  expect(JSON.stringify(mcp.resource)).toContain(fixture.business.legalName);
  expect(mcp.proposal).not.toHaveProperty("isError", true);
  expect(JSON.stringify(mcp.proposal)).toContain("reviewRequired");
  await page.getByRole("link", { name: "Invoices", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Invoices", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Use MCP draft" }).click();
  await expect(page.getByLabel("Invoice number")).toHaveValue("INV-MCP-001");
  await expect(page.getByTestId("invoice-total")).toContainText("$455.09");
  await expect(page.getByTestId("invoice-cost")).toContainText("$350.00");
  await expect(
    page.getByText("Linked to a reviewed expense · internal only", { exact: true }),
  ).toBeVisible();
  await page.getByLabel("Customer name", { exact: true }).fill("Reviewed invoice customer");
  await page.getByRole("link", { name: "Documents", exact: true }).click();
  await page.getByRole("link", { name: "Invoices", exact: true }).click();
  await expect(page.getByLabel("Customer name", { exact: true })).toHaveValue(
    "Reviewed invoice customer",
  );
  await page.getByRole("button", { name: "Save draft", exact: true }).click();
  await expect(page.getByText("Draft saved.", { exact: true })).toBeVisible();
  const draftPdfPath = path.join(userData, "invoice-acceptance-draft.pdf");
  await electronHarness.application.evaluate(({ dialog }, filePath) => {
    dialog.showSaveDialog = async () => ({ canceled: false, filePath });
  }, draftPdfPath);
  await page.getByRole("button", { name: "Export PDF" }).click();
  await expect(page.getByText("Invoice PDF saved.", { exact: true })).toBeVisible();
  const draftPdf = expectCustomerInvoicePdf(await readFile(draftPdfPath), expense.id);
  expect(draftPdf).toContain("DRAFT");
  await page.getByRole("button", { name: "Issue invoice", exact: true }).click();
  await page.getByRole("button", { name: "Confirm issue", exact: true }).click();
  await expect(page.getByText("Invoice issued.", { exact: true })).toBeVisible();
  await expect(page.getByLabel("Customer name", { exact: true })).toBeDisabled();
  const issuedPdfPath = path.join(userData, "invoice-acceptance-issued.pdf");
  await electronHarness.application.evaluate(({ dialog }, filePath) => {
    dialog.showSaveDialog = async () => ({ canceled: false, filePath });
  }, issuedPdfPath);
  await page.getByRole("button", { name: "Export PDF" }).click();
  await expect(page.getByText("Invoice PDF saved.", { exact: true })).toBeVisible();
  const issuedPdf = expectCustomerInvoicePdf(await readFile(issuedPdfPath), expense.id);
  expect(issuedPdf).not.toContain("DRAFT");
  await electronHarness.application.evaluate(({ Menu }) => {
    const item = Menu.getApplicationMenu()?.getMenuItemById("tammy-integrations-revoke");
    if (!item) throw new Error("PAIRED_API_MENU_MISSING");
    item.click(item, null, {} as KeyboardEvent);
  });
  await expect
    .poll(async () =>
      readFile(connectionPath).then(
        () => false,
        () => true,
      ),
    )
    .toBe(true);
  const restarted = await electronHarness.restart("accounting-workflow-restart");
  await restarted.getByLabel("Workspace passphrase").fill(CURRENT_WORKFLOW_PASSPHRASE);
  await restarted.getByLabel("Email or username").fill(CURRENT_WORKFLOW_USERNAME);
  await restarted.getByLabel("Administrator password").fill(CURRENT_WORKFLOW_ADMIN_PASSWORD);
  await restarted.getByRole("button", { name: "Unlock workspace" }).click();
  await restarted.getByRole("link", { name: "Invoices", exact: true }).click();
  await restarted.getByRole("button", { name: "Open INV-MCP-001" }).click();
  await expect(restarted.getByLabel("Customer name", { exact: true })).toHaveValue(
    "Reviewed invoice customer",
  );
  await expect(restarted.getByLabel("Customer name", { exact: true })).toBeDisabled();
  expect(electronHarness.consoleErrors).toEqual([]);
  expect(electronHarness.pageErrors).toEqual([]);
});

function expectCustomerInvoicePdf(bytes: Uint8Array, sourceDocumentId: string): string {
  expect(Buffer.from(bytes).subarray(0, 5).toString()).toBe("%PDF-");
  expect(bytes.byteLength).toBeGreaterThan(1000);
  const content = pdfStreams(bytes);
  for (const expected of [
    "Tax Invoice",
    "INV-MCP-001",
    "Reviewed invoice customer",
    "MCP accounting services",
    "440.08",
    "15.01",
    "455.09",
    "AUD",
  ]) {
    expect(content).toContain(expected);
  }
  expect(content).not.toContain("Invoice acceptance customer");
  expect(content).not.toContain(sourceDocumentId);
  expect(content).not.toMatch(/unitCostMinor|marginMinor|sourceDocumentId|Internal cost|Margin/);
  for (const privateAmount of ["$40.00", "$350.00", "$90.08"]) {
    expect(content).not.toContain(privateAmount);
  }
  return content;
}
