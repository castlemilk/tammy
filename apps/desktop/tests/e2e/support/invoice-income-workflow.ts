import { create } from "@bufbuild/protobuf";
import type { Page } from "@playwright/test";
import {
  GetJournalRequestSchema,
  GetJournalResponseSchema,
  JournalSource,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  GetBusinessIncomeSummaryRequestSchema,
  GetBusinessIncomeSummaryResponseSchema,
  GetInvoiceAccountingRequestSchema,
  GetInvoiceAccountingResponseSchema,
  ListInvoicesRequestSchema,
  ListInvoicesResponseSchema,
} from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { createProtoMethodCodec } from "../../../src/shared/proto-ipc";
import { expect } from "../fixtures";
import { invokeConveyorFrame } from "./conveyor";
import type { AuthenticatedAccountingWorkspace } from "./current-accounting-workflow";

const list = createProtoMethodCodec({
  input: ListInvoicesRequestSchema,
  output: ListInvoicesResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 16777216,
});
const accounting = createProtoMethodCodec({
  input: GetInvoiceAccountingRequestSchema,
  output: GetInvoiceAccountingResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 262144,
});
const income = createProtoMethodCodec({
  input: GetBusinessIncomeSummaryRequestSchema,
  output: GetBusinessIncomeSummaryResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 32768,
});
const journal = createProtoMethodCodec({
  input: GetJournalRequestSchema,
  output: GetJournalResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 262144,
});

function authority(workspace: AuthenticatedAccountingWorkspace) {
  return {
    organisationId: workspace.organisationId,
    authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
  };
}
function date(value: string) {
  const [year, month, day] = value.split("-").map(Number);
  return { year: year ?? 0, month: month ?? 0, day: day ?? 0 };
}

export async function readInvoiceAccounting(
  page: Page,
  workspace: AuthenticatedAccountingWorkspace,
  number: string,
) {
  const listed = list.decodeResponse(
    await invokeConveyorFrame(
      page,
      "accounting",
      "listInvoices",
      list.encodeRequest(
        create(ListInvoicesRequestSchema, { ...authority(workspace), page: { pageSize: 200 } }),
      ),
    ),
  );
  const invoice = listed.invoices.find((value) => value.draft?.number === number);
  if (!invoice) throw new Error("INVOICE_INCOME_ACCEPTANCE_INVOICE_MISSING");
  const response = accounting.decodeResponse(
    await invokeConveyorFrame(
      page,
      "accounting",
      "getInvoiceAccounting",
      accounting.encodeRequest(
        create(GetInvoiceAccountingRequestSchema, {
          ...authority(workspace),
          invoiceId: invoice.id,
        }),
      ),
    ),
  );
  if (!response.accounting) throw new Error("INVOICE_INCOME_ACCEPTANCE_ACCOUNTING_MISSING");
  return { invoice, accounting: response.accounting };
}

export async function readBusinessIncome(
  page: Page,
  workspace: AuthenticatedAccountingWorkspace,
  start: string,
  end: string,
  asOf: string,
) {
  const response = income.decodeResponse(
    await invokeConveyorFrame(
      page,
      "accounting",
      "getBusinessIncomeSummary",
      income.encodeRequest(
        create(GetBusinessIncomeSummaryRequestSchema, {
          ...authority(workspace),
          periodStart: date(start),
          periodEnd: date(end),
          asOfDate: date(asOf),
        }),
      ),
    ),
  );
  if (!response.summary) throw new Error("INVOICE_INCOME_ACCEPTANCE_SUMMARY_MISSING");
  return response.summary;
}

export async function expectInvoiceJournal(
  page: Page,
  workspace: AuthenticatedAccountingWorkspace,
  invoiceNumber: string,
  net: bigint,
  gross: bigint,
  gst: bigint,
) {
  const value = await readInvoiceAccounting(page, workspace, invoiceNumber);
  expect(value.accounting.posted).toBe(true);
  if (!value.accounting.journalId) throw new Error("INVOICE_INCOME_ACCEPTANCE_JOURNAL_MISSING");
  const response = journal.decodeResponse(
    await invokeConveyorFrame(
      page,
      "accounting",
      "getJournal",
      journal.encodeRequest(
        create(GetJournalRequestSchema, {
          authentication: authority(workspace).authentication,
          journalId: value.accounting.journalId,
        }),
      ),
    ),
  );
  expect(response.journal?.source).toBe(JournalSource.RECEIVABLE);
  expect(
    response.journal?.lines.reduce((sum, line) => sum + (line.debit?.minorUnits ?? 0n), 0n),
  ).toBe(gross);
  expect(
    response.journal?.lines.reduce((sum, line) => sum + (line.credit?.minorUnits ?? 0n), 0n),
  ).toBe(gross);
  expect(response.journal?.lines.map((line) => line.credit?.minorUnits ?? 0n)).toContain(net);
  if (gst > 0n)
    expect(response.journal?.lines.map((line) => line.credit?.minorUnits ?? 0n)).toContain(gst);
  return value;
}

export async function showBusinessIncome(page: Page, start: string, end: string, asOf: string) {
  await page.getByRole("link", { name: "Overview", exact: true }).click();
  await page.getByLabel("Income period start", { exact: true }).fill(start);
  await page.getByLabel("Income period end", { exact: true }).fill(end);
  await page.getByLabel("Balances as of", { exact: true }).fill(asOf);
  await expect(page.getByText("Loading business income…", { exact: true })).toHaveCount(0);
}

export async function recordInvoiceReceipt(
  page: Page,
  number: string,
  paymentDate: string,
  amount: string,
  reference: string,
) {
  await page.getByRole("link", { name: "Invoices", exact: true }).click();
  await page.getByRole("button", { name: `Open ${number}`, exact: true }).click();
  await page.getByRole("button", { name: "Record payment", exact: true }).click();
  await page.getByLabel("Payment date", { exact: true }).fill(paymentDate);
  await page.getByLabel("Amount received (AUD)", { exact: true }).fill(amount);
  await expect(page.getByLabel("Bank account", { exact: true })).not.toHaveValue("");
  await page.getByLabel("Payment reference (optional)", { exact: true }).fill(reference);
  await page.getByRole("button", { name: "Save payment", exact: true }).click();
  await expect(page.getByText("Payment recorded.", { exact: true })).toBeVisible();
}

export async function regenerateInvoiceBas(
  page: Page,
  start: string,
  end: string,
  sales: string,
  gst: string,
) {
  await page.getByRole("link", { name: "GST & BAS", exact: true }).click();
  await page.getByLabel("Period start", { exact: true }).fill(start);
  await page.getByLabel("Period end", { exact: true }).fill(end);
  const button = page.getByRole("button", { name: "Regenerate local draft", exact: true });
  await expect(button).toBeEnabled();
  await button.click();
  await expect(
    page.getByText(`Saved reporting period: ${displayDate(start)} – ${displayDate(end)}`, {
      exact: true,
    }),
  ).toBeVisible();
  await expect(page.getByTestId("bas-sales-g1")).toContainText(sales);
  await expect(page.getByTestId("bas-gst-on-sales-1a")).toContainText(gst);
  await expect(page.getByText(/Sources changed since this workpaper was saved/)).toHaveCount(0);
}
function displayDate(value: string) {
  return value.split("-").reverse().join("/");
}
