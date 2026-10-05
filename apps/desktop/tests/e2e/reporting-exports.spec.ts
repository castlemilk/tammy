import { mkdir, readdir, readFile, rm, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { create } from "@bufbuild/protobuf";
import type { Locator, Page, TestInfo } from "@playwright/test";
import {
  DocumentCandidateSchema,
  IngestDocumentRequestSchema,
  IngestDocumentResponseSchema,
  SaveDocumentReviewRequestSchema,
  SaveDocumentReviewResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import type { BusinessIncomeSummary } from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import {
  GetOrganisationRequestSchema,
  GetOrganisationResponseSchema,
  type Organisation,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import {
  type BasWorkpaper,
  GetCurrentBasDraftRequestSchema,
  GetCurrentBasDraftResponseSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { createProtoMethodCodec } from "../../src/shared/proto-ipc";
import { type ElectronHarness, expect, test } from "./fixtures";
import { invokeConveyorFrame } from "./support/conveyor";
import {
  type AuthenticatedAccountingWorkspace,
  authenticatedWorkspace,
  CURRENT_WORKFLOW_ADMIN_PASSWORD,
  CURRENT_WORKFLOW_PASSPHRASE,
  CURRENT_WORKFLOW_USERNAME,
  setupAndRunCurrentAccountingWorkflow,
} from "./support/current-accounting-workflow";
import {
  expectInvoiceJournal,
  readBusinessIncome,
  recordInvoiceReceipt,
  regenerateInvoiceBas,
  showBusinessIncome,
} from "./support/invoice-income-workflow";
import {
  exactDecimal,
  type ParsedReportCsv,
  parseReportCsv,
  parseReportPdf,
  reportArtifactHash,
} from "./support/reporting-export-artifacts";

const periodStart = "2026-10-01";
const periodEnd = "2026-12-31";
const asOfDate = "2026-10-03";
const customer = 'Export customer, "Élodie"';
const supplier = '=Export supplier, "Élodie"';
const revisedSupplier = '=Updated export supplier, "Élodie"';
const purchaseNumber = "EXPORT-PURCHASE-001";

const organisationCodec = createProtoMethodCodec({
  input: GetOrganisationRequestSchema,
  output: GetOrganisationResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 32768,
});
const basCodec = createProtoMethodCodec({
  input: GetCurrentBasDraftRequestSchema,
  output: GetCurrentBasDraftResponseSchema,
  maximumRequestBytes: 16384,
  maximumResponseBytes: 4194304,
});
const ingestCodec = createProtoMethodCodec({
  input: IngestDocumentRequestSchema,
  output: IngestDocumentResponseSchema,
  maximumRequestBytes: 11 * 1024 * 1024,
  maximumResponseBytes: 2 * 1024 * 1024,
});
const reviewCodec = createProtoMethodCodec({
  input: SaveDocumentReviewRequestSchema,
  output: SaveDocumentReviewResponseSchema,
  maximumRequestBytes: 32768,
  maximumResponseBytes: 2 * 1024 * 1024,
});

for (const gstBasis of ["cash", "non-cash"] as const) {
  test(`reporting/exports: ${gstBasis} income and retained BAS files reconcile through cancellation, regeneration and restart`, async ({
    electronHarness,
  }, testInfo) => {
    test.setTimeout(180_000);
    const directory = fileURLToPath(
      new URL(`../../../../.tmp/reporting-exports-e2e/${gstBasis}/`, import.meta.url),
    );
    await rm(directory, { recursive: true, force: true });
    await mkdir(directory, { recursive: true });
    const workspace = await setupAndRunCurrentAccountingWorkflow(
      electronHarness.page,
      electronHarness,
      { gstBasis },
    );
    let page = electronHarness.currentPage();
    const number = `EXPORT-${gstBasis === "cash" ? "CASH" : "ACCRUAL"}-001`;
    await issueExportInvoice(page, number);
    await expectInvoiceJournal(page, workspace, number, 17000n, 18000n, 1000n);
    await recordInvoiceReceipt(page, number, "2026-10-02", "90.00", "Export partial receipt");
    await retainPurchase(page, workspace);
    const organisation = await readOrganisation(page, workspace);
    await showBusinessIncome(page, periodStart, periodEnd, asOfDate);
    const income = await readBusinessIncome(page, workspace, periodStart, periodEnd, asOfDate);
    expect(income.businessIncome?.minorUnits).toBe(17000n);
    expect(income.invoiceIncome?.minorUnits).toBe(17000n);
    expect(income.paymentsReceived?.minorUnits).toBe(9000n);
    expect(income.outstanding?.minorUnits).toBe(9000n);
    const incomePanel = page.getByRole("region", { name: "Business income", exact: true });
    const incomeCsvPath = path.join(directory, "income.csv");
    const incomePdfPath = path.join(directory, "income.pdf");
    await saveExport(electronHarness, incomePanel, "CSV", incomeCsvPath);
    await saveExport(electronHarness, incomePanel, "PDF", incomePdfPath);
    assertIncomeCsv(await parseReportCsv(incomeCsvPath), income, organisation);
    assertIncomePdf(await parseReportPdf(incomePdfPath), income, organisation);
    await expectIncomeDates(page);

    await regenerateInvoiceBas(
      page,
      periodStart,
      periodEnd,
      gstBasis === "cash" ? "$80.00" : "$160.00",
      gstBasis === "cash" ? "$5.00" : "$10.00",
    );
    const initialBas = await readBas(page, workspace);
    expect(initialBas.sourcesChanged).toBe(false);
    expect(initialBas.salesSources).toHaveLength(1);
    expect(initialBas.sources).toHaveLength(gstBasis === "cash" ? 0 : 1);
    expect(initialBas.excludedCashPurchaseCount).toBe(gstBasis === "cash" ? 1n : 0n);
    const basCsvPath = path.join(directory, "bas.csv");
    const basPdfPath = path.join(directory, "bas.pdf");
    await saveExport(electronHarness, page.locator("main"), "CSV", basCsvPath);
    await saveExport(electronHarness, page.locator("main"), "PDF", basPdfPath);
    assertBasCsv(await parseReportCsv(basCsvPath), initialBas, organisation);
    assertBasPdf(await parseReportPdf(basPdfPath), initialBas, organisation);
    const beforeExport = await readBas(page, workspace);
    expect(beforeExport).toEqual(initialBas);

    const cancelledPath = path.join(directory, "cancelled-bas.csv");
    await electronHarness.application.evaluate(({ dialog }, filePath) => {
      dialog.showSaveDialog = async () => ({ canceled: true, filePath });
    }, cancelledPath);
    await page.getByRole("button", { name: "Export CSV", exact: true }).click();
    await expect(page.getByText("BAS export cancelled.", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "Export CSV", exact: true })).toBeEnabled();
    await expectMissingFile(cancelledPath);
    expect(await readBas(page, workspace)).toEqual(initialBas);
    await expect(page.getByLabel("Period start", { exact: true })).toHaveValue(periodStart);
    await expect(page.getByLabel("Period end", { exact: true })).toHaveValue(periodEnd);

    const invalidDestination = path.join(directory, "directory.csv");
    await mkdir(invalidDestination);
    await electronHarness.application.evaluate(({ dialog }, filePath) => {
      dialog.showSaveDialog = async () => ({ canceled: false, filePath });
    }, invalidDestination);
    await page.getByRole("button", { name: "Export CSV", exact: true }).click();
    await expect(
      page.getByText(
        "The report could not be saved. Choose a writable PDF or CSV file and try again.",
        { exact: true },
      ),
    ).toBeVisible();
    expect(await readdir(invalidDestination)).toEqual([]);
    expect(await readBas(page, workspace)).toEqual(initialBas);
    await rm(invalidDestination, { recursive: true });

    await recordInvoiceReceipt(page, number, "2026-10-03", "45.00", "Export later receipt");
    await retainPurchase(page, workspace, revisedSupplier, "EXPORT-PURCHASE-002", 2);
    await page.getByRole("link", { name: "GST & BAS", exact: true }).click();
    await expect(page.getByText(/Sources changed since this workpaper was saved/)).toBeVisible();
    const staleBas = await readBas(page, workspace);
    expect(staleBas.id).toBe(initialBas.id);
    expect(staleBas.version).toBe(initialBas.version);
    expect(staleBas.sourcesChanged).toBe(true);
    expect(staleBas.salesG1).toEqual(initialBas.salesG1);
    expect(staleBas.gstOnSales1a).toEqual(initialBas.gstOnSales1a);
    expect(staleBas.sources).toEqual(initialBas.sources);
    const staleCsvPath = path.join(directory, "stale-bas.csv");
    const stalePdfPath = path.join(directory, "stale-bas.pdf");
    await saveExport(electronHarness, page.locator("main"), "CSV", staleCsvPath);
    await saveExport(electronHarness, page.locator("main"), "PDF", stalePdfPath);
    assertBasCsv(await parseReportCsv(staleCsvPath), staleBas, organisation);
    assertBasPdf(await parseReportPdf(stalePdfPath), staleBas, organisation);
    expect(await readBas(page, workspace)).toEqual(staleBas);

    await regenerateInvoiceBas(
      page,
      periodStart,
      periodEnd,
      gstBasis === "cash" ? "$120.00" : "$160.00",
      gstBasis === "cash" ? "$7.50" : "$10.00",
    );
    const regenerated = await readBas(page, workspace);
    expect(regenerated.id).not.toBe(initialBas.id);
    expect(regenerated.sourcesChanged).toBe(false);
    expect(regenerated.salesSources).toHaveLength(gstBasis === "cash" ? 2 : 1);
    expect(regenerated.sources).toHaveLength(gstBasis === "cash" ? 0 : 2);
    expect(regenerated.excludedCashPurchaseCount).toBe(gstBasis === "cash" ? 2n : 0n);
    expect(regenerated.gstCredits1b?.minorUnits).toBe(gstBasis === "cash" ? 0n : 2000n);
    expect(regenerated.netGstPayable?.minorUnits).toBe(gstBasis === "cash" ? 750n : -1000n);
    if (gstBasis === "non-cash")
      expect(regenerated.sources.map((source) => source.supplierName)).toContain(revisedSupplier);
    const regeneratedCsvPath = path.join(directory, "regenerated-bas.csv");
    const regeneratedPdfPath = path.join(directory, "regenerated-bas.pdf");
    await saveExport(electronHarness, page.locator("main"), "CSV", regeneratedCsvPath);
    await saveExport(electronHarness, page.locator("main"), "PDF", regeneratedPdfPath);
    assertBasCsv(await parseReportCsv(regeneratedCsvPath), regenerated, organisation);
    assertBasPdf(await parseReportPdf(regeneratedPdfPath), regenerated, organisation);
    // Saved exports themselves remain immutable after both ledger and document changes.
    assertBasCsv(await parseReportCsv(basCsvPath), initialBas, organisation);
    assertIncomeCsv(await parseReportCsv(incomeCsvPath), income, organisation);
    const screenshot = path.join(directory, "regenerated-bas.png");
    await page.screenshot({ animations: "disabled", path: screenshot });
    await testInfo.attach("regenerated-bas", { contentType: "image/png", path: screenshot });

    page = await electronHarness.restart("accounting-workflow-restart");
    await unlockWorkspace(page);
    const restartedWorkspace = await authenticatedWorkspace(page);
    await page.getByRole("link", { name: "GST & BAS", exact: true }).click();
    expect(await readBas(page, restartedWorkspace)).toEqual(regenerated);
    const restartedCsvPath = path.join(directory, "restarted-bas.csv");
    await saveExport(electronHarness, page.locator("main"), "CSV", restartedCsvPath);
    assertBasCsv(await parseReportCsv(restartedCsvPath), regenerated, organisation);
    await showBusinessIncome(page, periodStart, periodEnd, asOfDate);
    const retainedIncome = await readBusinessIncome(
      page,
      restartedWorkspace,
      periodStart,
      periodEnd,
      asOfDate,
    );
    expect(retainedIncome.paymentsReceived?.minorUnits).toBe(13500n);
    expect(retainedIncome.outstanding?.minorUnits).toBe(4500n);
    const restartedIncomePath = path.join(directory, "restarted-income.csv");
    await saveExport(
      electronHarness,
      page.getByRole("region", { name: "Business income", exact: true }),
      "CSV",
      restartedIncomePath,
    );
    assertIncomeCsv(await parseReportCsv(restartedIncomePath), retainedIncome, organisation);
    await expectIncomeDates(page);
    const artifacts = [
      incomeCsvPath,
      incomePdfPath,
      basCsvPath,
      basPdfPath,
      staleCsvPath,
      stalePdfPath,
      regeneratedCsvPath,
      regeneratedPdfPath,
      restartedCsvPath,
      restartedIncomePath,
    ];
    await retainEvidence(electronHarness, testInfo, directory, artifacts, {
      gstBasis,
      organisationId: workspace.organisationId,
      incomeFinancialRevision: income.financialRevision.toString(),
      initialWorkpaperId: initialBas.id,
      regeneratedWorkpaperId: regenerated.id,
    });
    expect((await readdir(directory)).filter((entry) => entry.startsWith(".tammy-"))).toEqual([]);
    expect(electronHarness.consoleErrors).toEqual([]);
    expect(electronHarness.pageErrors).toEqual([]);
  });
}

async function issueExportInvoice(page: Page, number: string) {
  await page.getByRole("link", { name: "Invoices", exact: true }).click();
  await page.getByLabel("Invoice number", { exact: true }).fill(number);
  await page.getByLabel("Issue date", { exact: true }).fill("2026-10-01");
  await page.getByLabel("Due date", { exact: true }).fill("2026-10-15");
  await page.getByLabel("Customer name", { exact: true }).fill(customer);
  await page
    .getByRole("textbox", { name: "Customer address", exact: true })
    .fill("1 Export Road, Melbourne VIC 3000");
  await page
    .getByRole("textbox", { name: "Seller address", exact: true })
    .fill("27 Cambridge Road, Mooroolbark VIC 3138");
  await page.getByLabel("Registered for GST", { exact: true }).check();
  for (const [index, line] of [
    { description: "Taxable report service", price: "100.00", tax: "taxable" },
    { description: "GST-free report service", price: "50.00", tax: "gst-free" },
    { description: "Out-of-scope report service", price: "20.00", tax: "out-of-scope" },
  ].entries()) {
    if (index > 0) await page.getByRole("button", { name: "Add line item", exact: true }).click();
    await page.getByLabel(`Description ${index + 1}`, { exact: true }).fill(line.description);
    await page.getByLabel(`Unit price ${index + 1}`, { exact: true }).fill(line.price);
    await page.getByLabel(`GST treatment ${index + 1}`, { exact: true }).selectOption(line.tax);
  }
  await page.getByRole("button", { name: "Save draft", exact: true }).click();
  await expect(page.getByText("Draft saved.", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Issue invoice", exact: true }).click();
  await page.getByRole("button", { name: "Confirm issue", exact: true }).click();
  await expect(page.getByText("Invoice issued.", { exact: true })).toBeVisible();
}

function authority(workspace: AuthenticatedAccountingWorkspace) {
  return { actorUserId: workspace.userId, sessionId: workspace.sessionId };
}

function purchaseCandidate(supplierName: string, invoiceNumber: string) {
  return create(DocumentCandidateSchema, {
    supplierName,
    invoiceNumber,
    documentDate: { year: 2026, month: 10, day: 3 },
    subtotal: { currencyCode: "AUD", minorUnits: 10000n },
    gst: { currencyCode: "AUD", minorUnits: 1000n },
    total: { currencyCode: "AUD", minorUnits: 11000n },
  });
}

async function retainPurchase(
  page: Page,
  workspace: AuthenticatedAccountingWorkspace,
  supplierName = supplier,
  invoiceNumber = purchaseNumber,
  sequence = 1,
) {
  const text = `${supplierName}\n${invoiceNumber}\nSubtotal 100.00\nGST 10.00\nTotal 110.00`;
  const retained = ingestCodec.decodeResponse(
    await invokeConveyorFrame(
      page,
      "documents",
      "ingestDocument",
      ingestCodec.encodeRequest(
        create(IngestDocumentRequestSchema, {
          commandContext: {
            authentication: authority(workspace),
            idempotencyKey:
              sequence === 1
                ? "019bb2ec-9600-7000-8000-000000000001"
                : "019bb2ec-9600-7000-8000-000000000003",
          },
          organisationId: workspace.organisationId,
          sourceDisplayName: `${invoiceNumber.toLowerCase()}.pdf`,
          mimeType: "application/pdf",
          original: new TextEncoder().encode(`%PDF-1.7\nSynthetic export evidence\n${text}`),
          extractedText: text,
          candidate: purchaseCandidate(supplierName, invoiceNumber),
        }),
      ),
    ),
  );
  if (!retained.document) throw new Error("REPORTING_ACCEPTANCE_PURCHASE_MISSING");
  const response = reviewCodec.decodeResponse(
    await invokeConveyorFrame(
      page,
      "documents",
      "saveDocumentReview",
      reviewCodec.encodeRequest(
        create(SaveDocumentReviewRequestSchema, {
          commandContext: {
            authentication: authority(workspace),
            idempotencyKey:
              sequence === 1
                ? "019bb2ec-9600-7000-8000-000000000002"
                : "019bb2ec-9600-7000-8000-000000000004",
          },
          documentId: retained.document.id,
          expectedVersion: retained.document.version,
          candidate: purchaseCandidate(supplierName, invoiceNumber),
        }),
      ),
    ),
  );
  if (!response.document) throw new Error("REPORTING_ACCEPTANCE_PURCHASE_REVIEW_MISSING");
  return response.document;
}

async function readOrganisation(
  page: Page,
  workspace: AuthenticatedAccountingWorkspace,
): Promise<Organisation> {
  const result = organisationCodec.decodeResponse(
    await invokeConveyorFrame(
      page,
      "organisation",
      "getOrganisation",
      organisationCodec.encodeRequest(
        create(GetOrganisationRequestSchema, {
          authentication: authority(workspace),
          organisationId: workspace.organisationId,
        }),
      ),
    ),
  );
  if (!result.organisation) throw new Error("REPORTING_ACCEPTANCE_ORGANISATION_MISSING");
  return result.organisation;
}

async function readBas(
  page: Page,
  workspace: AuthenticatedAccountingWorkspace,
): Promise<BasWorkpaper> {
  const result = basCodec.decodeResponse(
    await invokeConveyorFrame(
      page,
      "reporting",
      "getCurrentBasDraft",
      basCodec.encodeRequest(
        create(GetCurrentBasDraftRequestSchema, {
          authentication: authority(workspace),
          organisationId: workspace.organisationId,
        }),
      ),
    ),
  );
  if (!result.workpaper) throw new Error("REPORTING_ACCEPTANCE_BAS_MISSING");
  return result.workpaper;
}

async function saveExport(
  harness: ElectronHarness,
  surface: Locator,
  format: "CSV" | "PDF",
  filePath: string,
) {
  await harness.application.evaluate(
    ({ dialog }, selection) => {
      dialog.showSaveDialog = async (...args: unknown[]) => {
        const options = args.at(-1) as {
          defaultPath?: string;
          filters?: { extensions: string[] }[];
        };
        if (
          !options?.defaultPath?.includes("2026-10-01") ||
          !options.defaultPath.includes("2026-12-31") ||
          !options.defaultPath.endsWith(`.${selection.extension}`) ||
          !options.filters?.some((filter) => filter.extensions.includes(selection.extension))
        ) {
          throw new Error("REPORTING_ACCEPTANCE_SAVE_DIALOG_INVALID");
        }
        return { canceled: false, filePath: selection.filePath };
      };
    },
    { extension: format.toLowerCase(), filePath },
  );
  const button = surface.getByRole("button", { name: `Export ${format}`, exact: true });
  await expect(button).toBeEnabled();
  await button.click();
  await expect
    .poll(() =>
      stat(filePath).then(
        (state) => state.size > 0,
        () => false,
      ),
    )
    .toBe(true);
  await expect(button).toBeEnabled();
  if (process.platform !== "win32") expect((await stat(filePath)).mode & 0o777).toBe(0o600);
}

async function expectMissingFile(file: string) {
  await expect(readFile(file)).rejects.toMatchObject({ code: "ENOENT" });
}

async function expectIncomeDates(page: Page) {
  await expect(page.getByLabel("Income period start", { exact: true })).toHaveValue(periodStart);
  await expect(page.getByLabel("Income period end", { exact: true })).toHaveValue(periodEnd);
  await expect(page.getByLabel("Balances as of", { exact: true })).toHaveValue(asOfDate);
}

async function unlockWorkspace(page: Page) {
  await page.getByLabel("Workspace passphrase").fill(CURRENT_WORKFLOW_PASSPHRASE);
  await page.getByLabel("Email or username").fill(CURRENT_WORKFLOW_USERNAME);
  await page.getByLabel("Administrator password").fill(CURRENT_WORKFLOW_ADMIN_PASSWORD);
  await page.getByRole("button", { name: "Unlock workspace", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
}

function csvMetric(csv: ParsedReportCsv, metric: string, kind?: string) {
  const rows = csv.rows.filter((row) => row.metric === metric && (!kind || row.kind === kind));
  expect(rows, `one CSV metric ${kind ?? ""}/${metric}`).toHaveLength(1);
  return rows[0];
}

function assertIncomeCsv(
  csv: ParsedReportCsv,
  income: BusinessIncomeSummary,
  organisation: Organisation,
) {
  expect(csv.header).toEqual(["metric", "value", "currency", "context"]);
  for (const [metric, amount] of [
    ["business_income", income.businessIncome?.minorUnits],
    ["invoice_income", income.invoiceIncome?.minorUnits],
    ["payments_received", income.paymentsReceived?.minorUnits],
    ["outstanding", income.outstanding?.minorUnits],
    ["invoice_gst", income.invoiceGst?.minorUnits],
  ] as const)
    expect(csvMetric(csv, metric)).toMatchObject({ value: exactDecimal(amount), currency: "AUD" });
  expect(csvMetric(csv, "financial_revision")?.value).toBe(income.financialRevision.toString());
  expect(csvMetric(csv, "organisation_id")?.value).toBe(organisation.id);
  expect(csvMetric(csv, "organisation_version")?.value).toBe(organisation.version.toString());
  expect(csvMetric(csv, "period_start")?.value).toBe(periodStart);
  expect(csvMetric(csv, "period_end")?.value).toBe(periodEnd);
  expect(csvMetric(csv, "as_of_date")?.value).toBe(asOfDate);
  expect(csvMetric(csv, "gst_basis")?.value).toBe(income.gstBasis === 1 ? "Cash" : "Non-cash");
  expect(csvMetric(csv, "organisation_legal_name")?.value).toBe(organisation.legalName);
  expect(csvMetric(csv, "organisation_display_name")?.value).toBe(organisation.displayName);
  expect(csvMetric(csv, "organisation_abn")?.value).toBe(organisation.abn);
  expect(csvMetric(csv, "issued_count")?.value).toBe(income.issuedCount.toString());
  expect(csvMetric(csv, "unposted_issued_count")?.value).toBe(
    income.unpostedIssuedCount.toString(),
  );
  expect(csvMetric(csv, "unposted_income")?.value).toBe(
    exactDecimal(income.unpostedIncome?.minorUnits),
  );
  expect(Number.isFinite(Date.parse(csvMetric(csv, "acquired_at")?.value ?? ""))).toBe(true);
}

function assertIncomePdf(text: string, income: BusinessIncomeSummary, organisation: Organisation) {
  const normalized = text.replace(/\s+/g, " ");
  expect(normalized).toContain("Income summary");
  expect(normalized).toContain(organisation.displayName);
  expect(normalized).toContain(organisation.abn);
  for (const date of [periodStart, periodEnd, asOfDate]) expect(normalized).toContain(date);
  for (const amount of [
    income.businessIncome,
    income.invoiceIncome,
    income.paymentsReceived,
    income.outstanding,
    income.invoiceGst,
  ])
    expect(normalized).toContain(exactDecimal(amount?.minorUnits));
  expect(normalized).toMatch(income.gstBasis === 1 ? /cash/i : /non-cash/i);
  expect(normalized).toContain(income.financialRevision.toString());
}

function assertBasCsv(csv: ParsedReportCsv, bas: BasWorkpaper, organisation: Organisation) {
  expect(csv.header).toEqual([
    "kind",
    "metric",
    "value",
    "currency",
    "context",
    "document_id",
    "invoice_id",
    "payment_id",
    "party",
    "invoice_number",
    "source_date",
    "gross",
    "gst",
    "source_revision",
    "source_hash",
  ]);
  for (const [metric, amount] of [
    ["sales_g1", bas.salesG1?.minorUnits],
    ["gst_on_sales_1a", bas.gstOnSales1a?.minorUnits],
    ["gst_credits_1b", bas.gstCredits1b?.minorUnits],
    ["net_gst_payable", bas.netGstPayable?.minorUnits],
  ] as const)
    expect(csvMetric(csv, metric, "totals")).toMatchObject({
      value: exactDecimal(amount),
      currency: "AUD",
    });
  expect(csvMetric(csv, "organisation_id", "metadata")?.value).toBe(organisation.id);
  expect(csvMetric(csv, "organisation_version", "metadata")?.value).toBe(
    organisation.version.toString(),
  );
  expect(csvMetric(csv, "workpaper_id", "metadata")?.value).toBe(bas.id);
  expect(csvMetric(csv, "workpaper_version", "metadata")?.value).toBe(bas.version.toString());
  expect(csvMetric(csv, "period_start", "metadata")?.value).toBe(periodStart);
  expect(csvMetric(csv, "period_end", "metadata")?.value).toBe(periodEnd);
  expect(csvMetric(csv, "gst_basis", "metadata")?.value).toBe(
    bas.gstBasis === 1 ? "Cash" : "Non-cash",
  );
  expect(csvMetric(csv, "sources_changed", "metadata")?.value).toBe(String(bas.sourcesChanged));
  expect(csvMetric(csv, "status", "metadata")?.value).toBe("Draft — not lodged");
  expect(csvMetric(csv, "source_fingerprint", "metadata")?.value).toBe(
    Buffer.from(bas.sourceFingerprint).toString("hex"),
  );
  expect(csvMetric(csv, "excluded_cash_purchase_count", "metadata")?.value).toBe(
    bas.excludedCashPurchaseCount.toString(),
  );
  expect(Number.isFinite(Date.parse(csvMetric(csv, "acquired_at", "metadata")?.value ?? ""))).toBe(
    true,
  );
  assertCreatedAt(csvMetric(csv, "created_at", "metadata")?.value ?? "", bas);
  const sales = csv.rows.filter((row) => row.kind === "sales");
  expect(sales).toHaveLength(bas.salesSources.length);
  for (const [index, source] of bas.salesSources.entries())
    expect(sales[index]).toMatchObject({
      invoice_id: source.invoiceId,
      invoice_number: source.invoiceNumber,
      party: source.customerName,
      payment_id: source.paymentId ?? "",
      source_date: isoDate(source.taxDate),
      gross: exactDecimal(source.gross?.minorUnits),
      gst: exactDecimal(source.gst?.minorUnits),
      source_revision: source.sourceRevision.toString(),
      source_hash: Buffer.from(source.sourceHash).toString("hex"),
    });
  const purchases = csv.rows.filter((row) => row.kind === "purchases");
  expect(purchases).toHaveLength(bas.sources.length);
  for (const [index, source] of bas.sources.entries())
    expect(purchases[index]).toMatchObject({
      document_id: source.documentId,
      invoice_number: source.invoiceNumber,
      party: `'${source.supplierName}`,
      source_date: isoDate(source.documentDate),
      gross: exactDecimal(source.gross?.minorUnits),
      gst: exactDecimal(source.gstCredit?.minorUnits),
      invoice_id: "",
      payment_id: "",
      source_revision: "",
      source_hash: "",
    });
  const warnings = csv.rows.filter((row) => row.kind === "warnings").map((row) => row.value);
  for (const warning of bas.reviewWarnings) expect(warnings).toContain(warning);
  expect(warnings).toHaveLength(bas.reviewWarnings.length + (bas.sourcesChanged ? 1 : 0));
  if (bas.sourcesChanged) expect(warnings.join(" ")).toMatch(/source changes.*saved workpaper/i);
}

function assertBasPdf(text: string, bas: BasWorkpaper, organisation: Organisation) {
  const normalized = text.replace(/\s+/g, " ");
  const tokens = text.replace(/\s+/g, "");
  expect(normalized).toContain("Draft — not lodged");
  expect(normalized).toContain(organisation.displayName);
  expect(normalized).toContain(organisation.abn);
  expect(normalized).toContain(bas.id);
  const created = normalized.match(
    /\bcreated at\s+(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z)\b/,
  );
  assertCreatedAt(created?.[1] ?? "", bas);
  for (const date of [periodStart, periodEnd]) expect(normalized).toContain(date);
  for (const amount of [bas.salesG1, bas.gstOnSales1a, bas.gstCredits1b, bas.netGstPayable])
    expect(normalized).toContain(exactDecimal(amount?.minorUnits));
  for (const source of bas.salesSources) {
    expect(normalized).toContain(source.invoiceNumber);
    expect(normalized).toContain(source.customerName);
    expect(normalized).toContain(isoDate(source.taxDate));
    expect(normalized).toContain(exactDecimal(source.gross?.minorUnits));
    expect(normalized).toContain(exactDecimal(source.gst?.minorUnits));
    expect(tokens).toContain(`Invoice${source.invoiceId}`);
    if (source.paymentId) expect(tokens).toContain(`Receipt${source.paymentId}`);
    expect(tokens).toContain(`Revision${source.sourceRevision}`);
    expect(tokens).toContain(`Hash${Buffer.from(source.sourceHash).toString("hex")}`);
  }
  for (const source of bas.sources) {
    expect(normalized).toContain(source.invoiceNumber);
    expect(normalized).toContain(source.supplierName);
    expect(normalized).toContain(isoDate(source.documentDate));
    expect(normalized).toContain(exactDecimal(source.gross?.minorUnits));
    expect(normalized).toContain(exactDecimal(source.gstCredit?.minorUnits));
    expect(tokens).toContain(`Document${source.documentId}`);
  }
  for (const warning of bas.reviewWarnings) expect(normalized).toContain(warning);
  if (bas.sourcesChanged) expect(normalized).toMatch(/source changes.*saved workpaper/i);
}

function assertCreatedAt(value: string, bas: BasWorkpaper) {
  if (!bas.createdAt) throw new Error("REPORTING_ACCEPTANCE_CREATED_AT_MISSING");
  const timestamp = value.match(/^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?Z$/);
  if (!timestamp) throw new Error("REPORTING_ACCEPTANCE_CREATED_AT_INVALID");
  expect(BigInt(Date.parse(`${timestamp[1]}Z`) / 1000)).toBe(bas.createdAt.seconds);
  expect(Number((timestamp[2] ?? "").padEnd(9, "0"))).toBe(bas.createdAt.nanos);
}

function isoDate(
  value: { readonly year: number; readonly month: number; readonly day: number } | undefined,
) {
  if (!value) throw new Error("REPORTING_ACCEPTANCE_DATE_MISSING");
  return `${value.year}-${String(value.month).padStart(2, "0")}-${String(value.day).padStart(2, "0")}`;
}

async function retainEvidence(
  harness: ElectronHarness,
  testInfo: TestInfo,
  directory: string,
  files: readonly string[],
  records: Record<string, string>,
) {
  const hashes = [];
  for (const file of files) {
    hashes.push({ file: path.basename(file), sha256: await reportArtifactHash(file) });
    await testInfo.attach(path.basename(file), {
      path: file,
      contentType: file.endsWith(".pdf") ? "application/pdf" : "text/csv",
    });
  }
  const evidence = path.join(directory, "evidence.json");
  const buildManifest = JSON.parse(
    await readFile(new URL("../../resources/build/build-manifest.json", import.meta.url), "utf8"),
  ) as { source_revision?: string; source_dirty?: boolean };
  expect(buildManifest.source_revision).toBe(harness.packagedLayout.sourceRevision);
  await writeFile(
    evidence,
    JSON.stringify(
      {
        sourceRevision: harness.packagedLayout.sourceRevision,
        sourceDirty: buildManifest.source_dirty,
        appSha256: harness.packagedLayout.appSha256,
        coreSha256: harness.packagedLayout.coreSha256,
        records,
        artifacts: hashes,
      },
      null,
      2,
    ),
  );
  await testInfo.attach("reporting-export-evidence", {
    path: evidence,
    contentType: "application/json",
  });
}
