import { createHash } from "node:crypto";
import { chmod, mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { create, type DescMessage, type MessageShape } from "@bufbuild/protobuf";
import {
  AccountType,
  CreateAccountRequestSchema,
  CreateAccountResponseSchema,
  GetJournalRequestSchema,
  GetJournalResponseSchema,
  GetTrialBalanceRequestSchema,
  GetTrialBalanceResponseSchema,
  JournalSource,
  JournalState,
  ListAccountsRequestSchema,
  ListAccountsResponseSchema,
  ListJournalsRequestSchema,
  ListJournalsResponseSchema,
  NormalBalance,
  PostManualJournalRequestSchema,
  PostManualJournalResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  BankStatementLineInputSchema,
  BankStatementLineStatus,
  CompleteBankReconciliationRequestSchema,
  CompleteBankReconciliationResponseSchema,
  GetBankingSummaryRequestSchema,
  GetBankingSummaryResponseSchema,
  ImportBankStatementRequestSchema,
  ImportBankStatementResponseSchema,
  ListBankStatementLinesRequestSchema,
  ListBankStatementLinesResponseSchema,
  MatchBankStatementLineRequestSchema,
  MatchBankStatementLineResponseSchema,
} from "@tammy/connect-client/tammy/v1/banking_pb.js";
import {
  AuthenticationContextSchema,
  CivilDateSchema,
  CommandContextSchema,
  MoneySchema,
  PageRequestSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  DocumentCandidateSchema,
  DocumentStatus,
  GetDocumentRequestSchema,
  GetDocumentResponseSchema,
  IngestDocumentRequestSchema,
  IngestDocumentResponseSchema,
  ListDocumentsRequestSchema,
  ListDocumentsResponseSchema,
  SaveDocumentReviewRequestSchema,
  SaveDocumentReviewResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import {
  BasAttentionStatus,
  GetAttentionSummaryRequestSchema,
  GetAttentionSummaryResponseSchema,
  ReportingPeriodSchema,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";
import {
  BasWorkpaperStatus,
  CreateBasDraftRequestSchema,
  CreateBasDraftResponseSchema,
  GetCurrentBasDraftRequestSchema,
  GetCurrentBasDraftResponseSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { pdfStreams } from "../../../../packages/invoices/test-support/pdf-text.ts";
import {
  normalizeAccessibilitySnapshot,
  validateScreenshotFixture,
} from "../../../../scripts/check-app-store-screenshots.mjs";
import screenshotFixture from "../../release/macos/screenshots/fixture.json" with { type: "json" };
import { createProtoMethodCodec, type ProtoMethodCodec } from "../../src/shared/proto-ipc";
import { expect, test } from "./fixtures";
import { type ConveyorModule, invokeConveyorFrame } from "./support/conveyor";
import {
  addReviewedInvoiceExpense,
  authenticatedWorkspace as authenticatedCurrentWorkflowWorkspace,
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
import { openSavedBasWorkpaperPanel } from "./support/saved-bas-workpaper-panel";

const UUID_V7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const USERNAME = "admin@tammy.local";
const WORKSPACE_PASSPHRASE = "workspace-passphrase-long-enough";
const ADMINISTRATOR_PASSWORD = "administrator-password-long-enough";
const PERIOD_START = create(CivilDateSchema, { year: 2024, month: 4, day: 1 });
const PERIOD_END = create(CivilDateSchema, { year: 2024, month: 6, day: 30 });
const POSTING_DATE = create(CivilDateSchema, { year: 2024, month: 5, day: 12 });
const CASE_IDS = {
  expenseAccount: "01900000-0000-7000-8000-000000000001",
  equityAccount: "01900000-0000-7000-8000-000000000002",
  journal: "01900000-0000-7000-8000-000000000003",
  journalDebit: "01900000-0000-7000-8000-000000000004",
  journalCredit: "01900000-0000-7000-8000-000000000005",
  bankImport: "01900000-0000-7000-8000-000000000006",
  bankMatch: "01900000-0000-7000-8000-000000000007",
  bankReconciliation: "01900000-0000-7000-8000-000000000008",
  document: "01900000-0000-7000-8000-000000000009",
  documentReview: "01900000-0000-7000-8000-00000000000a",
  bas: "01900000-0000-7000-8000-00000000000b",
} as const;

const createAccountCodec = createProtoMethodCodec({
  input: CreateAccountRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 32_768,
  output: CreateAccountResponseSchema,
});
const listAccountsCodec = createProtoMethodCodec({
  input: ListAccountsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 131_072,
  output: ListAccountsResponseSchema,
});
const postManualJournalCodec = createProtoMethodCodec({
  input: PostManualJournalRequestSchema,
  maximumRequestBytes: 131_072,
  maximumResponseBytes: 262_144,
  output: PostManualJournalResponseSchema,
});
const getJournalCodec = createProtoMethodCodec({
  input: GetJournalRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 262_144,
  output: GetJournalResponseSchema,
});
const listJournalsCodec = createProtoMethodCodec({
  input: ListJournalsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: ListJournalsResponseSchema,
});
const getTrialBalanceCodec = createProtoMethodCodec({
  input: GetTrialBalanceRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 524_288,
  output: GetTrialBalanceResponseSchema,
});
const importBankStatementCodec = createProtoMethodCodec({
  input: ImportBankStatementRequestSchema,
  maximumRequestBytes: 262_144,
  maximumResponseBytes: 32_768,
  output: ImportBankStatementResponseSchema,
});
const listBankStatementLinesCodec = createProtoMethodCodec({
  input: ListBankStatementLinesRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: ListBankStatementLinesResponseSchema,
});
const matchBankStatementLineCodec = createProtoMethodCodec({
  input: MatchBankStatementLineRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 32_768,
  output: MatchBankStatementLineResponseSchema,
});
const completeBankReconciliationCodec = createProtoMethodCodec({
  input: CompleteBankReconciliationRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
  output: CompleteBankReconciliationResponseSchema,
});
const getBankingSummaryCodec = createProtoMethodCodec({
  input: GetBankingSummaryRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
  output: GetBankingSummaryResponseSchema,
});
const ingestDocumentCodec = createProtoMethodCodec({
  input: IngestDocumentRequestSchema,
  maximumRequestBytes: 11 * 1024 * 1024,
  maximumResponseBytes: 2 * 1024 * 1024,
  output: IngestDocumentResponseSchema,
});
const listDocumentsCodec = createProtoMethodCodec({
  input: ListDocumentsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 4 * 1024 * 1024,
  output: ListDocumentsResponseSchema,
});
const getDocumentCodec = createProtoMethodCodec({
  input: GetDocumentRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 2 * 1024 * 1024,
  output: GetDocumentResponseSchema,
});
const saveDocumentReviewCodec = createProtoMethodCodec({
  input: SaveDocumentReviewRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 2 * 1024 * 1024,
  output: SaveDocumentReviewResponseSchema,
});
const createBasDraftCodec = createProtoMethodCodec({
  input: CreateBasDraftRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: CreateBasDraftResponseSchema,
});
const getCurrentBasDraftCodec = createProtoMethodCodec({
  input: GetCurrentBasDraftRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: GetCurrentBasDraftResponseSchema,
});
const attentionCodec = createProtoMethodCodec({
  input: GetAttentionSummaryRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 65_536,
  output: GetAttentionSummaryResponseSchema,
});

test("covers workspace setup, recovery, accounting, and interaction journeys through the packaged app", async ({
  electronHarness,
}) => {
  test.info().annotations.push({
    type: "feedback-source",
    description: electronHarness.packagedLayout.sourceRevision,
  });
  let page = electronHarness.page;
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.evaluate(() => {
    window.history.replaceState(null, "", "/setup/workspace");
    window.dispatchEvent(new PopStateEvent("popstate"));
  });
  await expect(page).toHaveURL(/\/setup\/workspace$/);
  await expect(page.getByRole("heading", { name: "Create your local workspace" })).toBeVisible();

  await captureJourney(page, "01-workspace");

  await page.getByLabel("Your name").fill("Tammy Admin");
  await page.getByLabel("Email or username").fill(USERNAME);
  await page.getByLabel("Business legal name").fill("Tammy Business Pty Ltd");
  await page.getByLabel("Business display name").fill("Tammy Business");
  await page.getByLabel("ABN").fill("51824753556");
  await page.getByLabel("Workspace passphrase", { exact: true }).fill(WORKSPACE_PASSPHRASE);
  await page.getByLabel("Confirm workspace passphrase", { exact: true }).fill(WORKSPACE_PASSPHRASE);
  await page.getByLabel("Administrator password", { exact: true }).fill(ADMINISTRATOR_PASSWORD);
  await page
    .getByLabel("Confirm administrator password", { exact: true })
    .fill(ADMINISTRATOR_PASSWORD);
  await page.getByRole("button", { name: "Create local workspace" }).click();
  await expect(page.getByRole("heading", { name: "Save your recovery code" })).toBeVisible();
  await expect(page.getByText("One-time recovery code")).toBeVisible();
  await captureJourney(page, "02-recovery", true);
  await page.getByRole("button", { name: "I saved my recovery code" }).click();
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
  await expect(
    page.getByRole("navigation", { name: "Primary" }).getByRole("link", { name: "General ledger" }),
  ).toHaveCount(0);

  await assertRoute(page, "Documents", "/documents", "Documents");
  await expect(page.getByText("Your next clear step", { exact: true })).toBeVisible();
  await captureJourney(page, "28-documents-empty");
  await assertRoute(page, "Banking", "/banking", "Banking");
  await expect(page.getByText("A calm start to reconciliation", { exact: true })).toBeVisible();
  await captureJourney(page, "29-banking-empty");
  const createdWorkspace = await authenticatedWorkspace(page);
  expect(createdWorkspace.workspaceId).toMatch(UUID_V7);
  expect(createdWorkspace.userId).toMatch(UUID_V7);
  expect(createdWorkspace.sessionId).toMatch(UUID_V7);
  expect(createdWorkspace.organisationId).toMatch(UUID_V7);

  page = await electronHarness.restart("accounting-workflow-restart");
  await page.evaluate(() => {
    window.history.replaceState(null, "", "/unlock");
    window.dispatchEvent(new PopStateEvent("popstate"));
  });
  await expect(page).toHaveURL(/\/unlock$/);
  await expect(page.getByRole("heading", { name: "Unlock your workspace" })).toBeVisible();
  await page.setViewportSize({ width: 1440, height: 1000 });
  await captureJourney(page, "03-unlock");
  await page.getByLabel("Workspace passphrase", { exact: true }).fill(WORKSPACE_PASSPHRASE);
  await page.getByLabel("Email or username").fill(USERNAME);
  await page.getByLabel("Administrator password", { exact: true }).fill(ADMINISTRATOR_PASSWORD);
  await page.getByRole("button", { name: "Unlock workspace" }).click();
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();

  const workspace = await authenticatedWorkspace(page);
  expect(workspace.workspaceId).toBe(createdWorkspace.workspaceId);
  expect(workspace.organisationId).toBe(createdWorkspace.organisationId);
  expect(workspace.userId).toBe(createdWorkspace.userId);
  expect(workspace.sessionId).not.toBe(createdWorkspace.sessionId);
  const authentication = create(AuthenticationContextSchema, {
    actorUserId: workspace.userId,
    sessionId: workspace.sessionId,
  });
  const organisationId = workspace.organisationId;

  const expense = await invokeProto(
    page,
    "createAccount",
    createAccountCodec,
    create(CreateAccountRequestSchema, {
      commandContext: command(authentication, CASE_IDS.expenseAccount),
      organisationId,
      code: "6100",
      name: "Office expenses",
      type: AccountType.EXPENSE,
      normalBalance: NormalBalance.DEBIT,
      reportClassification: "profit_loss.manual",
      cashFlowClassification: "noncash",
    }),
  );
  expect(expense.account).toMatchObject({
    organisationId,
    version: 1n,
    code: "6100",
    name: "Office expenses",
  });
  expect(expense.account?.id).toMatch(UUID_V7);
  const expenseAccount = expense.account;
  if (!expenseAccount) throw new Error("EXPENSE_ACCOUNT_MISSING");

  const equity = await invokeProto(
    page,
    "createAccount",
    createAccountCodec,
    create(CreateAccountRequestSchema, {
      commandContext: command(authentication, CASE_IDS.equityAccount),
      organisationId,
      code: "3100",
      name: "Owner contributions",
      type: AccountType.EQUITY,
      normalBalance: NormalBalance.CREDIT,
      reportClassification: "balance_sheet.manual",
      cashFlowClassification: "noncash",
    }),
  );
  expect(equity.account).toMatchObject({
    organisationId,
    version: 1n,
    code: "3100",
    name: "Owner contributions",
  });
  expect(equity.account?.id).toMatch(UUID_V7);
  const equityAccount = equity.account;
  if (!equityAccount) throw new Error("EQUITY_ACCOUNT_MISSING");

  const accountPage = await invokeProto(
    page,
    "listAccounts",
    listAccountsCodec,
    create(ListAccountsRequestSchema, {
      authentication,
      organisationId,
      page: create(PageRequestSchema, { pageSize: 50 }),
    }),
  );
  expect(accountPage.accounts).toHaveLength(14);
  expect(accountPage.page?.returnedCount).toBe(14);
  expect(accountPage.accounts.map((account) => account.code)).toEqual(
    [...accountPage.accounts.map((account) => account.code)].sort(),
  );

  const posted = await invokeProto(
    page,
    "postManualJournal",
    postManualJournalCodec,
    create(PostManualJournalRequestSchema, {
      commandContext: command(authentication, CASE_IDS.journal),
      organisationId,
      postingDate: POSTING_DATE,
      memo: "Office supplies paid personally",
      lines: [
        {
          clientLineId: CASE_IDS.journalDebit,
          accountId: expenseAccount.id,
          debit: aud(31_900n),
          credit: aud(0n),
          description: "Office supplies",
        },
        {
          clientLineId: CASE_IDS.journalCredit,
          accountId: equityAccount.id,
          debit: aud(0n),
          credit: aud(31_900n),
          description: "Owner contribution",
        },
      ],
    }),
  );
  expect(posted.journal).toMatchObject({
    organisationId,
    version: 1n,
    state: JournalState.POSTED,
    source: JournalSource.MANUAL,
    memo: "Office supplies paid personally",
  });
  expect(posted.journal?.id).toMatch(UUID_V7);
  expect(posted.journal?.totalDebits?.minorUnits).toBe(31_900n);
  expect(posted.journal?.totalCredits?.minorUnits).toBe(31_900n);
  const postedJournal = posted.journal;
  if (!postedJournal) throw new Error("POSTED_JOURNAL_MISSING");

  const storedJournal = await invokeProto(
    page,
    "getJournal",
    getJournalCodec,
    create(GetJournalRequestSchema, {
      authentication,
      journalId: postedJournal.id,
    }),
  );
  expect(storedJournal.journal?.id).toBe(postedJournal.id);
  expect(storedJournal.journal?.lines).toHaveLength(2);

  const journalPage = await invokeProto(
    page,
    "listJournals",
    listJournalsCodec,
    create(ListJournalsRequestSchema, {
      authentication,
      organisationId,
      page: create(PageRequestSchema, { pageSize: 50 }),
    }),
  );
  expect(journalPage.journals.map((journal) => journal.id)).toEqual([postedJournal.id]);
  expect(journalPage.page?.returnedCount).toBe(1);

  const trialBalance = await invokeProto(
    page,
    "getTrialBalance",
    getTrialBalanceCodec,
    create(GetTrialBalanceRequestSchema, {
      authentication,
      organisationId,
      asOfDate: POSTING_DATE,
    }),
  );
  expect(trialBalance.lines).toHaveLength(2);
  expect(trialBalance.totalDebits?.minorUnits).toBe(31_900n);
  expect(trialBalance.totalCredits?.minorUnits).toBe(31_900n);
  expect(trialBalance.financialRevision).toBe(1n);

  const statement = await invokeProto(
    page,
    "importBankStatement",
    importBankStatementCodec,
    create(ImportBankStatementRequestSchema, {
      commandContext: command(authentication, CASE_IDS.bankImport),
      organisationId,
      openingBalance: aud(100_000n),
      lines: [
        create(BankStatementLineInputSchema, {
          transactionDate: POSTING_DATE,
          description: "Officeworks INV-029847",
          amount: aud(-31_900n),
        }),
      ],
    }),
  );
  expect(statement.statementImport?.id).toMatch(UUID_V7);
  expect(statement.statementImport).toMatchObject({ organisationId, lineCount: 1 });
  expect(statement.statementImport?.openingBalance?.minorUnits).toBe(100_000n);
  expect(statement.statementImport?.closingBalance?.minorUnits).toBe(68_100n);

  const importedLines = await invokeProto(
    page,
    "listBankStatementLines",
    listBankStatementLinesCodec,
    create(ListBankStatementLinesRequestSchema, {
      authentication,
      organisationId,
      page: create(PageRequestSchema, { pageSize: 50 }),
    }),
  );
  expect(importedLines.lines).toHaveLength(1);
  expect(importedLines.page?.returnedCount).toBe(1);
  expect(importedLines.lines[0]).toMatchObject({
    version: 1n,
    status: BankStatementLineStatus.UNMATCHED,
    description: "Officeworks INV-029847",
  });
  expect(importedLines.lines[0]?.id).toMatch(UUID_V7);
  const importedLine = importedLines.lines[0];
  if (!importedLine) throw new Error("IMPORTED_BANK_LINE_MISSING");

  const matched = await invokeProto(
    page,
    "matchBankStatementLine",
    matchBankStatementLineCodec,
    create(MatchBankStatementLineRequestSchema, {
      commandContext: command(authentication, CASE_IDS.bankMatch),
      lineId: importedLine.id,
      expectedVersion: importedLine.version,
      matchReference: "Reviewed accounting source",
    }),
  );
  expect(matched.line).toMatchObject({
    version: 2n,
    status: BankStatementLineStatus.MATCHED,
    matchReference: "Reviewed accounting source",
  });

  const beforeReconciliation = await invokeProto(
    page,
    "getBankingSummary",
    getBankingSummaryCodec,
    create(GetBankingSummaryRequestSchema, { authentication, organisationId }),
  );
  expect(beforeReconciliation).toMatchObject({
    importedLineCount: 1,
    unmatchedLineCount: 0,
    unreconciledLineCount: 1,
  });
  expect(beforeReconciliation.latestClosingBalance?.minorUnits).toBe(68_100n);

  const reconciliation = await invokeProto(
    page,
    "completeBankReconciliation",
    completeBankReconciliationCodec,
    create(CompleteBankReconciliationRequestSchema, {
      commandContext: command(authentication, CASE_IDS.bankReconciliation),
      organisationId,
    }),
  );
  expect(reconciliation.reconciledLineCount).toBe(1);
  expect(reconciliation.closingBalance?.minorUnits).toBe(68_100n);

  const reconciledLines = await invokeProto(
    page,
    "listBankStatementLines",
    listBankStatementLinesCodec,
    create(ListBankStatementLinesRequestSchema, {
      authentication,
      organisationId,
      page: create(PageRequestSchema, { pageSize: 50 }),
    }),
  );
  expect(reconciledLines.lines[0]).toMatchObject({
    version: 3n,
    status: BankStatementLineStatus.RECONCILED,
  });
  const bankingSummary = await invokeProto(
    page,
    "getBankingSummary",
    getBankingSummaryCodec,
    create(GetBankingSummaryRequestSchema, { authentication, organisationId }),
  );
  expect(bankingSummary).toMatchObject({
    importedLineCount: 1,
    unmatchedLineCount: 0,
    unreconciledLineCount: 0,
  });
  expect(bankingSummary.latestClosingBalance?.minorUnits).toBe(68_100n);

  const invoiceBytes = new TextEncoder().encode("%PDF-1.4\nTammy native-text fixture\n%%EOF");
  const candidate = create(DocumentCandidateSchema, {
    supplierName: "Officeworks Ltd",
    invoiceNumber: "INV-029847",
    documentDate: POSTING_DATE,
    subtotal: aud(29_000n),
    gst: aud(2_900n),
    total: aud(31_900n),
  });
  const retained = await invokeProto(
    page,
    "ingestDocument",
    ingestDocumentCodec,
    create(IngestDocumentRequestSchema, {
      commandContext: command(authentication, CASE_IDS.document),
      organisationId,
      sourceDisplayName: "officeworks-invoice.pdf",
      mimeType: "application/pdf",
      original: invoiceBytes,
      extractedText: "Officeworks Ltd Invoice INV-029847 Subtotal $290.00 GST $29.00 Total $319.00",
      candidate,
    }),
  );
  expect(retained.document?.id).toMatch(UUID_V7);
  expect(retained.document).toMatchObject({
    organisationId,
    version: 1n,
    status: DocumentStatus.NEEDS_REVIEW,
    sourceDisplayName: "officeworks-invoice.pdf",
    byteLength: BigInt(invoiceBytes.byteLength),
  });
  expect(retained.document?.sha256).toHaveLength(32);
  const retainedDocument = retained.document;
  if (!retainedDocument) throw new Error("RETAINED_DOCUMENT_MISSING");

  const documentPage = await invokeProto(
    page,
    "listDocuments",
    listDocumentsCodec,
    create(ListDocumentsRequestSchema, {
      authentication,
      organisationId,
      page: create(PageRequestSchema, { pageSize: 50 }),
    }),
  );
  expect(documentPage.documents.map((document) => document.id)).toEqual([retainedDocument.id]);
  expect(documentPage.page?.returnedCount).toBe(1);

  const reviewed = await invokeProto(
    page,
    "saveDocumentReview",
    saveDocumentReviewCodec,
    create(SaveDocumentReviewRequestSchema, {
      commandContext: command(authentication, CASE_IDS.documentReview),
      documentId: retainedDocument.id,
      expectedVersion: retainedDocument.version,
      candidate,
    }),
  );
  expect(reviewed.document).toMatchObject({
    version: 2n,
    status: DocumentStatus.REVIEWED,
  });
  expect(reviewed.document?.reviewedAt).toBeDefined();

  const storedDocument = await invokeProto(
    page,
    "getDocument",
    getDocumentCodec,
    create(GetDocumentRequestSchema, {
      authentication,
      documentId: retainedDocument.id,
    }),
  );
  expect(storedDocument.document?.id).toBe(retainedDocument.id);
  expect(storedDocument.document?.candidate?.invoiceNumber).toBe("INV-029847");
  expect(storedDocument.document?.status).toBe(DocumentStatus.REVIEWED);

  const readAttention = () =>
    invokeProto(
      page,
      "getAttentionSummary",
      attentionCodec,
      create(GetAttentionSummaryRequestSchema, {
        authentication,
        organisationId,
        asOfDate: PERIOD_END,
        reportingPeriod: create(ReportingPeriodSchema, {
          startDate: PERIOD_START,
          endDate: PERIOD_END,
        }),
      }),
    );
  const revisionsBeforeBasCreate = await readAttention();
  expect(revisionsBeforeBasCreate.revisions).toMatchObject({
    financialRevision: 6n,
    ledgerRevision: 1n,
    bankingRevision: 3n,
    taxSourceRevision: 2n,
  });

  const createdBas = await invokeProto(
    page,
    "createBasDraft",
    createBasDraftCodec,
    create(CreateBasDraftRequestSchema, {
      commandContext: command(authentication, CASE_IDS.bas),
      organisationId,
      periodStart: PERIOD_START,
      periodEnd: PERIOD_END,
    }),
  );
  expect(createdBas.workpaper?.id).toMatch(UUID_V7);
  expect(createdBas.workpaper).toMatchObject({
    organisationId,
    version: 1n,
    status: BasWorkpaperStatus.DRAFT_NOT_LODGED,
  });
  expect(createdBas.workpaper?.salesG1?.minorUnits).toBe(0n);
  expect(createdBas.workpaper?.gstOnSales1a?.minorUnits).toBe(0n);
  expect(createdBas.workpaper?.gstCredits1b?.minorUnits).toBe(0n);
  expect(createdBas.workpaper?.netGstPayable?.minorUnits).toBe(0n);
  expect(createdBas.workpaper?.sources).toHaveLength(0);
  expect(createdBas.workpaper?.purchaseSources).toHaveLength(0);
  expect(createdBas.workpaper?.purchaseReviews).toHaveLength(1);
  expect(createdBas.workpaper?.legacyCandidateGst?.minorUnits).toBe(2_900n);
  expect(createdBas.workpaper?.purchaseReviews[0]?.legacySource?.source).toMatchObject({
    case: "document",
    value: { documentId: retainedDocument.id, reviewedVersion: 2n },
  });

  const currentBas = await invokeProto(
    page,
    "getCurrentBasDraft",
    getCurrentBasDraftCodec,
    create(GetCurrentBasDraftRequestSchema, { authentication, organisationId }),
  );
  expect(currentBas.workpaper?.id).toBe(createdBas.workpaper?.id);
  expect(currentBas.workpaper?.status).toBe(BasWorkpaperStatus.DRAFT_NOT_LODGED);

  const attention = await readAttention();
  expect(attention).toMatchObject({
    documentsNeedingReview: 0,
    documentsReviewedInPeriod: 1,
    bankingLinesNeedingReconciliation: 0,
    bankingLinesUnreconciledInPeriod: 0,
    currentDraftBasWorkpapers: 1,
    basStatus: BasAttentionStatus.ACTION_NEEDED,
  });
  expect(attention.revisions).toBeDefined();
  expect(attention.revisions?.financialRevision).toBe(6n);
  expect(attention.revisions?.ledgerRevision).toBe(1n);
  expect(attention.revisions?.bankingRevision).toBe(3n);
  expect(attention.revisions?.taxSourceRevision).toBe(2n);
  expect(attention.revisions).toEqual(revisionsBeforeBasCreate.revisions);
  expect(attention.attentionItems).toHaveLength(0);

  await assertRoute(page, "Documents", "/documents", "Documents");
  await expect(
    page.getByRole("heading", { name: "officeworks-invoice.pdf", exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Reviewed", { exact: true }).first()).toBeVisible();
  await captureJourney(page, "04-documents");
  await assertRoute(page, "Banking", "/banking", "Banking");
  await expect(page.getByText("$681.00", { exact: true })).toBeVisible();
  await captureJourney(page, "05-banking");
  await expect(
    page
      .getByRole("table", { name: "Statement lines" })
      .getByRole("cell", { name: "Reconciled", exact: true }),
  ).toBeVisible();
  await assertRoute(page, "Chart of accounts", "/accounting/chart", "Chart of accounts");
  await expect(page.getByText("Office expenses", { exact: true })).toBeVisible();
  await captureJourney(page, "06-chart");
  await assertRoute(page, "Journals", "/accounting/journals", "Journals");
  await expect(page.getByText("Office supplies paid personally", { exact: true })).toBeVisible();
  await captureJourney(page, "07-journals");
  await assertRoute(page, "Trial balance", "/accounting/trial-balance", "Trial balance");
  await expect(page.getByText("$319.00", { exact: true }).first()).toBeVisible();
  await captureJourney(page, "08-trial-balance");
  await assertRoute(page, "GST & BAS", "/gst-bas", "BAS preparation");
  await openSavedBasWorkpaperPanel(page);
  await expect(page.getByText("Draft — not lodged", { exact: true }).first()).toBeVisible();
  await expect(page.getByText("Unresolved legacy source", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Refresh exact history", exact: true }),
  ).toBeEnabled();
  await captureJourney(page, "09-gst-draft");
  await assertRoute(page, "Overview", "/overview", "Overview");
  await expect(
    page.getByText("Action needed · review this exact BAS preparation", { exact: true }),
  ).toBeVisible();

  await captureJourney(page, "10-overview");
  if (process.env.TAMMY_JOURNEY_CAPTURE !== "before") await interactionJourney(page);

  await page.setViewportSize({ width: 1440, height: 1000 });
  for (const [link, route, heading, capture] of [
    ["Invoices", "/invoices", "Invoices", "30-invoices"],
    ["Audit trail", "/audit", "Audit trail", "31-audit"],
    ["Settings", "/settings", "Settings", "32-settings"],
    ["Organisation", "/settings/organisation", "Organisation", "33-organisation"],
    ["SBR readiness", "/settings/sbr", "SBR readiness", "34-sbr-readiness"],
  ] as const) {
    await assertRoute(page, link, route, heading);
    if (route === "/invoices")
      await expect(page.getByLabel("Invoice number", { exact: true })).toBeVisible();
    if (route === "/audit")
      await expect(page.getByRole("main").getByRole("list").first()).toBeVisible();
    if (route === "/settings/organisation")
      await expect(page.getByText("51824753556", { exact: true })).toBeVisible();
    if (route === "/settings/sbr")
      await expect(
        page.getByRole("heading", { name: "Readiness unavailable", exact: true }),
      ).toBeVisible();
    await captureJourney(page, capture);
  }
  await page.setViewportSize({ width: 320, height: 900 });
  for (const [link, route, heading, capture, region] of [
    [
      "Chart of accounts",
      "/accounting/chart",
      "Chart of accounts",
      "35-320-chart",
      "Accounts table",
    ],
    ["Journals", "/accounting/journals", "Journals", "36-320-journals", "Journal history table"],
    [
      "Trial balance",
      "/accounting/trial-balance",
      "Trial balance",
      "37-320-trial-balance",
      "Account balances table",
    ],
  ] as const) {
    await assertRoute(page, link, route, heading);
    await expect(page.getByRole("table").first()).toBeVisible();
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
    ).toBe(true);
    expect(
      await page.getByRole("table").evaluateAll((tables) =>
        tables.every((table) => {
          for (let parent = table.parentElement; parent; parent = parent.parentElement) {
            if (getComputedStyle(parent).overflowX === "auto") return true;
          }
          return false;
        }),
      ),
    ).toBe(true);
    const scroller = page.getByRole("region", { name: region, exact: true });
    await expect(scroller).toHaveAttribute("tabindex", "0");
    await scroller.evaluate((element) => {
      element.scrollLeft = 0;
    });
    await scroller.focus();
    await page.keyboard.press("ArrowRight");
    await expect.poll(() => scroller.evaluate((element) => element.scrollLeft)).toBeGreaterThan(0);
    await scroller.evaluate((element) => {
      element.scrollLeft = 0;
    });
    await captureJourney(page, capture);
  }

  expect(electronHarness.consoleErrors).toEqual([]);
  expect(electronHarness.pageErrors).toEqual([]);
});

const METHOD_MODULES = {
  completeBankReconciliation: "banking",
  createAccount: "accounting",
  createBasDraft: "reporting",
  getAttentionSummary: "reporting",
  getBankingSummary: "banking",
  getCurrentBasDraft: "reporting",
  getDocument: "documents",
  getJournal: "accounting",
  getTrialBalance: "accounting",
  importBankStatement: "banking",
  ingestDocument: "documents",
  listAccounts: "accounting",
  listBankStatementLines: "banking",
  listDocuments: "documents",
  listJournals: "accounting",
  matchBankStatementLine: "banking",
  postManualJournal: "accounting",
  saveDocumentReview: "documents",
} as const satisfies Record<string, ConveyorModule>;
type BinaryMethod = keyof typeof METHOD_MODULES;

async function invokeProto<Input extends DescMessage, Output extends DescMessage>(
  page: import("@playwright/test").Page,
  method: BinaryMethod,
  codec: Readonly<ProtoMethodCodec<Input, Output>>,
  request: MessageShape<Input>,
): Promise<MessageShape<Output>> {
  const responseBytes = await invokeConveyorFrame(
    page,
    METHOD_MODULES[method],
    method,
    codec.encodeRequest(request),
  );
  return codec.decodeResponse(responseBytes);
}

function command(
  authentication: MessageShape<typeof AuthenticationContextSchema>,
  idempotencyKey: string,
) {
  return create(CommandContextSchema, { authentication, idempotencyKey });
}

function aud(minorUnits: bigint) {
  return create(MoneySchema, { currencyCode: "AUD", minorUnits });
}

async function authenticatedWorkspace(page: import("@playwright/test").Page) {
  return page.evaluate(() => {
    const retained = window.sessionStorage.getItem("tammy.session.active");
    if (!retained) throw new Error("AUTHENTICATED_WORKSPACE_MISSING");
    const value = JSON.parse(retained) as Record<string, unknown>;
    if (
      typeof value.workspaceId !== "string" ||
      typeof value.userId !== "string" ||
      typeof value.sessionId !== "string" ||
      typeof value.organisationId !== "string"
    ) {
      throw new Error("AUTHENTICATED_WORKSPACE_INVALID");
    }
    return {
      workspaceId: value.workspaceId,
      userId: value.userId,
      sessionId: value.sessionId,
      organisationId: value.organisationId,
    };
  });
}

async function assertRoute(
  page: import("@playwright/test").Page,
  linkName: string,
  path: string,
  heading: string,
) {
  await page.getByRole("link", { name: linkName }).click();
  await expect(page).toHaveURL(new RegExp(`${path.replaceAll("/", "\\/")}$`));
  await expect(page.getByRole("heading", { name: heading, exact: true })).toBeVisible();
}

async function captureJourney(
  page: import("@playwright/test").Page,
  milestone: string,
  recovery = false,
) {
  const sourceRevision = test
    .info()
    .annotations.find((annotation) => annotation.type === "feedback-source")?.description;
  if (
    sourceRevision &&
    /^(0[1-9]-|10-|19-320-(documents|banking)|25-match-review|2[89]-|3[0-7]-)/.test(milestone)
  ) {
    expect(sourceRevision).toMatch(/^[0-9a-f]{40}$/);
    const directory = path.resolve(
      import.meta.dirname,
      "../../../../.tmp/ui-feedback-2026-10-23",
      sourceRevision,
      "workflows",
    );
    await mkdir(directory, { recursive: true, mode: 0o700 });
    await chmod(directory, 0o700);
    const screenshotPath = path.join(directory, `${milestone}.png`);
    await page.screenshot({
      animations: "disabled",
      fullPage: true,
      path: screenshotPath,
      mask: [
        page.locator(
          'input[type="password"], input[autocomplete="new-password"], input[autocomplete="current-password"]',
        ),
        ...(recovery ? [page.locator("p.break-all")] : []),
      ],
    });
    await chmod(screenshotPath, 0o600);
    await writeFile(
      path.join(directory, `${milestone}.json`),
      `${JSON.stringify(
        {
          sourceRevision,
          milestone,
          viewport: page.viewportSize(),
          sha256: createHash("sha256")
            .update(await readFile(screenshotPath))
            .digest("hex"),
          secretInputsMasked: true,
          recoveryMasked: recovery,
        },
        null,
        2,
      )}\n`,
      { mode: 0o600 },
    );
  }
  const upliftPhase = process.env.TAMMY_UPLIFT_CAPTURE;
  const phase = upliftPhase ?? process.env.TAMMY_JOURNEY_CAPTURE;
  if (phase !== "before" && phase !== "after") {
    if (/^(04-documents|05-banking|19-320-(documents|banking)|25-match-review)$/.test(milestone)) {
      const screenshotPath = test
        .info()
        .outputPath("integrated-ui", `integrated-ui-${milestone}.png`);
      await mkdir(path.dirname(screenshotPath), { recursive: true });
      await page.screenshot({ animations: "disabled", fullPage: true, path: screenshotPath });
      await test.info().attach(`integrated-ui-${milestone}.png`, {
        path: screenshotPath,
        contentType: "image/png",
      });
    }
    return;
  }
  if (
    upliftPhase &&
    !/^(04-documents|05-banking|11-|12-|13-|18-|19-\d+-(documents|banking)|20-|21-|22-|23-|2[4-9]-)/.test(
      milestone,
    )
  )
    return;
  const directory = path.resolve(
    import.meta.dirname,
    upliftPhase
      ? "../../../../output/playwright/soft-precision-review-2026-10-22"
      : "../../../../output/playwright/soft-precision",
    phase,
  );
  await mkdir(directory, { recursive: true });
  const viewport = page.viewportSize();
  if (viewport) await page.mouse.move(viewport.width - 4, viewport.height - 4);
  const retainedScroll = upliftPhase
    ? await page.evaluate(() => ({ x: window.scrollX, y: window.scrollY }))
    : undefined;
  if (upliftPhase)
    await page.evaluate(() => window.scrollTo({ top: 0, left: 0, behavior: "instant" }));
  await page.screenshot({
    path: path.join(directory, `${milestone}.png`),
    fullPage: true,
    mask: recovery ? [page.locator("p.break-all")] : [],
  });
  if (retainedScroll)
    await page.evaluate(
      ({ x, y }) => window.scrollTo({ left: x, top: y, behavior: "instant" }),
      retainedScroll,
    );
}

async function interactionJourney(page: import("@playwright/test").Page) {
  await assertRoute(page, "Documents", "/documents", "Documents");
  await page.locator('input[type="file"]').setInputFiles({
    name: "unsupported-synthetic.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("synthetic"),
  });
  await expect(page.getByRole("alert")).toContainText("Choose a PDF, PNG or JPEG");
  await captureJourney(page, "11-document-upload-error");
  await page.locator('input[type="file"]').setInputFiles({
    name: "synthetic-supplies.png",
    mimeType: "image/png",
    buffer: Buffer.from(
      "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6iocAAAAASUVORK5CYII=",
      "base64",
    ),
  });
  await expect(page.getByRole("heading", { name: "synthetic-supplies.png" })).toBeVisible();
  await page.getByLabel("Supplier", { exact: true }).fill("Wattle Studio Supplies (synthetic)");
  await page.getByLabel("Invoice number").fill("SP-100");
  await page.getByLabel("Date", { exact: true }).fill("2024-05-12");
  await page.getByLabel("Subtotal", { exact: true }).fill("100.00");
  await page.getByLabel("GST", { exact: true }).fill("10.00");
  await page.getByLabel("Total", { exact: true }).fill("110.00");
  const overlongSupplier = "Synthetic supplier ".repeat(16);
  await page.getByLabel("Supplier", { exact: true }).fill(overlongSupplier);
  await page.getByRole("button", { name: "Save review", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("The review could not be saved");
  await expect(page.getByLabel("Supplier", { exact: true })).toHaveValue(overlongSupplier);
  await captureJourney(page, "12-review-error-preserved");
  await page.getByLabel("Supplier", { exact: true }).fill("Wattle Studio Supplies (synthetic)");
  await page.getByRole("button", { name: "Save review", exact: true }).dblclick();
  await expect(page.getByRole("button", { name: "Review saved", exact: true })).toBeDisabled();
  await expect(page.getByText("$110.00", { exact: true })).toBeVisible();
  await captureJourney(page, "13-review-saved");
  await expect(page.getByLabel("Supplier", { exact: true })).toHaveAttribute("readonly", "");
  await page.getByLabel("Search documents").fill("No synthetic match here");
  await expect(page.getByText("No matching documents", { exact: true })).toBeVisible();
  await captureJourney(page, "26-document-search-empty");
  await page.getByLabel("Search documents").fill("");
  await page.getByRole("button", { name: "Close review" }).click();
  await expect(page.getByRole("region", { name: "Document review" })).toHaveCount(0);
  await captureJourney(page, "27-document-collection");
  await assertRoute(page, "Banking", "/banking", "Banking");
  await page.getByRole("button", { name: "Import statement", exact: true }).click();
  await page.getByLabel("Opening balance").fill("1000.00");
  await page.getByLabel("CSV rows").fill("Cancelled synthetic row");
  await page.getByRole("button", { name: "Close statement import" }).click();
  await page.getByRole("button", { name: "Import statement", exact: true }).click();
  await expect(page.getByLabel("CSV rows")).toHaveValue("Cancelled synthetic row");
  await captureJourney(page, "24-import-draft-reopened");
  await page.getByRole("button", { name: "Cancel import" }).click();
  await page.getByRole("button", { name: "Import statement", exact: true }).click();
  await expect(page.getByLabel("CSV rows")).toHaveValue("");
  await page.getByLabel("Opening balance").fill("1000.00");
  await page.getByLabel("CSV rows").fill("invalid synthetic row");
  await page.getByRole("button", { name: "Import statement", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Use one row per line");
  await captureJourney(page, "20-csv-validation");
  await page.getByLabel("Opening balance").fill("123456789.01");
  await page.getByLabel("CSV rows").fill("2024-05-12,Synthetic studio supplies,-110.00");
  await page.getByRole("button", { name: "Import statement", exact: true }).click();
  await expect(
    page
      .getByRole("table", { name: "Statement lines" })
      .getByText("Synthetic studio supplies", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Complete reconciliation", exact: true }),
  ).toBeDisabled();
  await captureJourney(page, "21-statement-unmatched");
  await page.getByRole("button", { name: "Match", exact: true }).click();
  await expect(page.getByLabel("Reviewed source reference")).toBeFocused();
  await page.getByLabel("Reviewed source reference").fill("Cancelled synthetic reference");
  await captureJourney(page, "25-match-review");
  await page.setViewportSize({ width: 320, height: 1000 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  const narrowReconciliation = page.getByRole("button", {
    name: "Complete reconciliation",
    exact: true,
  });
  await narrowReconciliation.scrollIntoViewIfNeeded();
  await expect(narrowReconciliation).toBeVisible();
  await expect(narrowReconciliation).toBeDisabled();
  await captureJourney(page, "25-match-review-narrow");
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.getByRole("button", { name: "Cancel match" }).click();
  await expect(
    page.getByRole("button", { name: "Complete reconciliation", exact: true }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "Match", exact: true }).click();
  await expect(page.getByLabel("Reviewed source reference")).toHaveValue("");
  await page.getByLabel("Reviewed source reference").fill("Synthetic receipt SP-100");
  await page.getByRole("button", { name: "Confirm match", exact: true }).dblclick();
  await expect(
    page.getByRole("button", { name: "Complete reconciliation", exact: true }),
  ).toBeEnabled();
  await captureJourney(page, "22-statement-matched");
  await page.getByRole("button", { name: "Complete reconciliation", exact: true }).dblclick();
  await expect(
    page.getByRole("button", { name: "Complete reconciliation", exact: true }),
  ).toBeDisabled();
  await expect(page.getByText("$123,456,679.01", { exact: true })).toBeVisible();
  await captureJourney(page, "23-statement-reconciled");

  await assertRoute(page, "Chart of accounts", "/accounting/chart", "Chart of accounts");
  await page.getByRole("button", { name: "Add account", exact: true }).click();
  await page.getByLabel("Code", { exact: true }).fill("TEST");
  await page.getByLabel("Account name", { exact: true }).fill("Cancelled synthetic account");
  await captureJourney(page, "14-account-entry");
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(page.getByLabel("Account name", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("cell", { name: "Cancelled synthetic account" })).toHaveCount(0);

  await assertRoute(page, "Journals", "/accounting/journals", "Journals");
  await page.getByRole("cell", { name: "Office supplies paid personally", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Office supplies paid personally", exact: true }),
  ).toBeVisible();
  await captureJourney(page, "15-journal-detail");
  await page.getByRole("button", { name: "Journals", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Journals", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "New journal", exact: true }).click();
  await expect(page.getByLabel("Debit account")).toBeVisible();
  await captureJourney(page, "16-journal-entry");
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(page.getByLabel("Debit account")).toHaveCount(0);

  await assertRoute(page, "Audit trail", "/audit", "Audit trail");
  await expect(
    page.getByText("Wattle Studio Supplies (synthetic)", { exact: false }).first(),
  ).toBeVisible();
  await captureJourney(page, "17-local-activity");
  await page.goBack();
  await expect(page.getByRole("heading", { name: "Journals", exact: true })).toBeVisible();
  await page.goForward();
  await expect(page.getByRole("heading", { name: "Audit trail", exact: true })).toBeVisible();

  await page.getByRole("link", { name: "Overview", exact: true }).focus();
  await page.keyboard.press("Tab");
  const documentsLink = page.getByRole("link", { name: "Documents", exact: true });
  await expect(documentsLink).toBeFocused();
  expect(await documentsLink.evaluate((element) => getComputedStyle(element).outlineStyle)).toBe(
    "solid",
  );
  await captureJourney(page, "18-keyboard-focus");

  await page.emulateMedia({ reducedMotion: "reduce" });
  for (const width of [320, 390, 900]) {
    await page.setViewportSize({ width, height: 1000 });
    for (const [label, route, heading] of [
      ["Documents", "/documents", "Documents"],
      ["Banking", "/banking", "Banking"],
      ["Trial balance", "/accounting/trial-balance", "Trial balance"],
    ]) {
      if (!label || !route || !heading) throw new Error("JOURNEY_ROUTE_MISSING");
      await assertRoute(page, label, route, heading);
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
      ).toBe(true);
      await captureJourney(page, `19-${width}-${label.toLowerCase().replaceAll(" ", "-")}`);
      if (width === 320 && label === "Documents") {
        const previousPage = page.getByRole("button", {
          name: "Previous document page",
          exact: true,
        });
        const nextPage = page.getByRole("button", { name: "Next document page", exact: true });
        const paginationGroup = previousPage.locator("..");
        await paginationGroup.evaluate((element) => {
          const bounds = element.getBoundingClientRect();
          const top =
            window.scrollY + bounds.top - Math.max(0, (window.innerHeight - bounds.height) / 2);
          window.scrollTo(window.scrollX, Math.round(top));
        });
        await expect(previousPage).toBeInViewport({ ratio: 1 });
        await expect(nextPage).toBeInViewport({ ratio: 1 });
      }
      if (width === 320 && label === "Banking") {
        await page.getByRole("button", { name: "Import statement", exact: true }).click();
        await page.getByLabel("Opening balance").fill("681.00");
        await page.getByLabel("CSV rows").fill("2024-05-13,Synthetic narrow draft,-12.50");
        expect(
          await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
        ).toBe(true);
        const balance = page.getByText("$123,456,679.01", { exact: true });
        await expect(balance).toHaveCSS("white-space", "nowrap");
        await captureJourney(page, "19-320-banking-import");
        await page.getByRole("button", { name: "Cancel import" }).click();
      }
    }
  }
  expect(
    await documentsLink.evaluate((element) => getComputedStyle(element).transitionDuration),
  ).toBe("0s");
  await page.setViewportSize({ width: 1440, height: 1000 });
  await assertRoute(page, "Documents", "/documents", "Documents");
  await page.getByRole("button", { name: /Needs review/ }).click();
  await expect(page.getByText("No matching documents", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Close review" }).click();
  await expect(page.getByLabel("Search documents")).toBeFocused();
}

test("runs invoice, purchase, and BAS workflows through the packaged app", async ({
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
  await page.evaluate(() => window.history.replaceState(null, "", "/"));
  await page.reload();
  const retainedWorkspace = await authenticatedCurrentWorkflowWorkspace(page);
  expect({
    workspaceId: retainedWorkspace.workspaceId,
    userId: retainedWorkspace.userId,
    sessionId: retainedWorkspace.sessionId,
    organisationId: retainedWorkspace.organisationId,
  }).toEqual(workspace);
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
      await page.getByRole("button", { name: "Import statement", exact: true }).click();
      const importForm = page.getByRole("form", { name: "Statement import", exact: true });
      await expect(importForm).toBeVisible();
      await expect(page.getByLabel("Opening balance")).toBeVisible();
      await expect(page.getByLabel("CSV rows")).toBeVisible();
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
    switch (target.heading) {
      case "Overview":
        await expect(
          page.getByRole("heading", { name: "Needs review", exact: true }),
        ).toBeVisible();
        await expect(page.getByText("Nothing needs review", { exact: true })).toBeVisible();
        await expect(
          page.getByText("Your retained local workflows are up to date.", { exact: true }),
        ).toBeVisible();
        await expect(page.getByText("1 reviewed this quarter", { exact: true })).toBeVisible();
        break;
      case "Documents": {
        const retainedDocuments = page.getByRole("table", {
          name: "Retained documents",
          exact: true,
        });
        await expect(retainedDocuments).toBeVisible();
        const sourceRow = retainedDocuments
          .getByRole("row")
          .filter({ hasText: fixture.sourceDocument.sourceDisplayName });
        await expect(sourceRow).toContainText(fixture.sourceDocument.supplierName);
        await expect(sourceRow).toContainText(fixture.sourceDocument.invoiceNumber);
        await expect(sourceRow).toContainText("Reviewed");
        break;
      }
      case "Trial balance": {
        const postingDateLabel = fixture.period.postingDate.split("-").reverse().join("/");
        await expect(
          page.getByText(`Debit and credit balances as at ${postingDateLabel}.`, { exact: true }),
        ).toBeVisible();
        await expect(
          page.getByText(fixture.accounts[0]?.name ?? "", { exact: true }),
        ).toBeVisible();
        await expect(
          page.getByText(fixture.accounts[1]?.name ?? "", { exact: true }),
        ).toBeVisible();
        break;
      }
      case "Banking":
        await expect(
          page.getByRole("region", { name: "Statement workspace", exact: true }),
        ).toBeVisible();
        await expect(
          page.getByRole("form", { name: "Statement import", exact: true }),
        ).toBeVisible();
        break;
      case "BAS preparation":
        expect.soft(actualSnapshot).toContain("BAS preparation");
        expect.soft(actualSnapshot).toContain("BAS lifecycle stages");
        break;
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
  const issuedWorkspace = await authenticatedCurrentWorkflowWorkspace(page);
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
    await authenticatedCurrentWorkflowWorkspace(restarted),
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
  await page.getByLabel("Workspace passphrase", { exact: true }).fill(CURRENT_WORKFLOW_PASSPHRASE);
  await page.getByLabel("Email or username").fill(CURRENT_WORKFLOW_USERNAME);
  await page
    .getByLabel("Administrator password", { exact: true })
    .fill(CURRENT_WORKFLOW_ADMIN_PASSWORD);
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
