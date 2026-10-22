import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { pdfStreams } from "../../../../packages/invoices/test-support/pdf-text.ts";
import {
  normalizeAccessibilitySnapshot,
  validateScreenshotFixture,
} from "../../../../scripts/check-app-store-screenshots.mjs";
import screenshotFixture from "../../release/macos/screenshots/fixture.json" with { type: "json" };
import { expect, test } from "./fixtures";
import {
  addReviewedInvoiceExpense,
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
import {
  captureCompanyTemplate,
  configureCompanyInvoiceTemplate,
  expectCompanyTemplate,
  expectInvoiceTemplateSnapshot,
  ORIGINAL_COMPANY_TEMPLATE,
  UPDATED_COMPANY_TEMPLATE,
  updateCompanyInvoiceTemplate,
} from "./support/invoice-template-workflow";
import { callPairedApi, runPairedMcpSmoke } from "./support/local-api";

test("runs the current accounting workflows through the packaged app", async ({
  electronHarness,
}, testInfo) => {
  const workspace = await setupAndRunCurrentAccountingWorkflow(
    electronHarness.page,
    electronHarness,
    {
      fixedRendererClock: true,
    },
  );
  const fixture = validateScreenshotFixture(screenshotFixture);
  let page = electronHarness.currentPage();
  // Setup writes the review through the authenticated protocol fixture. Reload
  // the retained session so the Overview query reflects those committed writes.
  await page.reload();
  await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
  await expect(page.getByText("1 reviewed this quarter", { exact: true })).toBeVisible();
  const screenshotDirectory = path.resolve(
    path.dirname(fileURLToPath(import.meta.url)),
    "../../../../.tmp/invoice-cost-table-e2e",
  );
  await mkdir(screenshotDirectory, { recursive: true });
  const incomeEvidenceDirectory = path.resolve(
    path.dirname(fileURLToPath(import.meta.url)),
    "../../../../.tmp/invoice-income-bas-e2e",
  );
  await mkdir(incomeEvidenceDirectory, { recursive: true });
  const targets = [
    { heading: "Overview", link: "Overview", ready: "Nothing needs review" },
    {
      heading: "Documents",
      link: "Documents",
      ready: fixture.sourceDocument.sourceDisplayName,
    },
    { heading: "Trial balance", link: "Trial balance", ready: fixture.accounts[0]?.name },
    { heading: "Banking", link: "Banking", ready: "Reconciled" },
    { heading: "BAS preparation", link: "GST & BAS", ready: "BAS preparation" },
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
    if (target.heading === "Documents") {
      await expect(
        page.getByRole("option", { name: "6100 · Office expenses", exact: true }),
      ).toBeAttached();
      await expect(
        page.getByRole("option", { name: "3100 · Owner contributions", exact: true }),
      ).toBeAttached();
    }
    if (target.link === "GST & BAS") {
      const stages = page.getByRole("navigation", { name: "BAS lifecycle stages" });
      for (const stage of ["Prepare", "Review", "Freeze", "Export", "External record"] as const) {
        await expect(stages.getByRole("button", { name: stage, exact: true })).toBeAttached();
      }
      await expect(
        page.getByRole("heading", { name: "BAS preparation", exact: true }),
      ).toBeVisible();
    }
    const definition = fixture.screenshots[index];
    if (!definition) throw new Error("APP_STORE_SCREENSHOT_FIXTURE_INVALID");
    const actualSnapshot = normalizeAccessibilitySnapshot(
      await page.locator("body").ariaSnapshot(),
    );
    const accessibilityPath = path.join(screenshotDirectory, `current-workflow-${index + 1}.json`);
    const screenPath = path.join(screenshotDirectory, `current-workflow-${index + 1}.png`);
    await writeFile(accessibilityPath, JSON.stringify(actualSnapshot, null, 2));
    await page.screenshot({ animations: "disabled", path: screenPath });
    await testInfo.attach(`current-workflow-${index + 1}-accessibility`, {
      path: accessibilityPath,
      contentType: "text/plain",
    });
    await testInfo.attach(`current-workflow-${index + 1}-screen`, {
      path: screenPath,
      contentType: "image/png",
    });
    if (target.link === "GST & BAS") {
      expect.soft(actualSnapshot).toContain("BAS preparation");
      expect.soft(actualSnapshot).toContain("BAS lifecycle stages");
    } else {
      expect.soft(actualSnapshot).toEqual(definition.completeAccessibilityText);
    }
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
  const trainingExpense = await addReviewedInvoiceExpense(page, workspace);
  const costSources = await callPairedApi(connectionPath, "tammy.invoices.list_cost_sources");
  expect(costSources).toMatchObject({
    ok: true,
    data: {
      expenses: expect.arrayContaining([
        expect.objectContaining({ subtotalMinor: 29000, currency: "AUD" }),
        expect.objectContaining({ id: trainingExpense.id, subtotalMinor: 6000, gstMinor: 0 }),
      ]),
    },
  });
  expect(JSON.stringify(costSources)).not.toMatch(/extractedText|candidate/);
  const expense = costSources.data.expenses.find(
    (source: { subtotalMinor: number }) => source.subtotalMinor === 29000,
  );
  if (!expense) throw new Error("INVOICE_ACCEPTANCE_COST_SOURCE_MISSING");
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
  await configureCompanyInvoiceTemplate(page, screenshotDirectory, testInfo);
  await expect(page.getByTestId("invoice-total")).toContainText("$455.09");
  await expect(page.getByTestId("invoice-cost")).toContainText("$350.00");
  let lineTable = page.getByRole("table", { name: "Invoice line items", exact: true });
  const expenseTable = page.getByRole("table", { name: "Reviewed expenses", exact: true });
  await expect(lineTable).toBeVisible();
  await expect(expenseTable).toBeVisible();
  await expect(
    lineTable.getByText("Linked reviewed expense · internal only", { exact: true }),
  ).toBeVisible();
  const originalExpenseRow = expenseTable.getByRole("row").filter({ hasText: expense.description });
  await expect(originalExpenseRow.getByRole("checkbox")).toBeDisabled();
  await page.getByRole("button", { name: "Remove line 2", exact: true }).click();
  await expect(page.getByTestId("invoice-total")).toContainText("$165.09");
  await expect(originalExpenseRow.getByRole("checkbox")).toBeEnabled();
  await page.getByLabel("Search reviewed expenses", { exact: true }).fill("training");
  await expect(originalExpenseRow).toHaveCount(0);
  await expect(
    expenseTable.getByRole("row").filter({ hasText: trainingExpense.description }),
  ).toBeVisible();
  await page.getByLabel("Search reviewed expenses", { exact: true }).fill("");
  await originalExpenseRow.getByRole("checkbox").check();
  await expenseTable
    .getByRole("row")
    .filter({ hasText: trainingExpense.description })
    .getByRole("checkbox")
    .check();
  await page.getByLabel("Markup percentage", { exact: true }).fill("25");
  await page.getByRole("button", { name: "Add selected expenses", exact: true }).click();
  await expect(lineTable.getByRole("textbox", { name: /^Description \d+$/ })).toHaveCount(3);
  const secondDescription = await page.getByLabel("Description 2", { exact: true }).inputValue();
  const thirdDescription = await page.getByLabel("Description 3", { exact: true }).inputValue();
  expect([secondDescription, thirdDescription].sort()).toEqual(
    [expense.description, trainingExpense.description].sort(),
  );
  const expenseLine = secondDescription === expense.description ? 2 : 3;
  const trainingLine = expenseLine === 2 ? 3 : 2;
  await expect(page.getByLabel(`Unit price ${expenseLine}`, { exact: true })).toHaveValue("362.50");
  await expect(page.getByLabel(`Unit cost ${expenseLine}`, { exact: true })).toHaveValue("290.00");
  await expect(page.getByLabel(`Unit price ${trainingLine}`, { exact: true })).toHaveValue("75.00");
  await expect(page.getByLabel(`GST treatment ${trainingLine}`, { exact: true })).toHaveValue(
    "taxable",
  );
  await expect(expenseTable.getByRole("checkbox")).toHaveCount(2);
  for (const checkbox of await expenseTable.getByRole("checkbox").all()) {
    await expect(checkbox).toBeDisabled();
    await expect(checkbox).not.toBeChecked();
  }
  await expect(page.getByTestId("invoice-total")).toContainText("$646.34");
  await page.getByLabel(`Unit price ${expenseLine}`, { exact: true }).fill("375.25");
  await expect(page.getByTestId("invoice-subtotal")).toContainText("$600.33");
  await expect(page.getByTestId("invoice-gst")).toContainText("$60.04");
  await expect(page.getByTestId("invoice-total")).toContainText("$660.37");
  await expect(page.getByTestId("invoice-cost")).toContainText("$410.00");
  await expect(page.getByTestId("invoice-margin")).toContainText("$190.33");
  await expect(page.getByRole("button", { name: "Export PDF", exact: true })).toBeDisabled();
  await page.getByLabel("Customer name", { exact: true }).fill("Reviewed invoice customer");
  await page.getByRole("link", { name: "Documents", exact: true }).click();
  await page.getByRole("link", { name: "Invoices", exact: true }).click();
  await expect(page.getByLabel("Customer name", { exact: true })).toHaveValue(
    "Reviewed invoice customer",
  );
  await page.getByRole("button", { name: "Save draft", exact: true }).click();
  await expect(page.getByText("Draft saved.", { exact: true })).toBeVisible();
  await lineTable.scrollIntoViewIfNeeded();
  await resetTableHorizontalScroll(lineTable);
  await resetTableHorizontalScroll(expenseTable);
  const wideScreenshotPath = path.join(screenshotDirectory, "invoice-cost-table-desktop.png");
  await page.screenshot({ animations: "disabled", path: wideScreenshotPath });
  await testInfo.attach("invoice-cost-table-desktop", {
    contentType: "image/png",
    path: wideScreenshotPath,
  });
  const desktopSize = await electronHarness.application.evaluate(({ BrowserWindow }) => {
    const window = BrowserWindow.getAllWindows()[0];
    if (!window) throw new Error("INVOICE_ACCEPTANCE_WINDOW_MISSING");
    const size = window.getContentSize();
    window.setContentSize(1024, 700);
    return size;
  });
  await expect
    .poll(() => page.evaluate(() => [window.innerWidth, window.innerHeight]))
    .toEqual([1024, 700]);
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth))
    .toBe(true);
  await expect
    .poll(() =>
      page.getByRole("main").evaluate((element) => element.scrollWidth <= element.clientWidth),
    )
    .toBe(true);
  await lineTable.scrollIntoViewIfNeeded();
  await resetTableHorizontalScroll(lineTable);
  await resetTableHorizontalScroll(expenseTable);
  const compactScreenshotPath = path.join(screenshotDirectory, "invoice-cost-table-1024x700.png");
  await page.screenshot({ animations: "disabled", path: compactScreenshotPath });
  await testInfo.attach("invoice-cost-table-1024x700", {
    contentType: "image/png",
    path: compactScreenshotPath,
  });
  await page.getByRole("button", { name: "Manage invoice template", exact: true }).click();
  await expectCompanyTemplate(page, ORIGINAL_COMPANY_TEMPLATE, true);
  await captureCompanyTemplate(page, screenshotDirectory, "invoice-template-1024x700", testInfo);
  await page.getByRole("button", { name: "Close template editor", exact: true }).click();
  await electronHarness.application.evaluate(({ BrowserWindow }, size) => {
    const window = BrowserWindow.getAllWindows()[0];
    if (!window || size[0] === undefined || size[1] === undefined) {
      throw new Error("INVOICE_ACCEPTANCE_WINDOW_MISSING");
    }
    window.setContentSize(size[0], size[1]);
  }, desktopSize);
  const draftPdfPath = path.join(userData, "invoice-acceptance-draft.pdf");
  await electronHarness.application.evaluate(({ dialog }, filePath) => {
    dialog.showSaveDialog = async () => ({ canceled: false, filePath });
  }, draftPdfPath);
  await page.getByRole("button", { name: "Export PDF" }).click();
  await expect(page.getByText("Invoice PDF saved.", { exact: true })).toBeVisible();
  const draftPdf = expectCustomerInvoicePdf(await readFile(draftPdfPath), [
    expense.id,
    trainingExpense.id,
  ]);
  expect(draftPdf).toContain("DRAFT");
  const beforeIssueIncome = await readBusinessIncome(
    page,
    workspace,
    "2026-07-01",
    "2026-09-30",
    "2026-10-02",
  );
  expect(beforeIssueIncome.invoiceIncome?.minorUnits).toBe(0n);
  expect(beforeIssueIncome.businessIncome?.minorUnits).toBe(0n);
  expect(beforeIssueIncome.outstanding?.minorUnits).toBe(0n);
  page = await electronHarness.restart("accounting-workflow-restart");
  await unlockCurrentWorkflowWorkspace(page);
  await page.getByRole("link", { name: "Invoices", exact: true }).click();
  await page.getByRole("button", { name: "Open INV-MCP-001", exact: true }).click();
  lineTable = page.getByRole("table", { name: "Invoice line items", exact: true });
  await expect(page.getByLabel("Customer name", { exact: true })).toHaveValue(
    "Reviewed invoice customer",
  );
  await expect(page.getByLabel(`Unit price ${expenseLine}`, { exact: true })).toBeEnabled();
  await expect(page.getByLabel(`Unit price ${expenseLine}`, { exact: true })).toHaveValue("375.25");
  await expect(page.getByLabel(`Unit cost ${expenseLine}`, { exact: true })).toHaveValue("290.00");
  await expect(page.getByLabel(`Unit price ${trainingLine}`, { exact: true })).toHaveValue("75.00");
  await expect(page.getByLabel("Quantity 1", { exact: true })).toHaveValue("1.5");
  await expect(page.getByTestId("invoice-total")).toContainText("$660.37");
  await expect(page.getByTestId("invoice-cost")).toContainText("$410.00");
  await expectInvoiceTemplateSnapshot(page, ORIGINAL_COMPANY_TEMPLATE, "2026-10-30", true);
  await page.getByRole("button", { name: "Manage invoice template", exact: true }).click();
  await expectCompanyTemplate(page, ORIGINAL_COMPANY_TEMPLATE, true);
  await updateCompanyInvoiceTemplate(page);
  await captureCompanyTemplate(page, screenshotDirectory, "invoice-template-updated", testInfo);
  const snapshotPdfPath = path.join(userData, "invoice-acceptance-template-snapshot.pdf");
  await electronHarness.application.evaluate(({ dialog }, filePath) => {
    dialog.showSaveDialog = async () => ({ canceled: false, filePath });
  }, snapshotPdfPath);
  await page.getByRole("button", { name: "Export PDF", exact: true }).click();
  await expect(page.getByText("Invoice PDF saved.", { exact: true })).toBeVisible();
  const snapshotPdf = expectCustomerInvoicePdf(await readFile(snapshotPdfPath), [
    expense.id,
    trainingExpense.id,
  ]);
  expect(snapshotPdf).toContain("DRAFT");
  await page.getByRole("button", { name: "New invoice", exact: true }).click();
  const newIssueDate = await page.getByLabel("Issue date", { exact: true }).inputValue();
  const updatedDueDate = new Date(`${newIssueDate}T00:00:00Z`);
  updatedDueDate.setUTCDate(updatedDueDate.getUTCDate() + 7);
  await expectInvoiceTemplateSnapshot(
    page,
    UPDATED_COMPANY_TEMPLATE,
    updatedDueDate.toISOString().slice(0, 10),
    false,
  );
  await page.getByLabel("Invoice number", { exact: true }).fill("INV-TEMPLATE-002");
  await page.getByLabel("Customer name", { exact: true }).fill("Updated template customer");
  await page
    .getByRole("textbox", { name: "Customer address", exact: true })
    .fill("2 Customer Road, Melbourne VIC 3000");
  await page.getByLabel("Description 1", { exact: true }).fill("Template defaults service");
  await page.getByLabel("Unit price 1", { exact: true }).fill("50.00");
  await page.getByRole("button", { name: "Save draft", exact: true }).click();
  await expect(page.getByText("Draft saved.", { exact: true })).toBeVisible();
  const updatedPdfPath = path.join(userData, "invoice-acceptance-updated-template.pdf");
  await electronHarness.application.evaluate(({ dialog }, filePath) => {
    dialog.showSaveDialog = async () => ({ canceled: false, filePath });
  }, updatedPdfPath);
  await page.getByRole("button", { name: "Export PDF", exact: true }).click();
  await expect(page.getByText("Invoice PDF saved.", { exact: true })).toBeVisible();
  const updatedPdfBytes = await readFile(updatedPdfPath);
  const updatedPdf = expectInvoicePdfBranding(updatedPdfBytes, UPDATED_COMPANY_TEMPLATE, false);
  expect(updatedPdf).toContain("INV-TEMPLATE-002");
  expect(updatedPdf).toContain("Updated template customer");
  expect(updatedPdf).not.toContain(ORIGINAL_COMPANY_TEMPLATE.paymentInstructions);
  await page.getByRole("button", { name: "Close template editor", exact: true }).click();
  await page.getByRole("button", { name: "Open INV-MCP-001", exact: true }).click();
  await expectInvoiceTemplateSnapshot(page, ORIGINAL_COMPANY_TEMPLATE, "2026-10-30", true);
  await page.getByRole("button", { name: "Issue invoice", exact: true }).click();
  await page.getByRole("button", { name: "Confirm issue", exact: true }).click();
  await expect(page.getByText("Invoice issued.", { exact: true })).toBeVisible();
  await expect(page.getByLabel("Customer name", { exact: true })).toBeDisabled();
  await expect(lineTable.getByLabel(`Unit price ${expenseLine}`, { exact: true })).toBeDisabled();
  await expect(page.getByRole("textbox", { name: "Seller address", exact: true })).toBeDisabled();
  await expect(page.getByRole("textbox", { name: "Notes", exact: true })).toBeDisabled();
  await expect(
    page.getByRole("textbox", { name: "Payment instructions", exact: true }),
  ).toBeDisabled();
  await expect(page.getByRole("button", { name: "Remove invoice logo", exact: true })).toHaveCount(
    0,
  );
  const issuedPdfPath = path.join(userData, "invoice-acceptance-issued.pdf");
  await electronHarness.application.evaluate(({ dialog }, filePath) => {
    dialog.showSaveDialog = async () => ({ canceled: false, filePath });
  }, issuedPdfPath);
  await page.getByRole("button", { name: "Export PDF" }).click();
  await expect(page.getByText("Invoice PDF saved.", { exact: true })).toBeVisible();
  const issuedPdf = expectCustomerInvoicePdf(await readFile(issuedPdfPath), [
    expense.id,
    trainingExpense.id,
  ]);
  expect(issuedPdf).not.toContain("DRAFT");
  const issuedWorkspace = await authenticatedWorkspace(page);
  await expectInvoiceJournal(page, issuedWorkspace, "INV-MCP-001", 60033n, 66037n, 6004n);
  await showBusinessIncome(page, "2026-07-01", "2026-09-30", "2026-10-02");
  await expect(page.getByTestId("business-income-total")).toContainText("$600.33");
  await expect(page.getByTestId("business-invoice-income")).toContainText("$600.33");
  await expect(page.getByTestId("business-outstanding")).toContainText("$660.37");
  await page.getByRole("link", { name: "Journals", exact: true }).click();
  await expect(
    page.getByRole("row", { name: "30/09/2026 Invoice INV-MCP-001 $660.37 Posted", exact: true }),
  ).toBeVisible();
  await regenerateInvoiceBas(page, "2026-07-01", "2026-09-30", "$660.37", "$60.04");
  await expect(page.getByRole("table", { name: "BAS sales sources", exact: true })).toContainText(
    "Issued invoice",
  );
  const invoiceBasPath = path.join(incomeEvidenceDirectory, "non-cash-issued-q3-bas.png");
  await page.screenshot({ animations: "disabled", path: invoiceBasPath });
  await testInfo.attach("non-cash-issued-q3-bas", {
    path: invoiceBasPath,
    contentType: "image/png",
  });
  await recordInvoiceReceipt(page, "INV-MCP-001", "2026-10-02", "330.18", "First actual receipt");
  await expect(page.getByTestId("invoice-outstanding")).toContainText("$330.19");
  await showBusinessIncome(page, "2026-10-01", "2026-12-31", "2026-10-02");
  await expect(page.getByTestId("business-income-total")).toContainText("$0.00");
  await expect(page.getByTestId("business-payments-received")).toContainText("$330.18");
  await expect(page.getByTestId("business-outstanding")).toContainText("$330.19");
  const incomeOverviewPath = path.join(
    incomeEvidenceDirectory,
    "non-cash-receipts-q4-overview.png",
  );
  await page.screenshot({ animations: "disabled", path: incomeOverviewPath });
  await testInfo.attach("invoice-income-overview", {
    path: incomeOverviewPath,
    contentType: "image/png",
  });
  await regenerateInvoiceBas(page, "2026-10-01", "2026-12-31", "$0.00", "$0.00");
  const receiptsBasPath = path.join(incomeEvidenceDirectory, "non-cash-receipts-q4-bas.png");
  await page.screenshot({ animations: "disabled", path: receiptsBasPath });
  await testInfo.attach("non-cash-receipts-q4-bas", {
    path: receiptsBasPath,
    contentType: "image/png",
  });
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
  await unlockCurrentWorkflowWorkspace(restarted);
  await restarted.getByRole("link", { name: "Invoices", exact: true }).click();
  await restarted.getByRole("button", { name: "Open INV-MCP-001" }).click();
  await expect(restarted.getByLabel("Customer name", { exact: true })).toHaveValue(
    "Reviewed invoice customer",
  );
  await expect(restarted.getByLabel("Customer name", { exact: true })).toBeDisabled();
  await expect(restarted.getByLabel(`Unit price ${expenseLine}`, { exact: true })).toHaveValue(
    "375.25",
  );
  await expect(restarted.getByLabel(`Unit price ${trainingLine}`, { exact: true })).toHaveValue(
    "75.00",
  );
  await expect(restarted.getByTestId("invoice-total")).toContainText("$660.37");
  await expect(restarted.getByTestId("invoice-received")).toContainText("$330.18");
  await expect(restarted.getByTestId("invoice-outstanding")).toContainText("$330.19");
  const retainedAccounting = await readInvoiceAccounting(
    restarted,
    await authenticatedWorkspace(restarted),
    "INV-MCP-001",
  );
  expect(retainedAccounting.accounting.paymentCount).toBe(1n);
  expect(retainedAccounting.accounting.payments[0]?.reference).toBe("First actual receipt");
  await expectInvoiceTemplateSnapshot(restarted, ORIGINAL_COMPANY_TEMPLATE, "2026-10-30", true);
  await restarted.getByRole("button", { name: "Manage invoice template", exact: true }).click();
  await expectCompanyTemplate(restarted, UPDATED_COMPANY_TEMPLATE, false);
  await expect(
    restarted.getByRole("button", { name: "Apply saved template", exact: true }),
  ).toBeDisabled();
  expect(electronHarness.consoleErrors).toEqual([]);
  expect(electronHarness.pageErrors).toEqual([]);
});

async function resetTableHorizontalScroll(table: import("@playwright/test").Locator) {
  await table.evaluate((element) => {
    let container = element.parentElement;
    while (container) {
      if (["auto", "scroll"].includes(getComputedStyle(container).overflowX)) {
        container.scrollLeft = 0;
        return;
      }
      container = container.parentElement;
    }
  });
}

async function unlockCurrentWorkflowWorkspace(page: import("@playwright/test").Page) {
  await page.getByLabel("Workspace passphrase").fill(CURRENT_WORKFLOW_PASSPHRASE);
  await page.getByLabel("Email or username").fill(CURRENT_WORKFLOW_USERNAME);
  await page.getByLabel("Administrator password").fill(CURRENT_WORKFLOW_ADMIN_PASSWORD);
  await page.getByRole("button", { name: "Unlock workspace" }).click();
  await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
}

function expectCustomerInvoicePdf(bytes: Uint8Array, sourceDocumentIds: readonly string[]): string {
  const content = expectInvoicePdfBranding(bytes, ORIGINAL_COMPANY_TEMPLATE, true);
  for (const expected of [
    "Tax Invoice",
    "INV-MCP-001",
    "Reviewed invoice customer",
    "MCP accounting services",
    "375.25",
    "600.33",
    "60.04",
    "660.37",
    "Invoice acceptance training",
    "AUD",
  ]) {
    expect(content).toContain(expected);
  }
  expect(content).not.toContain("Invoice acceptance customer");
  for (const sourceDocumentId of sourceDocumentIds) expect(content).not.toContain(sourceDocumentId);
  expect(content).not.toMatch(
    /unitCostMinor|marginMinor|sourceDocumentId|Internal cost|Private cost|Margin|Profit/,
  );
  for (const privateAmount of ["$40.00", "$60.00", "$290.00", "$410.00", "$190.33"]) {
    expect(content).not.toContain(privateAmount);
  }
  return content;
}

function expectInvoicePdfBranding(
  bytes: Uint8Array,
  values: {
    readonly sellerAddress: string;
    readonly notes: string;
    readonly paymentInstructions: string;
  },
  hasLogo: boolean,
): string {
  expect(Buffer.from(bytes).subarray(0, 5).toString()).toBe("%PDF-");
  expect(bytes.byteLength).toBeGreaterThan(1000);
  const content = pdfStreams(bytes);
  expect(content).toContain(values.sellerAddress);
  expect(content).toContain(values.notes);
  expect(content).toContain(values.paymentInstructions);
  const source = Buffer.from(bytes).toString("latin1");
  if (hasLogo) {
    expect(source).toMatch(/\/Subtype\s*\/Image\b/);
    expect(source).toMatch(/\/Width\s+240\b/);
    expect(source).toMatch(/\/Height\s+96\b/);
    expect(source).toMatch(/\/SMask\s+\d+\s+0\s+R/);
    expect(content).not.toContain(UPDATED_COMPANY_TEMPLATE.paymentInstructions);
  } else {
    expect(source).not.toMatch(/\/Subtype\s*\/Image\b/);
  }
  return content;
}
