import type { CivilDate, Money } from "@tammy/connect-client/tammy/v1/common_pb.js";
import type {
  BusinessIncomeSummary,
  InvoiceAccounting,
  InvoicePayment,
} from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { GstBasis } from "@tammy/connect-client/tammy/v1/organisation_pb.js";

export type InvoiceGstBasis = "cash" | "non-cash" | "unconfigured";
export interface InvoicePaymentRecord {
  readonly id: string;
  readonly paymentDate: string;
  readonly amountMinor: number;
  readonly allocatedGstMinor: number;
  readonly basSalesGrossMinor: number;
  readonly bankAccountId: string;
  readonly reference: string;
  readonly journalId: string;
}
export interface InvoiceAccountingRecord {
  readonly invoiceId: string;
  readonly version: number;
  readonly posted: boolean;
  readonly gstBasis: InvoiceGstBasis;
  readonly revenueAccountId?: string;
  readonly journalId?: string;
  readonly receivedMinor: number;
  readonly outstandingMinor: number;
  readonly payments: readonly InvoicePaymentRecord[];
  readonly paymentCount: number;
}
export interface BusinessIncomeRecord {
  readonly organisationId: string;
  readonly periodStart: string;
  readonly periodEnd: string;
  readonly asOfDate: string;
  readonly businessIncomeMinor: number;
  readonly invoiceIncomeMinor: number;
  readonly paymentsReceivedMinor: number;
  readonly outstandingMinor: number;
  readonly invoiceGstMinor: number;
  readonly issuedCount: number;
  readonly unpostedIssuedCount: number;
  readonly gstBasis: InvoiceGstBasis;
  readonly financialRevision: number;
  readonly unpostedIncomeMinor: number;
}

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u;
function integer(value: bigint, error: string, signed = false): number {
  if (
    (!signed && value < 0n) ||
    value > BigInt(Number.MAX_SAFE_INTEGER) ||
    value < BigInt(Number.MIN_SAFE_INTEGER)
  )
    throw new Error(error);
  return Number(value);
}
function money(value: Money | undefined, error: string, signed = false): number {
  if (value?.currencyCode !== "AUD") throw new Error(error);
  return integer(value.minorUnits, error, signed);
}
function date(value: CivilDate | undefined, error: string): string {
  if (
    !value ||
    !Number.isInteger(value.year) ||
    value.year < 1 ||
    value.year > 9999 ||
    !Number.isInteger(value.month) ||
    !Number.isInteger(value.day)
  )
    throw new Error(error);
  const parsed = new Date(0);
  parsed.setUTCFullYear(value.year, value.month - 1, value.day);
  if (
    parsed.getUTCFullYear() !== value.year ||
    parsed.getUTCMonth() + 1 !== value.month ||
    parsed.getUTCDate() !== value.day
  )
    throw new Error(error);
  return `${String(value.year).padStart(4, "0")}-${String(value.month).padStart(2, "0")}-${String(value.day).padStart(2, "0")}`;
}
function basis(value: GstBasis, error: string): InvoiceGstBasis {
  if (value === GstBasis.CASH) return "cash";
  if (value === GstBasis.NON_CASH) return "non-cash";
  if (value === GstBasis.UNSPECIFIED) return "unconfigured";
  throw new Error(error);
}
function paymentToRecord(
  value: InvoicePayment,
  invoiceId: string,
  organisationId: string,
): InvoicePaymentRecord {
  const error = "INVALID_INVOICE_ACCOUNTING";
  if (
    !value ||
    !uuid.test(value.id) ||
    value.invoiceId !== invoiceId ||
    value.organisationId !== organisationId ||
    !uuid.test(value.bankAccountId) ||
    !uuid.test(value.journalId) ||
    [...value.reference].length > 200
  )
    throw new Error(error);
  const amountMinor = money(value.amount, error);
  const allocatedGstMinor = money(value.allocatedGst, error);
  const basSalesGrossMinor = money(value.basSalesGross, error);
  if (amountMinor <= 0 || allocatedGstMinor > amountMinor || basSalesGrossMinor > amountMinor)
    throw new Error(error);
  return {
    id: value.id,
    paymentDate: date(value.paymentDate, error),
    amountMinor,
    allocatedGstMinor,
    basSalesGrossMinor,
    bankAccountId: value.bankAccountId,
    reference: value.reference,
    journalId: value.journalId,
  };
}
export function invoiceAccountingToRecord(value: InvoiceAccounting): InvoiceAccountingRecord {
  const error = "INVALID_INVOICE_ACCOUNTING";
  if (
    !uuid.test(value.invoiceId) ||
    !uuid.test(value.organisationId) ||
    (value.revenueAccountId !== undefined && !uuid.test(value.revenueAccountId)) ||
    (value.journalId !== undefined && !uuid.test(value.journalId)) ||
    value.payments.length > 200 ||
    value.paymentCount < BigInt(value.payments.length) ||
    (value.posted
      ? value.version !== value.paymentCount + 1n
      : value.version !== 0n || value.paymentCount !== 0n || value.receivedMinor !== 0n)
  )
    throw new Error(error);
  return {
    invoiceId: value.invoiceId,
    version: integer(value.version, error),
    posted: value.posted,
    gstBasis: basis(value.gstBasis, error),
    ...(value.revenueAccountId ? { revenueAccountId: value.revenueAccountId } : {}),
    ...(value.journalId ? { journalId: value.journalId } : {}),
    receivedMinor: integer(value.receivedMinor, error),
    outstandingMinor: integer(value.outstandingMinor, error),
    payments: value.payments.map((payment) =>
      paymentToRecord(payment, value.invoiceId, value.organisationId),
    ),
    paymentCount: integer(value.paymentCount, error),
  };
}
export function businessIncomeToRecord(value: BusinessIncomeSummary): BusinessIncomeRecord {
  const error = "INVALID_BUSINESS_INCOME";
  const periodStart = date(value.periodStart, error),
    periodEnd = date(value.periodEnd, error);
  if (!uuid.test(value.organisationId) || periodStart > periodEnd) throw new Error(error);
  return {
    organisationId: value.organisationId,
    periodStart,
    periodEnd,
    asOfDate: date(value.asOfDate, error),
    businessIncomeMinor: money(value.businessIncome, error, true),
    invoiceIncomeMinor: money(value.invoiceIncome, error),
    paymentsReceivedMinor: money(value.paymentsReceived, error),
    outstandingMinor: money(value.outstanding, error),
    invoiceGstMinor: money(value.invoiceGst, error),
    issuedCount: integer(value.issuedCount, error),
    unpostedIssuedCount: integer(value.unpostedIssuedCount, error),
    gstBasis: basis(value.gstBasis, error),
    financialRevision: integer(value.financialRevision, error),
    unpostedIncomeMinor: money(value.unpostedIncome, error),
  };
}
