import { addCalendarDays, aud, parseAudCents } from "./calculations";

export interface ForecastMovement {
  readonly id: string;
  readonly date: string;
  readonly description: string;
  readonly direction: "receipt" | "payment";
  readonly amountCents: number;
}
export interface ForecastWeek {
  readonly week: number;
  readonly start: string;
  readonly end: string;
  readonly openingCents: number;
  readonly receiptsCents: number;
  readonly paymentsCents: number;
  readonly closingCents: number;
}
export interface ForecastResult {
  readonly weeks: readonly ForecastWeek[];
  readonly lowestClosingCents: number;
  readonly totalReceiptsCents: number;
  readonly totalPaymentsCents: number;
  readonly endingCents: number;
}
const LIMIT = 100_000_000_000;

export function computeCashForecast(start: string, openingCents: number, movements: readonly ForecastMovement[]): ForecastResult {
  if (!addCalendarDays(start, 0)) throw new Error("Choose a valid forecast start date.");
  if (!Number.isSafeInteger(openingCents) || openingCents < 0 || openingCents > LIMIT)
    throw new Error("Enter a valid non-negative opening cash balance.");
  if (!Array.isArray(movements) || movements.length > 150) throw new Error("Maximum 150 forecast movements.");
  const seen = new Set<string>();
  const last = addCalendarDays(start, 83)!;
  for (const line of movements) {
    if (typeof line.id !== "string" || !/^[A-Za-z0-9_-]{1,50}$/.test(line.id) || seen.has(line.id))
      throw new Error("Each forecast row needs a distinct ID.");
    seen.add(line.id);
    if (!addCalendarDays(line.date, 0) || line.date < start || line.date > last)
      throw new Error("Every movement date must be within the 12-week forecast.");
    if (typeof line.description !== "string" || !line.description.trim() || line.description.length > 160)
      throw new Error("Enter a description of at most 160 characters for every movement.");
    if (!["receipt","payment"].includes(line.direction))
      throw new Error("Select receipt or payment for every row.");
    if (!Number.isSafeInteger(line.amountCents) || line.amountCents <= 0 || line.amountCents > LIMIT)
      throw new Error("Use a positive AUD amount in whole cents for each movement.");
  }
  const weeks: ForecastWeek[] = [];
  let closing = openingCents;
  let lowestClosingCents = openingCents, totalReceiptsCents = 0, totalPaymentsCents = 0;
  for (let i=0;i<12;i++) {
    const weekStart = addCalendarDays(start, i*7)!;
    const end = addCalendarDays(weekStart, 6)!;
    const here = movements.filter((line) => line.date >= weekStart && line.date <= end);
    const receiptsCents = here.filter(x=>x.direction==="receipt").reduce((sum,x)=>sum+x.amountCents,0);
    const paymentsCents = here.filter(x=>x.direction==="payment").reduce((sum,x)=>sum+x.amountCents,0);
    const result = closing+receiptsCents-paymentsCents;
    if (![closing,receiptsCents,paymentsCents,result].every(Number.isSafeInteger))
      throw new Error("Total forecast movements exceed the supported range.");
    const week: ForecastWeek = {
      week:i+1,start:weekStart,end,
      openingCents:closing, receiptsCents,paymentsCents,closingCents:result,
    };
    weeks.push(week);
    totalReceiptsCents += receiptsCents;
    totalPaymentsCents += paymentsCents;
    closing = result;
    lowestClosingCents = Math.min(lowestClosingCents,closing);
  }
  return { weeks,lowestClosingCents,totalReceiptsCents,totalPaymentsCents,endingCents:closing };
}

/** A valid CSV exported by the user's browser, without spreadsheet formula execution. */
function csvCell(value: string): string {
  const safe = /^[\s]*[=+@-]/.test(value) ? "'" + value : value;
  return '"' + safe.replaceAll('"','""').replaceAll("\r"," ").replaceAll("\n"," ") + '"';
}
export function forecastToCsv(start: string, opening: number, movements: readonly ForecastMovement[]): string {
  const result = computeCashForecast(start,opening,movements);
  const lines = [
    ["Tammy forecast","Illustrative, user-entered; not a posted ledger"],
    ["Start date",start],["Opening cash",aud(opening)],
    [],
    ["Movement date","Type","Description","AUD"],
    ...movements.map((item)=>[item.date,item.direction,item.description,aud(item.amountCents)]),
    [],
    ["Week","Start","End","Opening AUD","Receipts AUD","Payments AUD","Closing AUD"],
    ...result.weeks.map((week)=>[String(week.week),week.start,week.end,
      aud(week.openingCents),aud(week.receiptsCents),aud(week.paymentsCents),aud(week.closingCents)]),
  ];
  return lines.map((line)=>line.map(csvCell).join(",")).join("\r\n")+"\r\n";
}
export function exampleForecast(start: string): readonly ForecastMovement[] {
  if (!addCalendarDays(start,83)) throw new Error("Choose the forecast date first.");
  return [
    {id:"example-1",date:addCalendarDays(start,3)!,description:"Customer payment",direction:"receipt",amountCents:450000},
    {id:"example-2",date:addCalendarDays(start,5)!,description:"Supplier payment",direction:"payment",amountCents:220000},
    {id:"example-3",date:addCalendarDays(start,12)!,description:"Payroll and operating costs",direction:"payment",amountCents:620000},
    {id:"example-4",date:addCalendarDays(start,16)!,description:"Client invoice receipt",direction:"receipt",amountCents:800000},
  ];
}
export function parseForecastMoney(value: string): number {
  const cents = parseAudCents(value);
  if (cents === null) throw new Error("Enter a valid AUD amount with at most two decimal places.");
  return cents;
}
