export const MAX_REPORT_BYTES = 16 * 1024 * 1024;
interface CivilDate {
  readonly year: number;
  readonly month: number;
  readonly day: number;
}
interface Money {
  readonly currencyCode: string;
  readonly minorUnits: bigint;
}
export interface ReportOrganisation {
  readonly id: string;
  readonly version: bigint;
  readonly legalName: string;
  readonly displayName: string;
  readonly abn: string;
}
interface Context {
  readonly organisation: ReportOrganisation;
  readonly acquiredAt: string;
}
export interface IncomeSnapshot extends Context {
  readonly summary: {
    readonly organisationId: string;
    readonly periodStart?: CivilDate | undefined;
    readonly periodEnd?: CivilDate | undefined;
    readonly asOfDate?: CivilDate | undefined;
    readonly businessIncome?: Money | undefined;
    readonly invoiceIncome?: Money | undefined;
    readonly paymentsReceived?: Money | undefined;
    readonly outstanding?: Money | undefined;
    readonly invoiceGst?: Money | undefined;
    readonly unpostedIncome?: Money | undefined;
    readonly issuedCount: bigint;
    readonly unpostedIssuedCount: bigint;
    readonly gstBasis: number;
    readonly financialRevision: bigint;
  };
}
export interface BasSnapshot extends Context {
  readonly workpaper: {
    readonly id: string;
    readonly organisationId: string;
    readonly version: bigint;
    readonly periodStart?: CivilDate | undefined;
    readonly periodEnd?: CivilDate | undefined;
    readonly status: number;
    readonly salesG1?: Money | undefined;
    readonly gstOnSales1a?: Money | undefined;
    readonly gstCredits1b?: Money | undefined;
    readonly netGstPayable?: Money | undefined;
    readonly gstBasis: number;
    readonly createdAt?: { readonly seconds: bigint; readonly nanos: number } | undefined;
    readonly sourceFingerprint: Uint8Array;
    readonly sourcesChanged: boolean;
    readonly reviewWarnings: readonly string[];
    readonly excludedCashPurchaseCount: bigint;
    readonly salesSources: readonly {
      readonly invoiceId: string;
      readonly paymentId?: string | undefined;
      readonly invoiceNumber: string;
      readonly customerName: string;
      readonly taxDate?: CivilDate | undefined;
      readonly gross?: Money | undefined;
      readonly gst?: Money | undefined;
      readonly sourceRevision: bigint;
      readonly sourceHash: Uint8Array;
    }[];
    readonly sources: readonly {
      readonly documentId: string;
      readonly supplierName: string;
      readonly invoiceNumber: string;
      readonly documentDate?: CivilDate | undefined;
      readonly gross?: Money | undefined;
      readonly gstCredit?: Money | undefined;
    }[];
  };
}
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u;
function invalid(): never {
  throw new Error("INVALID_REPORT_SNAPSHOT");
}
function text(value: string, maximum: number, required = false) {
  if (
    typeof value !== "string" ||
    !value.isWellFormed() ||
    [...value].length > maximum ||
    (required && !value.trim())
  )
    invalid();
}
function identity(value: string) {
  if (!uuid.test(value)) invalid();
}
function uint(value: bigint, positive = false) {
  if (typeof value !== "bigint" || value < (positive ? 1n : 0n) || value > 18446744073709551615n)
    invalid();
}
export function reportDate(value: CivilDate | undefined): string {
  if (
    !value ||
    !Number.isInteger(value.year) ||
    value.year < 1 ||
    value.year > 9999 ||
    !Number.isInteger(value.month) ||
    !Number.isInteger(value.day)
  )
    invalid();
  const date = new Date(0);
  date.setUTCFullYear(value.year, value.month - 1, value.day);
  if (
    date.getUTCFullYear() !== value.year ||
    date.getUTCMonth() + 1 !== value.month ||
    date.getUTCDate() !== value.day
  )
    invalid();
  return `${String(value.year).padStart(4, "0")}-${String(value.month).padStart(2, "0")}-${String(value.day).padStart(2, "0")}`;
}
export function exactDecimal(value: bigint): string {
  if (typeof value !== "bigint" || value < -9223372036854775808n || value > 9223372036854775807n)
    invalid();
  const magnitude = value < 0n ? -value : value;
  return `${value < 0n ? "-" : ""}${magnitude / 100n}.${String(magnitude % 100n).padStart(2, "0")}`;
}
function money(value: Money | undefined, signed = false) {
  if (value?.currencyCode !== "AUD") invalid();
  const amount = exactDecimal(value.minorUnits);
  if (!signed && value.minorUnits < 0n) invalid();
  return amount;
}
function basis(value: number) {
  if (value === 1) return "Cash";
  if (value === 2) return "Non-cash";
  if (value === 0) return "Unconfigured";
  return invalid();
}
export function reportTimestamp(value: BasSnapshot["workpaper"]["createdAt"]) {
  if (
    !value ||
    typeof value.seconds !== "bigint" ||
    value.seconds < -62135596800n ||
    value.seconds > 253402300799n ||
    !Number.isInteger(value.nanos) ||
    value.nanos < 0 ||
    value.nanos > 999999999
  )
    invalid();
  const fraction =
    value.nanos === 0 ? "000" : String(value.nanos).padStart(9, "0").replace(/0+$/u, "");
  return new Date(Number(value.seconds) * 1000).toISOString().replace(/\.000Z$/u, `.${fraction}Z`);
}
function hash(value: Uint8Array, legacy = false) {
  if (!(value instanceof Uint8Array) || (value.length !== 32 && !(legacy && value.length === 0)))
    invalid();
  return [...value].map((byte) => byte.toString(16).padStart(2, "0")).join("");
}
function context(value: Context) {
  identity(value.organisation.id);
  uint(value.organisation.version, true);
  text(value.organisation.legalName, 256, true);
  text(value.organisation.displayName, 256, true);
  if (!/^[0-9]{11}$/.test(value.organisation.abn)) invalid();
  if (
    typeof value.acquiredAt !== "string" ||
    value.acquiredAt.length > 32 ||
    !Number.isFinite(Date.parse(value.acquiredAt)) ||
    new Date(value.acquiredAt).toISOString() !== value.acquiredAt
  )
    invalid();
}
function period(start?: CivilDate, end?: CivilDate) {
  if (reportDate(start) > reportDate(end)) invalid();
}
export function validateIncomeSnapshot(value: IncomeSnapshot): IncomeSnapshot {
  context(value);
  const report = value.summary;
  if (report.organisationId !== value.organisation.id) invalid();
  period(report.periodStart, report.periodEnd);
  reportDate(report.asOfDate);
  basis(report.gstBasis);
  uint(report.financialRevision);
  uint(report.issuedCount);
  uint(report.unpostedIssuedCount);
  for (const key of [
    "businessIncome",
    "invoiceIncome",
    "paymentsReceived",
    "outstanding",
    "invoiceGst",
    "unpostedIncome",
  ] as const)
    money(report[key], key === "businessIncome");
  return value;
}
export function validateBasSnapshot(value: BasSnapshot): BasSnapshot {
  context(value);
  const report = value.workpaper;
  identity(report.id);
  if (report.organisationId !== value.organisation.id || report.status !== 1) invalid();
  uint(report.version, true);
  period(report.periodStart, report.periodEnd);
  if (report.gstBasis !== 1 && report.gstBasis !== 2) invalid();
  reportTimestamp(report.createdAt);
  hash(report.sourceFingerprint, true);
  uint(report.excludedCashPurchaseCount);
  if (
    typeof report.sourcesChanged !== "boolean" ||
    !Array.isArray(report.reviewWarnings) ||
    report.reviewWarnings.length > 10
  )
    invalid();
  for (const warning of report.reviewWarnings) text(warning, 500);
  if (!Array.isArray(report.sources) || !Array.isArray(report.salesSources)) invalid();
  if (report.sources.length > 1000 || report.salesSources.length > 1000)
    throw new Error("REPORT_SOURCE_LIMIT");
  for (const key of ["salesG1", "gstOnSales1a", "gstCredits1b", "netGstPayable"] as const)
    money(report[key], key === "netGstPayable");
  for (const source of report.sources) {
    identity(source.documentId);
    text(source.supplierName, 256);
    text(source.invoiceNumber, 128);
    reportDate(source.documentDate);
    money(source.gross);
    money(source.gstCredit);
    if (source.gstCredit && source.gross && source.gstCredit.minorUnits > source.gross.minorUnits)
      invalid();
  }
  for (const source of report.salesSources) {
    identity(source.invoiceId);
    if (source.paymentId !== undefined) identity(source.paymentId);
    text(source.customerName, 200);
    text(source.invoiceNumber, 40);
    reportDate(source.taxDate);
    money(source.gross);
    money(source.gst);
    if (source.gst && source.gross && source.gst.minorUnits > source.gross.minorUnits) invalid();
    uint(source.sourceRevision, true);
    hash(source.sourceHash);
  }
  return value;
}
export function boundedReportBytes(bytes: Uint8Array) {
  if (!(bytes instanceof Uint8Array) || bytes.length === 0) throw new Error("REPORT_RENDER_FAILED");
  if (bytes.length > MAX_REPORT_BYTES) throw new Error("REPORT_SIZE_LIMIT");
  return bytes;
}
