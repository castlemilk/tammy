/** Narrow Simpler BAS teaching workpaper, not an ATO lodgement or a tax engine.
 * User is responsible for source records, GST attribution and eligible credits.
 * Values supplied are already GST-inclusive AUD cents for standard-taxable
 * supplies; non-taxable G1 sales have been separately classified by the user.
 */
import { gstBreakdown } from "./calculations";

export interface SimpleBasInputs {
  readonly taxableSalesInclusiveCents: number;
  readonly otherReportableSalesCents: number;
  readonly creditablePurchasesInclusiveCents: number;
}
const MAX = 100_000_000_000;
function valid(cents: number) {
  return Number.isSafeInteger(cents) && cents >= 0 && cents <= MAX;
}
export interface SimpleBasResult {
  readonly totalSalesCents: number;
  readonly gstOnSalesCents: number;
  readonly eligibleGstCreditsCents: number;
  readonly netGstCents: number;
  readonly entryG1Whole: number;
  readonly entry1AWhole: number;
  readonly entry1BWhole: number;
  readonly entryNetWhole: number;
}

export function prepareSimpleBas(input: SimpleBasInputs): SimpleBasResult {
  const { taxableSalesInclusiveCents, otherReportableSalesCents,
    creditablePurchasesInclusiveCents } = input;
  if (![taxableSalesInclusiveCents, otherReportableSalesCents,
    creditablePurchasesInclusiveCents].every(valid)) {
    throw new Error("Enter valid, non-negative AUD amounts in whole cents.");
  }
  const totalSalesCents = taxableSalesInclusiveCents + otherReportableSalesCents;
  if (!Number.isSafeInteger(totalSalesCents) || totalSalesCents > MAX) {
    throw new Error("Total reportable sales exceed this workpaper's supported range.");
  }
  const gstOnSalesCents = gstBreakdown(taxableSalesInclusiveCents, "remove").gstCents;
  const eligibleGstCreditsCents = gstBreakdown(creditablePurchasesInclusiveCents, "remove").gstCents;
  const entryG1Whole = Math.trunc(totalSalesCents / 100);
  const entry1AWhole = Math.trunc(gstOnSalesCents / 100);
  const entry1BWhole = Math.trunc(eligibleGstCreditsCents / 100);
  return {
    totalSalesCents,
    gstOnSalesCents,
    eligibleGstCreditsCents,
    netGstCents: gstOnSalesCents - eligibleGstCreditsCents,
    entryG1Whole, entry1AWhole, entry1BWhole,
    entryNetWhole: entry1AWhole - entry1BWhole,
  };
}

/** Due-date reference, NOT the personalised due date shown by the ATO.
 * Does not apply online/agent extensions or next-business-day holiday rules.
 */
export type BasQuarter = 1 | 2 | 3 | 4;
export interface BasQuarterReference {
  readonly financialYear: string;
  readonly quarter: BasQuarter;
  readonly period: string;
  readonly standardDueDate: string;
  readonly note: string;
}
export function quarterlyBasReference(startYear: number, quarter: BasQuarter): BasQuarterReference {
  if (!Number.isInteger(startYear) || startYear < 2020 || startYear > 2035 ||
    ![1, 2, 3, 4].includes(quarter)) {
    throw new Error("Select a supported financial year and BAS quarter.");
  }
  const finish = startYear + 1;
  const data: Record<BasQuarter, readonly [string, string]> = {
    1: ["July–September", String(startYear) + "-10-28"],
    2: ["October–December", String(finish) + "-02-28"],
    3: ["January–March", String(finish) + "-04-28"],
    4: ["April–June", String(finish) + "-07-28"],
  };
  const [period, standardDueDate] = data[quarter];
  return {
    financialYear: startYear + "–" + finish,
    quarter,
    period,
    standardDueDate,
    note: "Standard reference date only. Your ATO statement may have a later due date for online/agent lodgement, weekends, public holidays or other circumstances.",
  };
}
