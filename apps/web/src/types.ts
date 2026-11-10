import type { MinorUnits } from "./money";

export interface Session {
  state: "setup" | "recovery" | "locked" | "authenticated";
  setupId?: string;
  /** A pending setup's recovery confirmation deadline (ISO 8601, UTC), when known. */
  expiresAt?: string;
  organisation?: { id: string; name: string };
}

export interface ListPage {
  limit: number;
  returnedCount: number;
  mayHaveMore: boolean;
  cursorSupported: boolean;
}

export interface Overview {
  documentsNeedingReview: number;
  documentsReviewedInPeriod: number;
  bankingLinesNeedingReconciliation: number;
  bankingLinesUnreconciledInPeriod: number;
  /** The core's BAS preparation attention status for the current calendar year. */
  basStatus: string;
  currentDraftBasWorkpapers: number;
  attentionItems: { kind: string; label: string; resourceId?: string }[];
}

export interface Account {
  id: string;
  code: string;
  name: string;
  type: string;
  normalBalance: string;
  designation: "ordinary" | "system" | "control";
  active: boolean;
}

export interface JournalLine {
  id: string;
  accountId: string;
  accountCode: string;
  accountName: string;
  description: string;
  debitMinor: MinorUnits;
  creditMinor: MinorUnits;
}

export interface Journal {
  id: string;
  date: string | null;
  description: string;
  reference: string;
  state: string;
  lines: JournalLine[];
}

export interface TrialBalance {
  asOfDate: string;
  lines: {
    accountId: string;
    accountCode: string;
    accountName: string;
    debitMinor: MinorUnits;
    creditMinor: MinorUnits;
  }[];
  totalDebitsMinor: MinorUnits;
  totalCreditsMinor: MinorUnits;
}

export interface DocumentRecord {
  id: string;
  version: string;
  sourceDisplayName: string;
  status: string;
  supplierName: string;
  invoiceNumber: string;
  date: string | null;
  subtotalMinor: MinorUnits;
  gstMinor: MinorUnits;
  totalMinor: MinorUnits;
}

export interface BankLine {
  id: string;
  version: string;
  date: string;
  description: string;
  amountMinor: MinorUnits;
  status: string;
  matchReference?: string;
}

export interface BasWorkpaper {
  status: string;
  periodStart: string;
  periodEnd: string;
  salesG1Minor: MinorUnits | null;
  gstOnSales1aMinor: MinorUnits | null;
  gstCredits1bMinor: MinorUnits;
  netGstPayableMinor: MinorUnits | null;
  /**
   * The core's provisional GST of reviewed documents not posted as purchases, whatever their date;
   * never part of 1B.
   */
  candidateGstMinor: MinorUnits | null;
  /** Source records changed after the draft was created. */
  sourcesChanged: boolean;
  reviewWarnings: string[];
  limitations: string[];
  sources: {
    id: string;
    date: string;
    supplierName: string;
    invoiceNumber: string;
    grossMinor: MinorUnits;
    gstCreditMinor: MinorUnits;
  }[];
  candidates: {
    id: string;
    date: string | null;
    supplierName: string;
    invoiceNumber: string;
    candidateGstMinor: MinorUnits;
  }[];
}

export interface Insight {
  id: string;
  title: string;
  detail: string;
  kind: "documents" | "banking" | "bas";
  count?: number;
}

export interface CapabilitySummary {
  api: { available: boolean; scope: string; description: string };
  mcp: { available: boolean; transport: string; access: string; description: string };
  intelligence: { available: boolean; description: string };
}
