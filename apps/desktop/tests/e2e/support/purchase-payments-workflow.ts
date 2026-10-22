import { createHash, randomBytes } from "node:crypto";
import { readFile, stat } from "node:fs/promises";
import { create, type DescMessage, type MessageShape } from "@bufbuild/protobuf";
import type { Locator, Page } from "@playwright/test";
import * as a from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import * as d from "@tammy/connect-client/tammy/v1/documents_pb.js";
import * as p from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import * as tax from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { createProtoMethodCodec } from "../../../src/shared/proto-ipc";
import { type ElectronHarness, expect } from "../fixtures";
import { type ConveyorModule, invokeConveyorFrame } from "./conveyor";
import {
  type AuthenticatedAccountingWorkspace,
  authenticatedWorkspace,
  CURRENT_WORKFLOW_ADMIN_PASSWORD,
  CURRENT_WORKFLOW_PASSPHRASE,
  CURRENT_WORKFLOW_USERNAME,
} from "./current-accounting-workflow";
import { regenerateInvoiceBas } from "./invoice-income-workflow";
import { timestampUtcCivilDate } from "./purchase-time.js";
import { openSavedBasWorkpaperPanel } from "./saved-bas-workpaper-panel";

export type PurchaseWorkspace = AuthenticatedAccountingWorkspace & {
  readonly expense: a.Account;
  readonly capital: a.Account;
  readonly privateUse: a.Account;
  readonly bank: a.Account;
};
export function purchaseKey(): string {
  const random = randomBytes(16);
  random[6] = (random[6]! & 15) | 112;
  random[8] = (random[8]! & 63) | 128;
  const hex = random.toString("hex");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
export function purchaseDate(iso: string) {
  const [year, month, day] = iso.split("-").map(Number);
  return { year: year!, month: month!, day: day! };
}
export function purchaseAuth(workspace: AuthenticatedAccountingWorkspace) {
  return { actorUserId: workspace.userId, sessionId: workspace.sessionId };
}
export function purchaseCommand(workspace: AuthenticatedAccountingWorkspace) {
  return { authentication: purchaseAuth(workspace), idempotencyKey: purchaseKey() };
}
export async function purchaseProto<I extends DescMessage, O extends DescMessage>(
  page: Page,
  module: ConveyorModule,
  method: string,
  input: I,
  output: O,
  request: MessageShape<I>,
  maximumRequestBytes = 16_384,
  maximumResponseBytes = 4_194_304,
): Promise<MessageShape<O>> {
  const codec = createProtoMethodCodec({
    input,
    output,
    maximumRequestBytes,
    maximumResponseBytes,
  });
  return codec.decodeResponse(
    await invokeConveyorFrame(page, module, method, codec.encodeRequest(request)),
  );
}

export async function setupPurchaseWorkspace(
  harness: ElectronHarness,
  basis: "cash" | "non-cash",
): Promise<PurchaseWorkspace> {
  const page = harness.currentPage();
  await page.evaluate(() => {
    window.history.replaceState(null, "", "/setup/workspace");
    window.dispatchEvent(new PopStateEvent("popstate"));
  });
  await page.getByLabel("Your name", { exact: true }).fill("Purchase acceptance operator");
  await page.getByLabel("Email or username", { exact: true }).fill(CURRENT_WORKFLOW_USERNAME);
  await page
    .getByLabel("Business legal name", { exact: true })
    .fill("Synthetic Purchase Acceptance Pty Ltd");
  await page.getByLabel("Business display name", { exact: true }).fill("Purchase Acceptance");
  await page.getByLabel("ABN", { exact: true }).fill("51824753556");
  await page
    .getByLabel("GST accounting basis", { exact: true })
    .selectOption(basis === "cash" ? "1" : "2");
  await page.getByLabel("Workspace passphrase", { exact: true }).fill(CURRENT_WORKFLOW_PASSPHRASE);
  await page
    .getByLabel("Administrator password", { exact: true })
    .fill(CURRENT_WORKFLOW_ADMIN_PASSWORD);
  await page.getByRole("button", { name: "Create local workspace", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Save your recovery code", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "I saved my recovery code", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
  const workspace = await authenticatedWorkspace(page);
  const createAccount = async (code: string, name: string, type: a.AccountType, flow: string) => {
    const result = await purchaseProto(
      page,
      "accounting",
      "createAccount",
      a.CreateAccountRequestSchema,
      a.CreateAccountResponseSchema,
      create(a.CreateAccountRequestSchema, {
        commandContext: purchaseCommand(workspace),
        organisationId: workspace.organisationId,
        code,
        name,
        type,
        normalBalance:
          type === a.AccountType.EQUITY ? a.NormalBalance.CREDIT : a.NormalBalance.DEBIT,
        reportClassification: "custom.account",
        cashFlowClassification: flow,
      }),
      32_768,
      32_768,
    );
    if (!result.account) throw new Error("PURCHASE_ACCEPTANCE_ACCOUNT_MISSING");
    return result.account;
  };
  const expense = await createAccount("6101", "Purchase cost", a.AccountType.EXPENSE, "operating");
  const capital = await createAccount(
    "1501",
    "Purchase equipment",
    a.AccountType.ASSET,
    "investing",
  );
  const privateUse = await createAccount(
    "3101",
    "Private acquisition",
    a.AccountType.EQUITY,
    "financing",
  );
  const accounts = await purchaseProto(
    page,
    "accounting",
    "listAccounts",
    a.ListAccountsRequestSchema,
    a.ListAccountsResponseSchema,
    create(a.ListAccountsRequestSchema, {
      authentication: purchaseAuth(workspace),
      organisationId: workspace.organisationId,
      page: { pageSize: 200 },
    }),
    16_384,
    131_072,
  );
  const bank = accounts.accounts.find((account) => account.code === "1000");
  if (!bank) throw new Error("PURCHASE_ACCEPTANCE_BANK_MISSING");
  return { ...workspace, expense, capital, privateUse, bank };
}

// A real one-page PDF original; all tax and amount decisions are reviewed in UI.
export function syntheticPurchasePdf(
  number: string,
  isNotice:
    | boolean
    | "legacy-review"
    | "payg-evidence"
    | "actual-due-date-evidence"
    | "required-return-notice"
    | "profile-evidence"
    | "gst-registration-evidence" = false,
): Buffer {
  const text =
    isNotice === "legacy-review"
      ? `BT /F1 12 Tf 50 750 Td (Synthetic legacy source review ${number}) Tj 0 -20 Td (Opening supplier item unpaid through 09 October 2026) Tj 0 -20 Td (Earlier invoice paid AUD 110.00 on 02 October 2026) Tj 0 -20 Td (Separate sources - no purchase credit or opening settlement inferred) Tj ET`
      : isNotice === "payg-evidence"
        ? `BT /F1 12 Tf 50 750 Td (Synthetic payroll and PAYG notice ${number}) Tj 0 -20 Td (W1 total payments AUD 1,000.00; W2 withheld AUD 50.00) Tj 0 -20 Td (PAYGI T1 AUD 2,010.99; operative rate 1.7 percent; prior variation none) Tj 0 -20 Td (Supporting evidence only - no purchase or payment value) Tj ET`
        : isNotice === "actual-due-date-evidence"
          ? `BT /F1 12 Tf 50 750 Td (Synthetic ATO activity statement ${number}) Tj 0 -20 Td (Actual statement due date 28 October 2026) Tj 0 -20 Td (Supporting evidence only - no purchase or payment value) Tj ET`
          : isNotice === "profile-evidence"
            ? `BT /F1 12 Tf 50 750 Td (Synthetic business registration and obligation evidence ${number}) Tj 0 -20 Td (GST registration effective from 01 July 2025) Tj 0 -20 Td (Quarterly GST, PAYGW and PAYGI obligations reviewed; eight other statement obligations not applicable) Tj 0 -20 Td (Supporting evidence only - no purchase or payment value) Tj ET`
            : isNotice === "gst-registration-evidence"
              ? `BT /F1 12 Tf 50 750 Td (Synthetic GST registration and PAYGI status evidence ${number}) Tj 0 -20 Td (GST registration effective from 01 July 2025) Tj 0 -20 Td (Synthetic PAYGI notice confirms no instalment obligation applies) Tj 0 -20 Td (Supporting evidence only - no purchase or payment value) Tj ET`
              : isNotice
                ? `BT /F1 12 Tf 50 750 Td (Synthetic required-return notice ${number}) Tj 0 -20 Td (Required return due date 03 November 2026) Tj 0 -20 Td (Supporting evidence only - no purchase or payment value) Tj ET`
                : `BT /F1 12 Tf 50 750 Td (Synthetic tax invoice ${number}) Tj 0 -20 Td (Supplier ABN 51 824 753 556) Tj 0 -20 Td (Purchase total AUD 110.00 including GST 10.00) Tj ET`;
  const objects = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
    `<< /Length ${Buffer.byteLength(text)} >>\nstream\n${text}\nendstream`,
  ];
  let content = "%PDF-1.4\n";
  const offsets = [0];
  for (const [index, object] of objects.entries()) {
    offsets.push(Buffer.byteLength(content));
    content += `${index + 1} 0 obj\n${object}\nendobj\n`;
  }
  const xref = Buffer.byteLength(content);
  content += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n${offsets
    .slice(1)
    .map((offset) => `${String(offset).padStart(10, "0")} 00000 n \n`)
    .join("")}trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  return Buffer.from(content);
}
export async function retainPurchaseThroughUi(
  page: Page,
  workspace: AuthenticatedAccountingWorkspace,
  number: string,
  invoiceDate = "2026-08-31",
  zeroMoney = false,
  legacyReviewProof = false,
  documentKind:
    | "purchase"
    | "payg-evidence"
    | "actual-due-date-evidence"
    | "required-return-notice"
    | "profile-evidence"
    | "gst-registration-evidence" = "purchase",
): Promise<d.Document> {
  await page.getByRole("link", { name: "Documents", exact: true }).click();
  await page.locator('input[type="file"]').setInputFiles({
    name: `${number}.pdf`,
    mimeType: "application/pdf",
    buffer: syntheticPurchasePdf(
      number,
      legacyReviewProof
        ? "legacy-review"
        : documentKind === "payg-evidence" ||
            documentKind === "actual-due-date-evidence" ||
            documentKind === "required-return-notice" ||
            documentKind === "profile-evidence" ||
            documentKind === "gst-registration-evidence"
          ? documentKind
          : zeroMoney,
    ),
  });
  await expect(page.getByRole("heading", { name: `${number}.pdf`, exact: true })).toBeVisible();
  await page
    .getByLabel("Supplier", { exact: true })
    .fill(
      documentKind === "payg-evidence"
        ? "Synthetic payroll and ATO notice"
        : documentKind === "actual-due-date-evidence"
          ? "Synthetic ATO activity statement"
          : documentKind === "required-return-notice"
            ? "Synthetic ATO required-return notice"
            : documentKind === "gst-registration-evidence"
              ? "Synthetic GST registration and PAYGI status evidence"
              : documentKind === "profile-evidence"
                ? "Synthetic business registration evidence"
                : '=Synthetic supplier, "Élodie"',
    );
  const review = page
    .locator("section")
    .filter({ has: page.getByRole("heading", { name: `${number}.pdf`, exact: true }) });
  await review.getByLabel("Invoice number", { exact: true }).fill(number);
  await page.getByLabel("Date", { exact: true }).fill(invoiceDate);
  await page.getByLabel("Subtotal", { exact: true }).fill(zeroMoney ? "0.00" : "100.00");
  await page.getByLabel("GST", { exact: true }).fill(zeroMoney ? "0.00" : "10.00");
  await page.getByLabel("Total", { exact: true }).fill(zeroMoney ? "0.00" : "110.00");
  await page.getByRole("button", { name: "Save review", exact: true }).click();
  await expect(page.getByRole("button", { name: "Review saved", exact: true })).toBeVisible();
  const response = await purchaseProto(
    page,
    "documents",
    "listDocuments",
    d.ListDocumentsRequestSchema,
    d.ListDocumentsResponseSchema,
    create(d.ListDocumentsRequestSchema, {
      authentication: purchaseAuth(workspace),
      organisationId: workspace.organisationId,
      page: { pageSize: 200 },
    }),
  );
  const document = response.documents.find((item) => item.candidate?.invoiceNumber === number);
  if (!document) throw new Error("PURCHASE_ACCEPTANCE_REVIEWED_DOCUMENT_MISSING");
  expect(document.status).toBe(d.DocumentStatus.REVIEWED);
  expect(document.candidate?.total?.minorUnits).toBe(zeroMoney ? 0n : 11000n);
  expect(document.sha256).toHaveLength(32);
  return document;
}

export function reviewedDocumentUtcDate(document: d.Document): string {
  return timestampUtcCivilDate(document.reviewedAt);
}

// Volume fixtures use the same authenticated document commands as ordinary
// intake and review; primary financial decisions remain UI actions.
export async function retainPurchaseVolumeProof(
  page: Page,
  workspace: AuthenticatedAccountingWorkspace,
  sequence: number,
): Promise<d.Document> {
  const number = `VOLUME-PROOF-${String(sequence).padStart(4, "0")}`;
  const candidate = create(d.DocumentCandidateSchema, {
    supplierName: "Synthetic supporting notice",
    invoiceNumber: number,
    documentDate: purchaseDate("2026-08-31"),
    subtotal: { currencyCode: "AUD", minorUnits: 0n },
    gst: { currencyCode: "AUD", minorUnits: 0n },
    total: { currencyCode: "AUD", minorUnits: 0n },
  });
  const ingested = await purchaseProto(
    page,
    "documents",
    "ingestDocument",
    d.IngestDocumentRequestSchema,
    d.IngestDocumentResponseSchema,
    create(d.IngestDocumentRequestSchema, {
      commandContext: purchaseCommand(workspace),
      organisationId: workspace.organisationId,
      sourceDisplayName: `${number}.pdf`,
      mimeType: "application/pdf",
      original: syntheticPurchasePdf(number, true),
      extractedText: `Synthetic zero-value supporting notice ${number}`,
      candidate,
    }),
    32_768,
    131_072,
  );
  if (!ingested.document) throw new Error("PURCHASE_VOLUME_INTAKE_MISSING");
  expect(ingested.document.version).toBe(1n);
  const reviewed = await purchaseProto(
    page,
    "documents",
    "saveDocumentReview",
    d.SaveDocumentReviewRequestSchema,
    d.SaveDocumentReviewResponseSchema,
    create(d.SaveDocumentReviewRequestSchema, {
      commandContext: purchaseCommand(workspace),
      documentId: ingested.document.id,
      expectedVersion: ingested.document.version,
      candidate,
    }),
    32_768,
    131_072,
  );
  if (!reviewed.document) throw new Error("PURCHASE_VOLUME_REVIEW_MISSING");
  expect(reviewed.document.status).toBe(d.DocumentStatus.REVIEWED);
  expect(reviewed.document.version).toBe(2n);
  expect(reviewed.document.candidate).toEqual(candidate);
  return reviewed.document;
}
export async function openPurchase(page: Page, number: string) {
  await page.getByRole("link", { name: "Documents", exact: true }).click();
  await page.getByRole("button").filter({ hasText: number }).click();
  await expect(
    page.getByRole("region", { name: "Purchase accounting", exact: true }),
  ).toBeVisible();
}
export async function readPurchase(
  page: Page,
  workspace: AuthenticatedAccountingWorkspace,
  documentId: string,
) {
  return purchaseProto(
    page,
    "purchases",
    "getPurchaseAccounting",
    p.GetPurchaseAccountingRequestSchema,
    p.GetPurchaseAccountingResponseSchema,
    create(p.GetPurchaseAccountingRequestSchema, {
      authentication: purchaseAuth(workspace),
      organisationId: workspace.organisationId,
      documentId,
    }),
  );
}
export async function readPurchasePayments(
  page: Page,
  workspace: AuthenticatedAccountingWorkspace,
  documentId: string,
) {
  const payments: p.PurchasePayment[] = [];
  let cursor = "";
  do {
    const result = await purchaseProto(
      page,
      "purchases",
      "listPurchasePayments",
      p.ListPurchasePaymentsRequestSchema,
      p.ListPurchasePaymentsResponseSchema,
      create(p.ListPurchasePaymentsRequestSchema, {
        authentication: purchaseAuth(workspace),
        organisationId: workspace.organisationId,
        documentId,
        page: { pageSize: 200, cursor: cursor || undefined },
      }),
    );
    payments.push(...result.payments);
    const next = result.page?.nextCursor ?? "";
    if (next && next === cursor) throw new Error("PURCHASE_ACCEPTANCE_PAYMENT_CURSOR_REPEATED");
    cursor = next;
  } while (cursor);
  return payments;
}

export async function expectPurchaseJournal(
  page: Page,
  workspace: AuthenticatedAccountingWorkspace,
  journalId: string | undefined,
  source: a.JournalSource,
  postingDate: string,
  expected: readonly { readonly code: string; readonly debit: bigint; readonly credit: bigint }[],
) {
  if (!journalId) throw new Error("PURCHASE_ACCEPTANCE_JOURNAL_MISSING");
  const result = await purchaseProto(
    page,
    "accounting",
    "getJournal",
    a.GetJournalRequestSchema,
    a.GetJournalResponseSchema,
    create(a.GetJournalRequestSchema, { authentication: purchaseAuth(workspace), journalId }),
    8192,
    2_097_152,
  );
  const accounts = await purchaseProto(
    page,
    "accounting",
    "listAccounts",
    a.ListAccountsRequestSchema,
    a.ListAccountsResponseSchema,
    create(a.ListAccountsRequestSchema, {
      authentication: purchaseAuth(workspace),
      organisationId: workspace.organisationId,
      page: { pageSize: 200 },
    }),
    16_384,
    131_072,
  );
  expect(accounts.page?.nextCursor ?? "").toBe("");
  const journal = result.journal;
  expect(journal?.id).toBe(journalId);
  expect(journal?.organisationId).toBe(workspace.organisationId);
  expect(journal?.source).toBe(source);
  expect(journal?.state).toBe(a.JournalState.POSTED);
  expect(journal?.postingDate).toMatchObject(purchaseDate(postingDate));
  expect(
    journal?.lines.map((line) => ({
      code: accounts.accounts.find((account) => account.id === line.accountId)?.code,
      debit: line.debit?.minorUnits,
      credit: line.credit?.minorUnits,
    })),
  ).toEqual(expected);
  for (const [index, line] of (journal?.lines ?? []).entries()) {
    expect(line.sequence).toBe(index + 1);
    expect(line.journalId).toBe(journalId);
    expect(line.debit?.currencyCode).toBe("AUD");
    expect(line.credit?.currencyCode).toBe("AUD");
  }
  expect(journal?.totalDebits?.minorUnits).toBe(
    expected.reduce((sum, line) => sum + line.debit, 0n),
  );
  expect(journal?.totalCredits?.minorUnits).toBe(journal?.totalDebits?.minorUnits);
  return journal;
}
export async function postPurchaseThroughUi(
  page: Page,
  workspace: PurchaseWorkspace,
  document: d.Document,
  options: { mixed?: boolean; evidence?: "invoice" | "missing" } = {},
) {
  const panel = page.getByRole("region", { name: "Purchase accounting", exact: true });
  await expect(
    panel.getByText("Reviewed source · not added to accounts", { exact: true }),
  ).toBeVisible();
  const allocations = options.mixed
    ? [
        {
          account: workspace.expense,
          gross: "66.00",
          supplier: "6.00",
          eligible: "6.00",
          use: p.PurchaseUse.BUSINESS,
          bas: p.PurchaseBasClassification.NON_CAPITAL,
          flow: p.PurchaseCashFlowClassification.OPERATING,
        },
        {
          account: workspace.capital,
          gross: "22.00",
          supplier: "2.00",
          eligible: "2.00",
          use: p.PurchaseUse.BUSINESS,
          bas: p.PurchaseBasClassification.CAPITAL,
          flow: p.PurchaseCashFlowClassification.INVESTING,
        },
        {
          account: workspace.privateUse,
          gross: "22.00",
          supplier: "2.00",
          eligible: "0.00",
          use: p.PurchaseUse.PRIVATE,
          bas: p.PurchaseBasClassification.EXCLUDED,
          flow: p.PurchaseCashFlowClassification.FINANCING,
        },
      ]
    : [
        {
          account: workspace.expense,
          gross: "110.00",
          supplier: "10.00",
          eligible: "10.00",
          use: p.PurchaseUse.BUSINESS,
          bas: p.PurchaseBasClassification.NON_CAPITAL,
          flow: p.PurchaseCashFlowClassification.OPERATING,
        },
      ];
  for (const [index, allocation] of allocations.entries()) {
    if (index) await panel.getByRole("button", { name: "Add allocation", exact: true }).click();
    const n = index + 1;
    await panel
      .getByRole("combobox", { name: `Account ${n}`, exact: true })
      .selectOption(allocation.account.id);
    await panel.getByLabel(`Gross ${n} (AUD)`, { exact: true }).fill(allocation.gross);
    await panel.getByLabel(`Supplier GST ${n} (AUD)`, { exact: true }).fill(allocation.supplier);
    await panel.getByLabel(`Eligible GST ${n} (AUD)`, { exact: true }).fill(allocation.eligible);
    await panel
      .getByRole("combobox", { name: `Use ${n}`, exact: true })
      .selectOption(String(allocation.use));
    await panel
      .getByRole("combobox", { name: `BAS classification ${n}`, exact: true })
      .selectOption(String(allocation.bas));
    await panel
      .getByRole("combobox", { name: `Cash flow ${n}`, exact: true })
      .selectOption(String(allocation.flow));
    if (allocation.use === p.PurchaseUse.PRIVATE)
      await panel
        .getByLabel(`Private / noncreditable reason ${n}`, { exact: true })
        .fill("Private acquisition; no credit claimed");
    await panel
      .getByLabel(`Eligibility and evidence reviewed for allocation ${n}`, { exact: true })
      .check();
  }
  await fillPurchaseEvidence(panel, options.evidence ?? "invoice", "2026-08-31");
  await panel.getByLabel("Checked existing accounting and GST claims", { exact: true }).check();
  await panel
    .getByLabel("Prior accounting review reason", { exact: true })
    .fill("Synthetic acquisition has no prior expense, opening payable or GST claim");
  await panel.getByLabel("No duplicate transaction found", { exact: true }).check();
  await panel.getByRole("button", { name: "Add purchase to accounts", exact: true }).click();
  await panel.getByRole("button", { name: "Confirm purchase posting", exact: true }).click();
  await expect(
    panel.getByRole("table", { name: "Posted purchase allocations", exact: true }),
  ).toBeVisible();
  const result = await readPurchase(page, workspace, document.id);
  expect(result.accounting?.accountingVersion).toBe(1n);
  expect(result.accounting?.posting?.gross?.minorUnits).toBe(11000n);
  expect(result.accounting?.paid?.minorUnits).toBe(0n);
  expect(result.accounting?.outstanding?.minorUnits).toBe(11000n);
  return result;
}
export async function fillPurchaseEvidence(
  panel: Locator,
  evidence: "invoice" | "missing",
  held: string,
) {
  await panel
    .getByRole("combobox", { name: "GST evidence classification", exact: true })
    .selectOption(
      String(
        evidence === "invoice"
          ? p.PurchaseEvidenceClassification.TAX_INVOICE
          : p.PurchaseEvidenceClassification.UNKNOWN,
      ),
    );
  if (evidence === "invoice")
    await panel.getByLabel("Actual evidence held date", { exact: true }).fill(held);
  await panel
    .getByLabel("Registration, business use and evidence reviewed", { exact: true })
    .check();
  await panel
    .getByLabel("Evidence review reason", { exact: true })
    .fill(
      evidence === "invoice"
        ? "Original supplier tax invoice held and eligibility reviewed"
        : "Tax invoice evidence is currently unavailable",
    );
}
export async function recordPurchasePaymentThroughUi(
  page: Page,
  workspace: PurchaseWorkspace,
  documentId: string,
  date: string,
  amount: string,
  reference: string,
) {
  const panel = page.getByRole("region", { name: "Purchase accounting", exact: true });
  const summary = panel.locator("summary").filter({ hasText: /^Record payment$/ });
  if ((await summary.locator("..").getAttribute("open")) === null) await summary.click();
  await panel.getByLabel("Actual payment date", { exact: true }).fill(date);
  await panel.getByLabel("Payment amount (AUD)", { exact: true }).fill(amount);
  await panel
    .getByRole("combobox", { name: "Payment bank account", exact: true })
    .selectOption(workspace.bank.id);
  await panel.getByLabel("Payment reference", { exact: true }).fill(reference);
  await panel.getByRole("button", { name: "Review recorded payment", exact: true }).click();
  await panel.getByRole("button", { name: "Confirm recorded payment", exact: true }).click();
  await expect(panel.getByRole("table", { name: "Purchase payments", exact: true })).toContainText(
    reference,
  );
  return readPurchase(page, workspace, documentId);
}
export async function reviewPurchaseEvidenceThroughUi(
  page: Page,
  workspace: AuthenticatedAccountingWorkspace,
  documentId: string,
  evidence: "invoice" | "missing",
  held: string,
) {
  const panel = page.getByRole("region", { name: "Purchase accounting", exact: true });
  await panel
    .locator("summary")
    .filter({ hasText: /^Review GST evidence$/ })
    .click();
  const fields = panel.locator("details").filter({
    has: page.getByRole("button", { name: "Retain GST evidence review", exact: true }),
  });
  await fillPurchaseEvidence(fields, evidence, held);
  const before = await readPurchase(page, workspace, documentId);
  await panel.getByRole("button", { name: "Retain GST evidence review", exact: true }).click();
  await panel.getByRole("button", { name: "Confirm GST evidence review", exact: true }).click();
  await expect
    .poll(
      async () => (await readPurchase(page, workspace, documentId)).accounting?.accountingVersion,
    )
    .toBe((before.accounting?.accountingVersion ?? 0n) + 1n);
  return readPurchase(page, workspace, documentId);
}
export async function readPurchaseBas(page: Page, workspace: AuthenticatedAccountingWorkspace) {
  const result = await purchaseProto(
    page,
    "reporting",
    "getCurrentBasDraft",
    tax.GetCurrentBasDraftRequestSchema,
    tax.GetCurrentBasDraftResponseSchema,
    create(tax.GetCurrentBasDraftRequestSchema, {
      authentication: purchaseAuth(workspace),
      organisationId: workspace.organisationId,
    }),
  );
  if (!result.workpaper) throw new Error("PURCHASE_ACCEPTANCE_BAS_MISSING");
  return result.workpaper;
}
export async function regeneratePurchaseBas(
  page: Page,
  workspace: AuthenticatedAccountingWorkspace,
  start: string,
  end: string,
  credit: bigint,
) {
  await regenerateInvoiceBas(page, start, end, "$0.00", "$0.00");
  const saved = await readPurchaseBas(page, workspace);
  expect(saved.gstCredits1b?.minorUnits).toBe(credit);
  expect(saved.netGstPayable?.minorUnits).toBe(-credit);
  expect(saved.purchaseSourceFormat).toBe(tax.BasPurchaseSourceFormat.RECONCILED_V1);
  expect(
    saved.purchaseSources.reduce((sum, source) => sum + (source.claimedGst?.minorUnits ?? 0n), 0n),
  ).toBe(credit);
  return saved;
}
export async function expectPurchaseControls(
  page: Page,
  workspace: AuthenticatedAccountingWorkspace,
  expected: Readonly<Record<string, bigint>>,
) {
  const balance = await purchaseProto(
    page,
    "accounting",
    "getTrialBalance",
    a.GetTrialBalanceRequestSchema,
    a.GetTrialBalanceResponseSchema,
    create(a.GetTrialBalanceRequestSchema, {
      authentication: purchaseAuth(workspace),
      organisationId: workspace.organisationId,
      asOfDate: purchaseDate("2026-10-09"),
    }),
    8192,
    524_288,
  );
  for (const [code, amount] of Object.entries(expected)) {
    const line = balance.lines.find((item) => item.code === code);
    expect(
      (line?.debits?.minorUnits ?? 0n) - (line?.credits?.minorUnits ?? 0n),
      `actual debit minus credit for ${code}`,
    ).toBe(amount);
  }
  expect(balance.totalDebits?.minorUnits).toBe(balance.totalCredits?.minorUnits);
  return balance;
}
export async function savePurchaseExport(
  harness: ElectronHarness,
  format: "CSV" | "PDF",
  filePath: string,
  start: string,
  end: string,
) {
  await openSavedBasWorkpaperPanel(harness.currentPage());
  await harness.application.evaluate(
    ({ dialog }, selection) => {
      dialog.showSaveDialog = async (...args: unknown[]) => {
        const options = args.at(-1) as {
          defaultPath?: string;
          filters?: { extensions: string[] }[];
        };
        if (
          !options?.defaultPath?.includes(selection.start) ||
          !options.defaultPath.includes(selection.end) ||
          !options.defaultPath.endsWith(`.${selection.extension}`) ||
          !options.filters?.some((filter) => filter.extensions.includes(selection.extension))
        )
          throw new Error("PURCHASE_ACCEPTANCE_NATIVE_DIALOG_INVALID");
        return { canceled: false, filePath: selection.filePath };
      };
    },
    { extension: format.toLowerCase(), filePath, start, end },
  );
  const button = harness
    .currentPage()
    .getByRole("button", { name: `Export ${format}`, exact: true });
  await expect(button).toBeEnabled();
  await button.click();
  const completion = `BAS ${format} exported.`;
  await expect(
    harness.currentPage().getByRole("status").filter({ hasText: completion }),
  ).toHaveText(completion);
  await expect
    .poll(() =>
      stat(filePath).then(
        (state) => state.size > 0,
        () => false,
      ),
    )
    .toBe(true);
  const [file, fileState] = await Promise.all([readFile(filePath), stat(filePath)]);
  expect(file.length).toBeGreaterThan(0);
  expect(fileState.size).toBe(file.length);
  const sha256 = createHash("sha256").update(file).digest("hex");
  expect(sha256).toMatch(/^[a-f0-9]{64}$/u);
  if (process.platform !== "win32") expect(fileState.mode & 0o777).toBe(0o600);
  // The BAS screen disables exports while its retained-workpaper query refetches
  // after the native save dialog returns. The next export still checks readiness.
  return { sha256, size: fileState.size };
}
export async function reopenPurchaseWorkspace(
  harness: ElectronHarness,
  inspectClosedWorkspace?: (userDataPath: string) => Promise<void>,
) {
  const page = await harness.restart("accounting-workflow-restart", inspectClosedWorkspace);
  await page.getByLabel("Workspace passphrase", { exact: true }).fill(CURRENT_WORKFLOW_PASSPHRASE);
  await page.getByLabel("Email or username", { exact: true }).fill(CURRENT_WORKFLOW_USERNAME);
  await page
    .getByLabel("Administrator password", { exact: true })
    .fill(CURRENT_WORKFLOW_ADMIN_PASSWORD);
  await page.getByRole("button", { name: "Unlock workspace", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
  return { page, workspace: await authenticatedWorkspace(page) };
}
