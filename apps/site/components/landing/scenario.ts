/*
 * One synthetic set of books, used by every illustrative scene on the landing
 * page so the figures agree from the hero to the GST workpaper.
 *
 * Everything here is invented. Amounts are whole AUD cents. Nothing is a
 * customer record, and no figure should ever be tweened or counted up.
 *
 * The books (non-cash GST basis, quarter 1 October to 31 December 2026):
 *   10 Oct  Manual journal   Dr 6100 Office expenses 200.00 / Cr 3100 Owner contributions 200.00
 *   14 Oct  Purchase EX-2041 Dr 6100 290.00, Dr 1150 29.00 / Cr 2000 319.00
 *   21 Oct  Purchase paid    Dr 2000 319.00 / Cr 1000 319.00
 *    3 Nov  Invoice INV-014  Dr 1100 1,320.00 / Cr 4000 1,200.00, Cr 2100 120.00
 *   12 Nov  Receipt          Dr 1000 500.00 / Cr 1100 500.00
 * Trial balance: debits 1,520.00 = credits 1,520.00.
 */

export const accounts = {
  bank: { code: "1000", name: "Business bank" },
  receivable: { code: "1100", name: "Accounts receivable" },
  gstReceivable: { code: "1150", name: "GST receivable current" },
  payable: { code: "2000", name: "Accounts payable" },
  gstPayable: { code: "2100", name: "GST payable current" },
  ownerContributions: { code: "3100", name: "Owner contributions" },
  sales: { code: "4000", name: "Invoice sales" },
  officeExpenses: { code: "6100", name: "Office expenses" },
} as const;

export type Account = (typeof accounts)[keyof typeof accounts];

export const business = {
  name: "Example Trading Co",
  basis: "Non-cash basis",
  quarter: "1 October to 31 December 2026",
  quarterShort: "Oct to Dec 2026",
} as const;

/** A supplier invoice that arrives as a PDF and becomes a posted purchase. */
export const purchase = {
  file: "example-supplies-invoice.pdf",
  fileKind: "PDF",
  supplier: "Example Supplies Pty Ltd",
  reference: "EX-2041",
  date: "14 October 2026",
  dateShort: "14 Oct 2026",
  subtotalCents: 29_000,
  gstCents: 2_900,
  totalCents: 31_900,
  lines: [
    { side: "debit", account: accounts.officeExpenses, cents: 29_000 },
    { side: "debit", account: accounts.gstReceivable, cents: 2_900 },
    { side: "credit", account: accounts.payable, cents: 31_900 },
  ],
  payment: { date: "21 October 2026", dateShort: "21 Oct 2026", cents: 31_900, reference: "EX-2041" },
} as const;

/** A second document still waiting for a person to check it. */
export const pendingDocument = {
  file: "example-couriers-receipt.jpeg",
  fileKind: "JPEG",
  supplier: "Example Couriers",
  reference: "EC-7731",
  dateShort: "5 Nov 2026",
  totalCents: 4_400,
} as const;

/** A sales invoice: issued, three lines posted together, then part paid. */
export const invoice = {
  number: "INV-014",
  customer: "Example Studio",
  issueDate: "3 November 2026",
  issueShort: "3 Nov 2026",
  dueDate: "17 November 2026",
  dueShort: "17 Nov 2026",
  line: {
    description: "Studio strategy workshop",
    quantity: "1",
    unitCents: 120_000,
    treatment: "Taxable · 10%",
  },
  subtotalCents: 120_000,
  gstCents: 12_000,
  totalCents: 132_000,
  issueLines: [
    { side: "debit", account: accounts.receivable, cents: 132_000 },
    { side: "credit", account: accounts.sales, cents: 120_000 },
    { side: "credit", account: accounts.gstPayable, cents: 12_000 },
  ],
  payment: { date: "12 November 2026", dateShort: "12 Nov 2026", cents: 50_000, reference: "EFT 5521" },
  receiptLines: [
    { side: "debit", account: accounts.bank, cents: 50_000 },
    { side: "credit", account: accounts.receivable, cents: 50_000 },
  ],
  receivedCents: 50_000,
  outstandingCents: 82_000,
} as const;

/** A draft staged through the local MCP connection, waiting for a person. */
export const proposedInvoice = {
  number: "INV-015",
  customer: "Example Studio",
  description: "Office supplies (on-charged)",
  costCents: 29_000,
  markupPercent: 20,
  subtotalCents: 34_800,
  gstCents: 3_480,
  totalCents: 38_280,
  marginCents: 5_800,
  sourceReference: "EX-2041",
} as const;

/** A manual journal between two ordinary accounts. */
export const manualJournal = {
  date: "10 October 2026",
  dateShort: "10 Oct 2026",
  description: "Studio supplies paid personally",
  cents: 20_000,
  debit: accounts.officeExpenses,
  credit: accounts.ownerContributions,
} as const;

/** Imported statement rows. Matching is done by a person, row by row. */
export const statement = {
  /* Opening balance nil, so the statement closes on the ledger's bank balance. */
  openingCents: 0,
  closingCents: 18_100,
  rows: [
    { date: "21 Oct 2026", description: "Example Supplies EX-2041", cents: -31_900, reference: "EX-2041" },
    { date: "12 Nov 2026", description: "Example Studio INV-014", cents: 50_000, reference: "INV-014" },
  ],
} as const;

/** Trial balance as at 12 November 2026. Debits equal credits. */
export const trialBalance = {
  asAt: "12 November 2026",
  rows: [
    { account: accounts.bank, debitCents: 18_100, creditCents: 0 },
    { account: accounts.receivable, debitCents: 82_000, creditCents: 0 },
    { account: accounts.gstReceivable, debitCents: 2_900, creditCents: 0 },
    { account: accounts.gstPayable, debitCents: 0, creditCents: 12_000 },
    { account: accounts.ownerContributions, debitCents: 0, creditCents: 20_000 },
    { account: accounts.sales, debitCents: 0, creditCents: 120_000 },
    { account: accounts.officeExpenses, debitCents: 49_000, creditCents: 0 },
  ],
  totalCents: 152_000,
} as const;

/** Overview, this quarter. */
export const overview = {
  businessIncomeCents: 120_000,
  invoiceIncomeCents: 120_000,
  paymentsReceivedCents: 50_000,
  invoicesOwingCents: 82_000,
} as const;

/**
 * The local GST workpaper for the quarter. G1 is GST-inclusive sales.
 * On the cash basis, sales and GST follow recorded payments instead: the
 * 500.00 received carries one-eleventh, 45.45, of GST.
 */
export const gstWorkpaper = {
  status: "Draft — not lodged",
  nonCash: { g1Cents: 132_000, a1Cents: 12_000, b1Cents: 2_900, netCents: 9_100 },
  cash: { g1Cents: 50_000, a1Cents: 4_545, b1Cents: 2_900, netCents: 1_645 },
} as const;
