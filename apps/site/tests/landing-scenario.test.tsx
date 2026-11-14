import { describe, expect, it } from "vitest";
import {
  accounts,
  gstWorkpaper,
  invoice,
  manualJournal,
  overview,
  proposedInvoice,
  purchase,
  statement,
  trialBalance,
} from "../components/landing/scenario";
import { gstBreakdown } from "../lib/tools/calculations";

type Line = { readonly side: "debit" | "credit"; readonly account: { readonly code: string }; readonly cents: number };

const sum = (lines: readonly Line[], side: Line["side"]) =>
  lines.filter((line) => line.side === side).reduce((total, line) => total + line.cents, 0);

/** Every posting the landing page's synthetic quarter contains, in date order. */
const postings: readonly (readonly Line[])[] = [
  [
    { side: "debit", account: manualJournal.debit, cents: manualJournal.cents },
    { side: "credit", account: manualJournal.credit, cents: manualJournal.cents },
  ],
  purchase.lines,
  [
    { side: "debit", account: accounts.payable, cents: purchase.payment.cents },
    { side: "credit", account: accounts.bank, cents: purchase.payment.cents },
  ],
  invoice.issueLines,
  invoice.receiptLines,
];

describe("landing page synthetic books", () => {
  it("posts only balanced journals", () => {
    for (const journal of postings) {
      expect(sum(journal, "debit")).toBe(sum(journal, "credit"));
      expect(sum(journal, "debit")).toBeGreaterThan(0);
    }
  });

  it("shows a trial balance that is exactly the sum of those journals", () => {
    const balances = new Map<string, number>();
    for (const line of postings.flat()) {
      const signed = line.side === "debit" ? line.cents : -line.cents;
      balances.set(line.account.code, (balances.get(line.account.code) ?? 0) + signed);
    }
    const nonZero = [...balances].filter(([, cents]) => cents !== 0);
    expect(trialBalance.rows).toHaveLength(nonZero.length);
    for (const row of trialBalance.rows) {
      expect(row.debitCents - row.creditCents).toBe(balances.get(row.account.code));
      expect(Math.min(row.debitCents, row.creditCents)).toBe(0);
    }
    const debits = trialBalance.rows.reduce((total, row) => total + row.debitCents, 0);
    const credits = trialBalance.rows.reduce((total, row) => total + row.creditCents, 0);
    expect(debits).toBe(credits);
    expect(debits).toBe(trialBalance.totalCents);
  });

  it("uses the standard GST rule on the purchase and the invoice", () => {
    expect(gstBreakdown(purchase.subtotalCents, "add")).toEqual({
      exclusiveCents: purchase.subtotalCents,
      gstCents: purchase.gstCents,
      inclusiveCents: purchase.totalCents,
    });
    expect(gstBreakdown(invoice.subtotalCents, "add")).toEqual({
      exclusiveCents: invoice.subtotalCents,
      gstCents: invoice.gstCents,
      inclusiveCents: invoice.totalCents,
    });
    expect(invoice.line.unitCents).toBe(invoice.subtotalCents);
  });

  it("keeps the receipt, the balance owing and the overview in agreement", () => {
    expect(invoice.receivedCents).toBe(invoice.payment.cents);
    expect(invoice.outstandingCents).toBe(invoice.totalCents - invoice.receivedCents);
    expect(overview.businessIncomeCents).toBe(invoice.subtotalCents);
    expect(overview.invoiceIncomeCents).toBe(invoice.subtotalCents);
    expect(overview.paymentsReceivedCents).toBe(invoice.receivedCents);
    expect(overview.invoicesOwingCents).toBe(invoice.outstandingCents);
  });

  it("derives both GST workpaper bases from the same records", () => {
    expect(gstWorkpaper.nonCash).toEqual({
      g1Cents: invoice.totalCents,
      a1Cents: invoice.gstCents,
      b1Cents: purchase.gstCents,
      netCents: invoice.gstCents - purchase.gstCents,
    });
    const receivedGst = gstBreakdown(invoice.receivedCents, "remove").gstCents;
    expect(gstWorkpaper.cash).toEqual({
      g1Cents: invoice.receivedCents,
      a1Cents: receivedGst,
      b1Cents: purchase.gstCents,
      netCents: receivedGst - purchase.gstCents,
    });
    expect(purchase.payment.cents).toBe(purchase.totalCents);
  });

  it("reconciles the statement rows to its closing balance", () => {
    const movement = statement.rows.reduce((total, row) => total + row.cents, 0);
    expect(statement.closingCents).toBe(statement.openingCents + movement);
    const bank = trialBalance.rows.find((row) => row.account.code === accounts.bank.code);
    expect(statement.closingCents).toBe(bank?.debitCents);
    expect(statement.rows.map((row) => row.cents)).toEqual([-purchase.payment.cents, invoice.payment.cents]);
  });

  it("prices the staged MCP proposal from the reviewed purchase", () => {
    expect(proposedInvoice.costCents).toBe(purchase.subtotalCents);
    expect(proposedInvoice.subtotalCents).toBe((proposedInvoice.costCents * (100 + proposedInvoice.markupPercent)) / 100);
    expect(gstBreakdown(proposedInvoice.subtotalCents, "add")).toEqual({
      exclusiveCents: proposedInvoice.subtotalCents,
      gstCents: proposedInvoice.gstCents,
      inclusiveCents: proposedInvoice.totalCents,
    });
    expect(proposedInvoice.marginCents).toBe(proposedInvoice.subtotalCents - proposedInvoice.costCents);
    expect(proposedInvoice.sourceReference).toBe(purchase.reference);
  });
});
