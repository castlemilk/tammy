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
import type { TammyDesktopAPI } from "../../../src/shared/desktop-api";
import { createProtoMethodCodec, type ProtoMethodCodec } from "../../../src/shared/proto-ipc";
import { type ElectronHarness, expect } from "../fixtures";

const UUID_V7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
export const CURRENT_WORKFLOW_USERNAME = "admin@tammy.local";
export const CURRENT_WORKFLOW_PASSPHRASE = "workspace-passphrase-long-enough";
export const CURRENT_WORKFLOW_ADMIN_PASSWORD = "administrator-password-long-enough";
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

export interface AuthenticatedAccountingWorkspace {
  readonly organisationId: string;
  readonly sessionId: string;
  readonly userId: string;
  readonly workspaceId: string;
}

export async function setupAndRunCurrentAccountingWorkflow(
  initialPage: import("@playwright/test").Page,
  electronHarness: ElectronHarness,
): Promise<AuthenticatedAccountingWorkspace> {
  let page = initialPage;
  await page.evaluate(() => {
    window.history.replaceState(null, "", "/setup/workspace");
    window.dispatchEvent(new PopStateEvent("popstate"));
  });
  await expect(page).toHaveURL(/\/setup\/workspace$/);
  await expect(page.getByRole("heading", { name: "Create your local workspace" })).toBeVisible();

  await page.getByLabel("Your name").fill("Tammy Admin");
  await page.getByLabel("Email or username").fill(CURRENT_WORKFLOW_USERNAME);
  await page.getByLabel("Business legal name").fill("Wattle & Co Test Pty Ltd");
  await page.getByLabel("Business display name").fill("Wattle & Co Test Pty Ltd");
  await page.getByLabel("ABN").fill("11000000560");
  await page.getByLabel("Workspace passphrase").fill(CURRENT_WORKFLOW_PASSPHRASE);
  await page.getByLabel("Administrator password").fill(CURRENT_WORKFLOW_ADMIN_PASSWORD);
  await page.getByRole("button", { name: "Create local workspace" }).click();
  await expect(page.getByRole("heading", { name: "Save your recovery code" })).toBeVisible();
  await expect(page.getByText("One-time recovery code")).toBeVisible();
  await page.getByRole("button", { name: "I saved my recovery code" }).click();
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
  await expect(
    page.getByRole("navigation", { name: "Primary" }).getByRole("link", { name: "General ledger" }),
  ).toHaveCount(0);

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
  await page.getByLabel("Workspace passphrase").fill(CURRENT_WORKFLOW_PASSPHRASE);
  await page.getByLabel("Email or username").fill(CURRENT_WORKFLOW_USERNAME);
  await page.getByLabel("Administrator password").fill(CURRENT_WORKFLOW_ADMIN_PASSWORD);
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
  expect(createdBas.workpaper?.gstCredits1b?.minorUnits).toBe(2_900n);
  expect(createdBas.workpaper?.netGstPayable?.minorUnits).toBe(-2_900n);
  expect(createdBas.workpaper?.sources).toHaveLength(1);
  expect(createdBas.workpaper?.sources[0]).toMatchObject({
    documentId: retainedDocument.id,
    supplierName: "Officeworks Ltd",
    invoiceNumber: "INV-029847",
  });

  const currentBas = await invokeProto(
    page,
    "getCurrentBasDraft",
    getCurrentBasDraftCodec,
    create(GetCurrentBasDraftRequestSchema, { authentication, organisationId }),
  );
  expect(currentBas.workpaper?.id).toBe(createdBas.workpaper?.id);
  expect(currentBas.workpaper?.status).toBe(BasWorkpaperStatus.DRAFT_NOT_LODGED);

  const attention = await invokeProto(
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
  expect(attention).toMatchObject({
    documentsNeedingReview: 0,
    documentsReviewedInPeriod: 1,
    bankingLinesNeedingReconciliation: 0,
    bankingLinesUnreconciledInPeriod: 0,
    currentDraftBasWorkpapers: 1,
    basStatus: BasAttentionStatus.DRAFT_NOT_LODGED,
  });
  expect(attention.revisions).toBeDefined();
  expect(attention.revisions?.financialRevision).toBe(7n);
  expect(attention.revisions?.ledgerRevision).toBe(1n);
  expect(attention.revisions?.bankingRevision).toBe(3n);
  expect(attention.revisions?.taxSourceRevision).toBe(3n);
  expect(attention.attentionItems).toHaveLength(0);

  await assertRoute(page, "Documents", "/documents", "Documents");
  await expect(page.getByText("officeworks-invoice.pdf")).toBeVisible();
  await expect(page.getByText("Reviewed", { exact: true }).first()).toBeVisible();
  await assertRoute(page, "Banking", "/banking", "Banking");
  await expect(page.getByText("$681.00", { exact: true })).toBeVisible();
  await expect(page.getByText("Reconciled", { exact: true })).toBeVisible();
  await assertRoute(page, "Chart of accounts", "/accounting/chart", "Chart of accounts");
  await expect(page.getByText("Office expenses", { exact: true })).toBeVisible();
  await assertRoute(page, "Journals", "/accounting/journals", "Journals");
  await expect(page.getByText("Office supplies paid personally", { exact: true })).toBeVisible();
  await assertRoute(page, "Trial balance", "/accounting/trial-balance", "Trial balance");
  await expect(page.getByText("$319.00", { exact: true }).first()).toBeVisible();
  await assertRoute(page, "GST & BAS", "/gst-bas", "GST & BAS");
  await expect(page.getByText("Draft — not lodged", { exact: true }).first()).toBeVisible();
  await expect(page.getByText("Officeworks Ltd", { exact: true })).toBeVisible();
  await assertRoute(page, "Overview", "/overview", "Overview");
  await expect(page.getByText("Draft — not lodged", { exact: true })).toBeVisible();

  expect(electronHarness.consoleErrors).toEqual([]);
  expect(electronHarness.pageErrors).toEqual([]);
  return workspace;
}

type BinaryMethod = Exclude<
  keyof TammyDesktopAPI,
  | "getSystemDiagnostics"
  | "selectMachineCredentialFile"
  | "importMachineCredential"
  | "replaceMachineCredential"
  | "unlockMachineCredential"
  | "importSbrProductId"
>;

async function invokeProto<Input extends DescMessage, Output extends DescMessage>(
  page: import("@playwright/test").Page,
  method: BinaryMethod,
  codec: Readonly<ProtoMethodCodec<Input, Output>>,
  request: MessageShape<Input>,
): Promise<MessageShape<Output>> {
  const requestBytes = [...codec.encodeRequest(request)];
  const responseBytes = await page.evaluate(
    async ({ methodName, bytes }) => {
      const response = await window.tammy[methodName](Uint8Array.from(bytes));
      return [...response];
    },
    { methodName: method, bytes: requestBytes },
  );
  return codec.decodeResponse(Uint8Array.from(responseBytes));
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
