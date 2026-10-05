import { create } from "@bufbuild/protobuf";
import type { AuthenticationContext } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  GetCurrentUserRequestSchema,
  UserState,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import { GetBusinessIncomeSummaryRequestSchema } from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { GetOrganisationRequestSchema } from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import { GetCurrentBasDraftRequestSchema } from "@tammy/connect-client/tammy/v1/tax_pb.js";
import {
  type BasSnapshot,
  boundedReportBytes,
  type IncomeSnapshot,
  renderBasCsv,
  renderBasPdf,
  renderIncomeCsv,
  renderIncomePdf,
  reportDate,
  validateBasSnapshot,
  validateIncomeSnapshot,
} from "@tammy/reporting";
import {
  type BasExportRequest,
  basExportRequest,
  type IncomeExportRequest,
  incomeExportRequest,
  type ReportExports,
  type ReportFormat,
  reportExportError,
} from "../shared/report-export";
import type { CoreClient } from "./core-client";
import { createValidatedExport } from "./validated-export";

type Reader = {
  readonly getCurrentUser: (
    request: Parameters<CoreClient["getCurrentUser"]>[0],
  ) => Promise<Pick<Awaited<ReturnType<CoreClient["getCurrentUser"]>>, "user">>;
  readonly getOrganisation: (
    request: Parameters<CoreClient["getOrganisation"]>[0],
  ) => Promise<Pick<Awaited<ReturnType<CoreClient["getOrganisation"]>>, "organisation">>;
  readonly getBusinessIncomeSummary: (
    request: Parameters<CoreClient["getBusinessIncomeSummary"]>[0],
  ) => Promise<Pick<Awaited<ReturnType<CoreClient["getBusinessIncomeSummary"]>>, "summary">>;
  readonly getCurrentBasDraft: (
    request: Parameters<CoreClient["getCurrentBasDraft"]>[0],
  ) => Promise<Pick<Awaited<ReturnType<CoreClient["getCurrentBasDraft"]>>, "workpaper">>;
};
function civil(value: string) {
  const [year, month, day] = value.split("-").map(Number);
  return { year: year ?? 0, month: month ?? 0, day: day ?? 0 };
}
function freeze<T>(value: T): T {
  if (value && typeof value === "object" && !(value instanceof Uint8Array)) {
    for (const child of Object.values(value)) freeze(child);
    Object.freeze(value);
  }
  return value;
}
export function createReportExports(options: {
  readonly client: Reader;
  readonly capture: (authentication: AuthenticationContext, organisationId: string) => () => void;
  readonly select: (
    filename: string,
    format: ReportFormat,
  ) => Promise<{ readonly canceled: boolean; readonly filePath?: string }>;
  readonly now?: () => Date;
  readonly renderIncome?: (snapshot: IncomeSnapshot, format: ReportFormat) => Promise<Uint8Array>;
  readonly renderBas?: (snapshot: BasSnapshot, format: ReportFormat) => Promise<Uint8Array>;
}): ReportExports {
  const guardFor = (request: IncomeExportRequest | BasExportRequest) => {
    try {
      const captured = options.capture(
        createAuthentication(request.authentication),
        request.organisationId,
      );
      return () => {
        try {
          captured();
        } catch {
          throw new Error("REPORT_SESSION_CHANGED");
        }
      };
    } catch {
      throw new Error("REPORT_SESSION_CHANGED");
    }
  };
  const revalidateFor =
    (request: IncomeExportRequest | BasExportRequest, guard: () => void) => async () => {
      try {
        const result = await options.client.getCurrentUser(
          create(GetCurrentUserRequestSchema, { authentication: request.authentication }),
        );
        guard();
        if (
          result.user?.id !== request.authentication.actorUserId ||
          result.user.state !== UserState.ACTIVE
        )
          throw new Error();
      } catch {
        throw new Error("REPORT_SESSION_CHANGED");
      }
    };
  const profile = async (request: IncomeExportRequest | BasExportRequest, guard: () => void) => {
    const result = await options.client.getOrganisation(
      create(GetOrganisationRequestSchema, {
        authentication: request.authentication,
        organisationId: request.organisationId,
      }),
    );
    guard();
    if (
      !result.organisation ||
      result.organisation.id !== request.organisationId ||
      result.organisation.version !== BigInt(request.expectedOrganisationVersion)
    )
      throw new Error("REPORT_REFRESH_REQUIRED");
    return structuredClone(result.organisation);
  };
  const prepareIncome = async (input: IncomeExportRequest) => {
    const request = freeze(structuredClone(parseIncome(input)));
    const guard = guardFor(request);
    try {
      const organisation = await profile(request, guard);
      const result = await options.client.getBusinessIncomeSummary(
        create(GetBusinessIncomeSummaryRequestSchema, {
          authentication: request.authentication,
          organisationId: request.organisationId,
          periodStart: civil(request.periodStart),
          periodEnd: civil(request.periodEnd),
          asOfDate: civil(request.asOfDate),
        }),
      );
      guard();
      const summary = structuredClone(result.summary);
      await profile(request, guard);
      if (
        !summary ||
        summary.organisationId !== request.organisationId ||
        summary.financialRevision !== BigInt(request.expectedFinancialRevision) ||
        reportDate(summary.periodStart) !== request.periodStart ||
        reportDate(summary.periodEnd) !== request.periodEnd ||
        reportDate(summary.asOfDate) !== request.asOfDate
      )
        throw new Error("REPORT_REFRESH_REQUIRED");
      const snapshot = freeze(
        validateIncomeSnapshot({
          organisation,
          summary,
          acquiredAt: (options.now?.() ?? new Date()).toISOString(),
        }),
      );
      return {
        snapshot,
        guard,
        revalidateAuthorisation: revalidateFor(request, guard),
        extension: request.format,
        filename: `income-summary-${request.periodStart}-${request.periodEnd}.${request.format}`,
      };
    } catch (error) {
      throw reportExportError(error, "REPORT_REFRESH_REQUIRED");
    }
  };
  const prepareBas = async (input: BasExportRequest) => {
    const request = freeze(structuredClone(parseBas(input)));
    const guard = guardFor(request);
    try {
      const organisation = await profile(request, guard);
      const result = await options.client.getCurrentBasDraft(
        create(GetCurrentBasDraftRequestSchema, {
          authentication: request.authentication,
          organisationId: request.organisationId,
        }),
      );
      guard();
      const workpaper = structuredClone(result.workpaper);
      await profile(request, guard);
      if (
        !workpaper ||
        workpaper.organisationId !== request.organisationId ||
        workpaper.id !== request.expectedWorkpaperId ||
        workpaper.version !== BigInt(request.expectedWorkpaperVersion) ||
        reportDate(workpaper.periodStart) !== request.periodStart ||
        reportDate(workpaper.periodEnd) !== request.periodEnd
      )
        throw new Error("REPORT_REFRESH_REQUIRED");
      const snapshot = freeze(
        validateBasSnapshot({
          organisation,
          workpaper,
          acquiredAt: (options.now?.() ?? new Date()).toISOString(),
        }),
      );
      return {
        snapshot,
        guard,
        revalidateAuthorisation: revalidateFor(request, guard),
        extension: request.format,
        filename: `gst-workpaper-${request.periodStart}-${request.periodEnd}.${request.format}`,
      };
    } catch (error) {
      throw reportExportError(error, "REPORT_REFRESH_REQUIRED");
    }
  };
  const shared = {
    select: options.select,
    validateBytes: (bytes: Uint8Array, format: ReportFormat) => {
      boundedReportBytes(bytes);
      if (
        format === "pdf" &&
        (bytes.length < 8 || new TextDecoder().decode(bytes.subarray(0, 5)) !== "%PDF-")
      )
        throw new Error("REPORT_RENDER_FAILED");
    },
    renderError: (error: unknown) => reportExportError(error, "REPORT_RENDER_FAILED"),
    saveError: (error: unknown) => reportExportError(error, "REPORT_SAVE_FAILED"),
  };
  const income = createValidatedExport({
    ...shared,
    prepare: prepareIncome,
    render:
      options.renderIncome ??
      (async (snapshot: IncomeSnapshot, format: ReportFormat) =>
        format === "pdf" ? renderIncomePdf(snapshot) : renderIncomeCsv(snapshot)),
  });
  const bas = createValidatedExport({
    ...shared,
    prepare: prepareBas,
    render:
      options.renderBas ??
      (async (snapshot: BasSnapshot, format: ReportFormat) =>
        format === "pdf" ? renderBasPdf(snapshot) : renderBasCsv(snapshot)),
  });
  return { exportIncome: income.export, exportBas: bas.export };
}
function createAuthentication(value: IncomeExportRequest["authentication"]): AuthenticationContext {
  return { $typeName: "tammy.v1.AuthenticationContext", ...value };
}

function parseIncome(input: IncomeExportRequest) {
  const parsed = incomeExportRequest.safeParse(input);
  if (!parsed.success) throw new Error("REPORT_REFRESH_REQUIRED");
  return parsed.data;
}
function parseBas(input: BasExportRequest) {
  const parsed = basExportRequest.safeParse(input);
  if (!parsed.success) throw new Error("REPORT_REFRESH_REQUIRED");
  return parsed.data;
}
