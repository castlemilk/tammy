import { create } from "@bufbuild/protobuf";
import {
  BusinessIncomeSummarySchema,
  InvoiceAccountingSchema,
  InvoicePaymentSchema,
} from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { GstBasis } from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import { expect, it } from "vitest";
import { businessIncomeToRecord, invoiceAccountingToRecord } from "./invoice-accounting";

const id = "01900000-0000-7000-8000-000000000001";
const date = { year: 2026, month: 10, day: 1 };
const aud = (minorUnits: bigint) => ({ currencyCode: "AUD", minorUnits });

it("projects a posted invoice and dated payment without rounding cents", () => {
  const value = create(InvoiceAccountingSchema, {
    invoiceId: id,
    organisationId: id,
    version: 2n,
    posted: true,
    gstBasis: GstBasis.CASH,
    receivedMinor: 10001n,
    outstandingMinor: 6499n,
    paymentCount: 1n,
    payments: [
      {
        id,
        organisationId: id,
        invoiceId: id,
        paymentDate: date,
        amount: aud(10001n),
        allocatedGst: aud(909n),
        basSalesGross: aud(10001n),
        bankAccountId: id,
        journalId: id,
      },
    ],
  });
  expect(invoiceAccountingToRecord(value)).toMatchObject({
    posted: true,
    gstBasis: "cash",
    version: 2,
    receivedMinor: 10001,
    outstandingMinor: 6499,
    payments: [{ paymentDate: "2026-10-01", amountMinor: 10001, allocatedGstMinor: 909 }],
  });
  value.receivedMinor = BigInt(Number.MAX_SAFE_INTEGER) + 1n;
  expect(() => invoiceAccountingToRecord(value)).toThrow("INVALID_INVOICE_ACCOUNTING");
});

it("rejects invalid dates, currencies and impossible posting versions", () => {
  const value = create(InvoiceAccountingSchema, {
    invoiceId: id,
    organisationId: id,
    posted: true,
  });
  expect(() => invoiceAccountingToRecord(value)).toThrow("INVALID_INVOICE_ACCOUNTING");
  value.version = 2n;
  value.paymentCount = 1n;
  value.payments = [
    create(InvoicePaymentSchema, {
      id,
      organisationId: id,
      invoiceId: id,
      paymentDate: { year: 2026, month: 2, day: 30 },
      amount: aud(100n),
      allocatedGst: aud(9n),
      basSalesGross: aud(100n),
      bankAccountId: id,
      journalId: id,
    }),
  ];
  expect(() => invoiceAccountingToRecord(value)).toThrow("INVALID_INVOICE_ACCOUNTING");
  const payment = value.payments[0];
  if (!payment?.amount) throw new Error("Payment fixture missing");
  payment.paymentDate = { ...date, $typeName: "tammy.v1.CivilDate" };
  payment.amount.currencyCode = "USD";
  expect(() => invoiceAccountingToRecord(value)).toThrow("INVALID_INVOICE_ACCOUNTING");
});

it("uses Unicode character limits for retained receipt references", () => {
  const value = create(InvoiceAccountingSchema, {
    invoiceId: id,
    organisationId: id,
    posted: true,
    version: 2n,
    paymentCount: 1n,
    receivedMinor: 100n,
    payments: [
      {
        id,
        organisationId: id,
        invoiceId: id,
        paymentDate: date,
        amount: aud(100n),
        allocatedGst: aud(9n),
        basSalesGross: aud(100n),
        bankAccountId: id,
        journalId: id,
        reference: "🧾".repeat(200),
      },
    ],
  });
  expect(invoiceAccountingToRecord(value).payments[0]?.reference).toBe("🧾".repeat(200));
  const payment = value.payments[0];
  if (!payment) throw new Error("Payment fixture missing");
  payment.reference += "🧾";
  expect(() => invoiceAccountingToRecord(value)).toThrow("INVALID_INVOICE_ACCOUNTING");
});

it("keeps period activity separate from outstanding as-of dates and signed revenue", () => {
  const value = create(BusinessIncomeSummarySchema, {
    organisationId: id,
    periodStart: date,
    periodEnd: { year: 2026, month: 12, day: 31 },
    asOfDate: { year: 2026, month: 10, day: 2 },
    businessIncome: aud(-100n),
    invoiceIncome: aud(15000n),
    paymentsReceived: aud(10001n),
    outstanding: aud(6499n),
    invoiceGst: aud(909n),
    unpostedIncome: aud(0n),
    issuedCount: 1n,
    gstBasis: GstBasis.CASH,
    financialRevision: 4n,
  });
  expect(businessIncomeToRecord(value)).toMatchObject({
    businessIncomeMinor: -100,
    outstandingMinor: 6499,
    periodEnd: "2026-12-31",
    asOfDate: "2026-10-02",
  });
  if (!value.businessIncome) throw new Error("Income fixture missing");
  value.businessIncome.currencyCode = "USD";
  expect(() => businessIncomeToRecord(value)).toThrow("INVALID_BUSINESS_INCOME");
  value.businessIncome.currencyCode = "AUD";
  value.asOfDate = { year: 2026, month: 2, day: 30, $typeName: "tammy.v1.CivilDate" };
  expect(() => businessIncomeToRecord(value)).toThrow("INVALID_BUSINESS_INCOME");
});
