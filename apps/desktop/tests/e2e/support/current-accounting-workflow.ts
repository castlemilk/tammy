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
import { validateScreenshotFixture } from "../../../../../scripts/check-app-store-screenshots.mjs";
import screenshotFixture from "../../../release/macos/screenshots/fixture.json" with {
  type: "json",
};
import type { TammyDesktopAPI } from "../../../src/shared/desktop-api";
import { createProtoMethodCodec, type ProtoMethodCodec } from "../../../src/shared/proto-ipc";
import { type ElectronHarness, expect } from "../fixtures";

const UUID_V7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const WORKFLOW_FIXTURE = validateScreenshotFixture(screenshotFixture);
const EXPENSE_ACCOUNT = requiredFixtureAccount("expense");
const EQUITY_ACCOUNT = requiredFixtureAccount("equity");
export const CURRENT_WORKFLOW_USERNAME = WORKFLOW_FIXTURE.operator.username;
export const CURRENT_WORKFLOW_PASSPHRASE = "workspace-passphrase-long-enough";
export const CURRENT_WORKFLOW_ADMIN_PASSWORD = "administrator-password-long-enough";
const PERIOD_START = civilDate(WORKFLOW_FIXTURE.period.start);
const PERIOD_END = civilDate(WORKFLOW_FIXTURE.period.end);
const POSTING_DATE = civilDate(WORKFLOW_FIXTURE.period.postingDate);
const CASE_IDS = WORKFLOW_FIXTURE.ids;

function requiredFixtureAccount(role: "expense" | "equity") {
  const accounts = WORKFLOW_FIXTURE.accounts.filter((account) => account.role === role);
  const account = accounts[0];
  if (accounts.length !== 1 || !account) {
    throw new Error("SCREENSHOT_FIXTURE_ACCOUNTS_INVALID");
  }
  return account;
}

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
  { fixedRendererClock = false }: { readonly fixedRendererClock?: boolean } = {},
): Promise<AuthenticatedAccountingWorkspace> {
  let page = initialPage;
  const fixedInstant = `${WORKFLOW_FIXTURE.period.postingDate}T02:00:00.000Z`;
  const installFixtureClock = (instant: string) => {
    const NativeDate = Date;
    const timestamp = new NativeDate(instant).getTime();
    class FixedDate extends NativeDate {
      constructor(...args: unknown[]) {
        if (args.length === 0) super(timestamp);
        else if (args.length === 1) super(args[0] as string);
        else {
          const [year, month, date, hours, minutes, seconds, milliseconds] = args as number[];
          super(
            year ?? 0,
            month ?? 0,
            date ?? 1,
            hours ?? 0,
            minutes ?? 0,
            seconds ?? 0,
            milliseconds ?? 0,
          );
        }
      }
      static override now() {
        return timestamp;
      }
    }
    globalThis.Date = FixedDate as DateConstructor;
  };
  if (fixedRendererClock) {
    await electronHarness.application.context().addInitScript(installFixtureClock, fixedInstant);
    await page.evaluate(installFixtureClock, fixedInstant);
  }
  await page.evaluate(() => {
    window.history.replaceState(null, "", "/setup/workspace");
    window.dispatchEvent(new PopStateEvent("popstate"));
  });
  await expect(page).toHaveURL(/\/setup\/workspace$/);
  await expect(page.getByRole("heading", { name: "Create your local workspace" })).toBeVisible();

  await page.getByLabel("Your name").fill(WORKFLOW_FIXTURE.operator.displayName);
  await page.getByLabel("Email or username").fill(CURRENT_WORKFLOW_USERNAME);
  await page.getByLabel("Business legal name").fill(WORKFLOW_FIXTURE.business.legalName);
  await page.getByLabel("Business display name").fill(WORKFLOW_FIXTURE.business.displayName);
  await page.getByLabel("ABN").fill(WORKFLOW_FIXTURE.business.abn.value);
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
  if (fixedRendererClock) await page.evaluate(installFixtureClock, fixedInstant);
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
      code: EXPENSE_ACCOUNT.code,
      name: EXPENSE_ACCOUNT.name,
      type: AccountType.EXPENSE,
      normalBalance: NormalBalance.DEBIT,
      reportClassification: EXPENSE_ACCOUNT.reportClassification,
      cashFlowClassification: EXPENSE_ACCOUNT.cashFlowClassification,
    }),
  );
  expect(expense.account).toMatchObject({
    organisationId,
    version: 1n,
    code: EXPENSE_ACCOUNT.code,
    name: EXPENSE_ACCOUNT.name,
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
      code: EQUITY_ACCOUNT.code,
      name: EQUITY_ACCOUNT.name,
      type: AccountType.EQUITY,
      normalBalance: NormalBalance.CREDIT,
      reportClassification: EQUITY_ACCOUNT.reportClassification,
      cashFlowClassification: EQUITY_ACCOUNT.cashFlowClassification,
    }),
  );
  expect(equity.account).toMatchObject({
    organisationId,
    version: 1n,
    code: EQUITY_ACCOUNT.code,
    name: EQUITY_ACCOUNT.name,
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
      memo: WORKFLOW_FIXTURE.journal.memo,
      lines: [
        {
          clientLineId: CASE_IDS.journalDebit,
          accountId: expenseAccount.id,
          debit: aud(BigInt(WORKFLOW_FIXTURE.journal.amountMinorUnits)),
          credit: aud(0n),
          description: WORKFLOW_FIXTURE.journal.debitDescription,
        },
        {
          clientLineId: CASE_IDS.journalCredit,
          accountId: equityAccount.id,
          debit: aud(0n),
          credit: aud(BigInt(WORKFLOW_FIXTURE.journal.amountMinorUnits)),
          description: WORKFLOW_FIXTURE.journal.creditDescription,
        },
      ],
    }),
  );
  expect(posted.journal).toMatchObject({
    organisationId,
    version: 1n,
    state: JournalState.POSTED,
    source: JournalSource.MANUAL,
    memo: WORKFLOW_FIXTURE.journal.memo,
  });
  expect(posted.journal?.id).toMatch(UUID_V7);
  expect(posted.journal?.totalDebits?.minorUnits).toBe(
    BigInt(WORKFLOW_FIXTURE.journal.amountMinorUnits),
  );
  expect(posted.journal?.totalCredits?.minorUnits).toBe(
    BigInt(WORKFLOW_FIXTURE.journal.amountMinorUnits),
  );
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
  expect(trialBalance.totalDebits?.minorUnits).toBe(
    BigInt(WORKFLOW_FIXTURE.journal.amountMinorUnits),
  );
  expect(trialBalance.totalCredits?.minorUnits).toBe(
    BigInt(WORKFLOW_FIXTURE.journal.amountMinorUnits),
  );
  expect(trialBalance.financialRevision).toBe(1n);

  const statement = await invokeProto(
    page,
    "importBankStatement",
    importBankStatementCodec,
    create(ImportBankStatementRequestSchema, {
      commandContext: command(authentication, CASE_IDS.bankImport),
      organisationId,
      openingBalance: aud(BigInt(WORKFLOW_FIXTURE.banking.openingBalanceMinorUnits)),
      lines: [
        create(BankStatementLineInputSchema, {
          transactionDate: POSTING_DATE,
          description: WORKFLOW_FIXTURE.banking.lineDescription,
          amount: aud(BigInt(WORKFLOW_FIXTURE.banking.lineAmountMinorUnits)),
        }),
      ],
    }),
  );
  expect(statement.statementImport?.id).toMatch(UUID_V7);
  expect(statement.statementImport).toMatchObject({ organisationId, lineCount: 1 });
  expect(statement.statementImport?.openingBalance?.minorUnits).toBe(
    BigInt(WORKFLOW_FIXTURE.banking.openingBalanceMinorUnits),
  );
  expect(statement.statementImport?.closingBalance?.minorUnits).toBe(
    BigInt(WORKFLOW_FIXTURE.banking.closingBalanceMinorUnits),
  );

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
    description: WORKFLOW_FIXTURE.banking.lineDescription,
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
      matchReference: WORKFLOW_FIXTURE.banking.matchReference,
    }),
  );
  expect(matched.line).toMatchObject({
    version: 2n,
    status: BankStatementLineStatus.MATCHED,
    matchReference: WORKFLOW_FIXTURE.banking.matchReference,
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
  expect(beforeReconciliation.latestClosingBalance?.minorUnits).toBe(
    BigInt(WORKFLOW_FIXTURE.banking.closingBalanceMinorUnits),
  );

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
  expect(reconciliation.closingBalance?.minorUnits).toBe(
    BigInt(WORKFLOW_FIXTURE.banking.closingBalanceMinorUnits),
  );

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
  expect(bankingSummary.latestClosingBalance?.minorUnits).toBe(
    BigInt(WORKFLOW_FIXTURE.banking.closingBalanceMinorUnits),
  );

  const invoiceBytes = new TextEncoder().encode(WORKFLOW_FIXTURE.sourceDocument.syntheticPdfText);
  const candidate = create(DocumentCandidateSchema, {
    supplierName: WORKFLOW_FIXTURE.sourceDocument.supplierName,
    invoiceNumber: WORKFLOW_FIXTURE.sourceDocument.invoiceNumber,
    documentDate: POSTING_DATE,
    subtotal: aud(BigInt(WORKFLOW_FIXTURE.sourceDocument.subtotalMinorUnits)),
    gst: aud(BigInt(WORKFLOW_FIXTURE.sourceDocument.gstMinorUnits)),
    total: aud(BigInt(WORKFLOW_FIXTURE.sourceDocument.totalMinorUnits)),
  });
  const retained = await invokeProto(
    page,
    "ingestDocument",
    ingestDocumentCodec,
    create(IngestDocumentRequestSchema, {
      commandContext: command(authentication, CASE_IDS.document),
      organisationId,
      sourceDisplayName: WORKFLOW_FIXTURE.sourceDocument.sourceDisplayName,
      mimeType: WORKFLOW_FIXTURE.sourceDocument.mimeType,
      original: invoiceBytes,
      extractedText: WORKFLOW_FIXTURE.sourceDocument.extractedText,
      candidate,
    }),
  );
  expect(retained.document?.id).toMatch(UUID_V7);
  expect(retained.document).toMatchObject({
    organisationId,
    version: 1n,
    status: DocumentStatus.NEEDS_REVIEW,
    sourceDisplayName: WORKFLOW_FIXTURE.sourceDocument.sourceDisplayName,
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
  expect(storedDocument.document?.candidate?.invoiceNumber).toBe(
    WORKFLOW_FIXTURE.sourceDocument.invoiceNumber,
  );
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
  expect(createdBas.workpaper?.gstCredits1b?.minorUnits).toBe(
    BigInt(WORKFLOW_FIXTURE.bas.gstCreditsMinorUnits),
  );
  expect(createdBas.workpaper?.netGstPayable?.minorUnits).toBe(
    BigInt(WORKFLOW_FIXTURE.bas.netGstPayableMinorUnits),
  );
  expect(createdBas.workpaper?.sources).toHaveLength(1);
  expect(createdBas.workpaper?.sources[0]).toMatchObject({
    documentId: retainedDocument.id,
    supplierName: WORKFLOW_FIXTURE.sourceDocument.supplierName,
    invoiceNumber: WORKFLOW_FIXTURE.sourceDocument.invoiceNumber,
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
  await expect(page.getByText(WORKFLOW_FIXTURE.sourceDocument.sourceDisplayName)).toBeVisible();
  await expect(page.getByText("Reviewed", { exact: true }).first()).toBeVisible();
  await assertRoute(page, "Banking", "/banking", "Banking");
  await expect(page.getByText("$681.00", { exact: true })).toBeVisible();
  await expect(page.getByText("Reconciled", { exact: true })).toBeVisible();
  await assertRoute(page, "Chart of accounts", "/accounting/chart", "Chart of accounts");
  await expect(page.getByText(EXPENSE_ACCOUNT.name, { exact: true })).toBeVisible();
  await assertRoute(page, "Journals", "/accounting/journals", "Journals");
  await expect(page.getByText(WORKFLOW_FIXTURE.journal.memo, { exact: true })).toBeVisible();
  await assertRoute(page, "Trial balance", "/accounting/trial-balance", "Trial balance");
  await expect(page.getByText("$319.00", { exact: true }).first()).toBeVisible();
  await assertRoute(page, "GST & BAS", "/gst-bas", "GST & BAS");
  await expect(
    page.getByText(WORKFLOW_FIXTURE.bas.statusLabel, { exact: true }).first(),
  ).toBeVisible();
  await expect(
    page.getByText(WORKFLOW_FIXTURE.sourceDocument.supplierName, { exact: true }),
  ).toBeVisible();
  await assertRoute(page, "Overview", "/overview", "Overview");
  await expect(page.getByText(WORKFLOW_FIXTURE.bas.statusLabel, { exact: true })).toBeVisible();

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

function civilDate(value: string) {
  const [year, month, day] = value.split("-").map(Number);
  if (!year || !month || !day) throw new Error("SCREENSHOT_FIXTURE_DATE_INVALID");
  return create(CivilDateSchema, { day, month, year });
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
