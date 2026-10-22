import { z } from "zod";

const uuid = z
  .string()
  .regex(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u);
const uint = z
  .string()
  .regex(/^(?:0|[1-9][0-9]{0,19})$/u)
  .refine((value) => BigInt(value) <= 18446744073709551615n);
const version = uint.refine((value) => BigInt(value) > 0n);
const date = z
  .string()
  .regex(/^[0-9]{4}-[0-9]{2}-[0-9]{2}$/u)
  .refine((value) => {
    const parsed = new Date(`${value}T00:00:00Z`);
    return (
      value.slice(0, 4) !== "0000" &&
      Number.isFinite(parsed.getTime()) &&
      parsed.toISOString().slice(0, 10) === value
    );
  });
const common = {
  authentication: z.strictObject({ actorUserId: uuid, sessionId: uuid }),
  organisationId: uuid,
  expectedOrganisationVersion: version,
  format: z.enum(["pdf", "csv"]),
  periodStart: date,
  periodEnd: date,
};
export const incomeExportRequest = z
  .strictObject({ ...common, asOfDate: date, expectedFinancialRevision: uint })
  .refine((value) => value.periodStart <= value.periodEnd);
export const basExportRequest = z
  .strictObject({ ...common, expectedWorkpaperId: uuid, expectedWorkpaperVersion: version })
  .refine((value) => value.periodStart <= value.periodEnd);
const exactBasTarget = z.discriminatedUnion("kind", [
  z.strictObject({
    kind: z.literal("calculation"),
    id: uuid,
    revision: version,
    hash: z.string().regex(/^[0-9a-f]{64}$/u),
  }),
  z.strictObject({
    kind: z.literal("frozen_bas"),
    id: uuid,
    revision: z.literal("1"),
    hash: z.string().regex(/^[0-9a-f]{64}$/u),
  }),
]);
export const basPreparationExportRequest = z.strictObject({
  authentication: common.authentication,
  organisationId: common.organisationId,
  target: exactBasTarget,
  format: common.format,
});
export type IncomeExportRequest = z.infer<typeof incomeExportRequest>;
export type BasExportRequest = z.infer<typeof basExportRequest>;
export type BasPreparationExportRequest = z.infer<typeof basPreparationExportRequest>;
export type ReportFormat = "pdf" | "csv";
export interface ReportExports {
  exportIncome(request: IncomeExportRequest): Promise<{ readonly saved: boolean }>;
  exportBas(request: BasExportRequest): Promise<{ readonly saved: boolean }>;
  exportBasPreparation(request: BasPreparationExportRequest): Promise<{ readonly saved: boolean }>;
}
export const REPORT_EXPORT_ERRORS = [
  "REPORT_REFRESH_REQUIRED",
  "REPORT_SESSION_CHANGED",
  "REPORT_PDF_UNSUPPORTED_TEXT",
  "REPORT_SOURCE_LIMIT",
  "REPORT_SIZE_LIMIT",
  "REPORT_RENDER_FAILED",
  "REPORT_SAVE_FAILED",
] as const;
export function reportExportError(error: unknown, fallback: string): Error {
  const message = error instanceof Error ? error.message : "";
  return new Error(
    (REPORT_EXPORT_ERRORS as readonly string[]).includes(message) ? message : fallback,
  );
}
export function reportExportMessage(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error);
  if (message.includes("REPORT_REFRESH_REQUIRED"))
    return "The displayed report has changed. Refresh the report and organisation details, then export again.";
  if (message.includes("REPORT_SESSION_CHANGED"))
    return "The session changed. Unlock this workspace and refresh before exporting.";
  if (message.includes("REPORT_PDF_UNSUPPORTED_TEXT"))
    return "The PDF exporter cannot preserve some report text. Export CSV to preserve all characters.";
  if (message.includes("REPORT_SOURCE_LIMIT"))
    return "This report exceeds the supported source limit. No file was saved.";
  if (message.includes("REPORT_SIZE_LIMIT"))
    return "This report exceeds the 16 MiB export limit. No file was saved.";
  if (message.includes("REPORT_RENDER_FAILED"))
    return "The report could not be rendered. Try another format or refresh the report.";
  return "The report could not be saved. Choose a writable PDF or CSV file and try again.";
}
