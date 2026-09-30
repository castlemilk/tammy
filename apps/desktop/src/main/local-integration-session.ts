import { create, toJson } from "@bufbuild/protobuf";
import {
  GetTrialBalanceRequestSchema,
  GetTrialBalanceResponseSchema,
  ListAccountsRequestSchema,
  ListAccountsResponseSchema,
  ListJournalsRequestSchema,
  ListJournalsResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  GetBankingSummaryRequestSchema,
  GetBankingSummaryResponseSchema,
} from "@tammy/connect-client/tammy/v1/banking_pb.js";
import {
  type AuthenticationContext,
  AuthenticationContextSchema,
  CivilDateSchema,
  PageRequestSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  DocumentSchema,
  ListDocumentsRequestSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import {
  GetCurrentUserRequestSchema,
  GetCurrentUserResponseSchema,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  GetOrganisationRequestSchema,
  GetOrganisationResponseSchema,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import {
  GetAttentionSummaryRequestSchema,
  GetAttentionSummaryResponseSchema,
  ReportingPeriodSchema,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";
import {
  GetReportingCapabilityRequestSchema,
  GetReportingCapabilityResponseSchema,
  ReportingEntityType,
  ReportKind,
} from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import {
  GetSbrReadinessRequestSchema,
  GetSbrReadinessResponseSchema,
} from "@tammy/connect-client/tammy/v1/sbr_pb.js";
import {
  GetCurrentBasDraftRequestSchema,
  GetCurrentBasDraftResponseSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import type { ValidatedLocalApiRequest } from "@tammy/local-api/request";

import type { CoreClient } from "./core-client";

const UUID_V7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u;

export interface LocalIntegrationSession {
  readonly client: Readonly<CoreClient>;
  clear(): void;
  createReader(): Promise<(request: ValidatedLocalApiRequest) => Promise<unknown>>;
}

/** Only successful native app calls can establish the principal used by integrations. */
export function createLocalIntegrationSession(
  core: Readonly<CoreClient>,
  onInvalidate: () => void,
): LocalIntegrationSession {
  let generation = 0;
  let authentication: AuthenticationContext | undefined;
  let organisationId: string | undefined;
  const clear = () => {
    generation += 1;
    authentication = undefined;
    organisationId = undefined;
    onInvalidate();
  };
  const matches = (value: AuthenticationContext | undefined) =>
    authentication !== undefined &&
    value?.actorUserId === authentication.actorUserId &&
    value.sessionId === authentication.sessionId;
  const retainOrganisation = (auth: AuthenticationContext | undefined, id: string | undefined) => {
    if (!matches(auth) || !id || !UUID_V7.test(id)) return;
    if (organisationId !== undefined && organisationId !== id) {
      generation += 1;
      onInvalidate();
    }
    organisationId = id;
  };
  const client: Readonly<CoreClient> = Object.freeze({
    ...core,
    signIn: async (request: Parameters<CoreClient["signIn"]>[0]) => {
      clear();
      const started = generation;
      const result = await core.signIn(request);
      if (
        started === generation &&
        result.user &&
        result.session &&
        UUID_V7.test(result.user.id) &&
        UUID_V7.test(result.session.id) &&
        result.session.userId === result.user.id
      ) {
        authentication = create(AuthenticationContextSchema, {
          actorUserId: result.user.id,
          sessionId: result.session.id,
        });
      }
      return result;
    },
    unlockWorkspace: async (request: Parameters<CoreClient["unlockWorkspace"]>[0]) => {
      clear();
      return core.unlockWorkspace(request);
    },
    getOrganisation: async (request: Parameters<CoreClient["getOrganisation"]>[0]) => {
      const started = generation;
      const result = await core.getOrganisation(request);
      if (
        started === generation &&
        result.organisation &&
        result.organisation.id === request.organisationId
      ) {
        retainOrganisation(request.authentication, result.organisation.id);
      }
      return result;
    },
    createOrganisation: async (request: Parameters<CoreClient["createOrganisation"]>[0]) => {
      const started = generation;
      const result = await core.createOrganisation(request);
      if (started === generation) {
        retainOrganisation(request.commandContext?.authentication, result.organisation?.id);
      }
      return result;
    },
  });
  return {
    client,
    clear,
    createReader: async () => {
      if (!authentication || !organisationId) throw new Error("LOCAL_SESSION_REQUIRED");
      const started = generation;
      const principal = create(AuthenticationContextSchema, authentication);
      const organisation = organisationId;
      const [currentUser, currentOrganisation] = await Promise.all([
        core.getCurrentUser(create(GetCurrentUserRequestSchema, { authentication: principal })),
        core.getOrganisation(
          create(GetOrganisationRequestSchema, {
            authentication: principal,
            organisationId: organisation,
          }),
        ),
      ]);
      if (
        started !== generation ||
        currentUser.user?.id !== principal.actorUserId ||
        currentOrganisation.organisation?.id !== organisation
      ) {
        throw new Error("LOCAL_SESSION_REQUIRED");
      }
      return async (request) => {
        if (started !== generation) throw new Error("LOCAL_PAIRING_REVOKED");
        const target =
          request.operation === "tool.call" ? request.tool.target : request.resource.target;
        const result = await executeRead(
          core,
          principal,
          organisation,
          `${target.module}.${target.procedure}`,
        );
        if (started !== generation) throw new Error("LOCAL_PAIRING_REVOKED");
        return result;
      };
    },
  };
}

async function executeRead(
  client: Readonly<CoreClient>,
  authentication: AuthenticationContext,
  organisationId: string,
  target: string,
): Promise<unknown> {
  const context = { authentication, organisationId };
  const page = create(PageRequestSchema, { pageSize: 200 });
  const today = new Date();
  const parts = new Intl.DateTimeFormat("en", {
    timeZone: "Australia/Melbourne",
    year: "numeric",
    month: "numeric",
    day: "numeric",
  }).formatToParts(today);
  const part = (kind: string) => Number(parts.find(({ type }) => type === kind)?.value);
  const year = part("year"),
    month = part("month"),
    day = part("day");
  const asOfDate = create(CivilDateSchema, { year, month, day });
  switch (target) {
    case "system.getSystemDiagnostics":
      return client.getDiagnostics();
    case "organisation.getCurrentUser":
      return toJson(
        GetCurrentUserResponseSchema,
        await client.getCurrentUser(create(GetCurrentUserRequestSchema, { authentication })),
      );
    case "organisation.getOrganisation":
      return toJson(
        GetOrganisationResponseSchema,
        await client.getOrganisation(create(GetOrganisationRequestSchema, context)),
      );
    case "accounting.listAccounts":
      return toJson(
        ListAccountsResponseSchema,
        await client.listAccounts(create(ListAccountsRequestSchema, { ...context, page })),
      );
    case "accounting.listJournals":
      return toJson(
        ListJournalsResponseSchema,
        await client.listJournals(create(ListJournalsRequestSchema, { ...context, page })),
      );
    case "accounting.getTrialBalance":
      return toJson(
        GetTrialBalanceResponseSchema,
        await client.getTrialBalance(
          create(GetTrialBalanceRequestSchema, { ...context, asOfDate }),
        ),
      );
    case "banking.getBankingSummary":
      return toJson(
        GetBankingSummaryResponseSchema,
        await client.getBankingSummary(create(GetBankingSummaryRequestSchema, context)),
      );
    case "documents.listDocuments": {
      const result = await client.listDocuments(
        create(ListDocumentsRequestSchema, { ...context, page }),
      );
      return {
        documents: result.documents.map((document) => {
          const {
            extractedText: _text,
            candidate: _candidate,
            ...metadata
          } = toJson(DocumentSchema, document) as Record<string, unknown>;
          return metadata;
        }),
        ...(result.page
          ? {
              page: {
                returnedCount: result.page.returnedCount,
                ...(result.page.nextCursor ? { nextCursor: result.page.nextCursor } : {}),
              },
            }
          : {}),
      };
    }
    case "reporting.getCurrentBasDraft":
      return toJson(
        GetCurrentBasDraftResponseSchema,
        await client.getCurrentBasDraft(create(GetCurrentBasDraftRequestSchema, context)),
      );
    case "reporting.getAttentionSummary": {
      const quarterStart = Math.floor((month - 1) / 3) * 3 + 1;
      const quarterEnd = quarterStart + 2;
      const reportingPeriod = create(ReportingPeriodSchema, {
        startDate: { year, month: quarterStart, day: 1 },
        endDate: {
          year,
          month: quarterEnd,
          day: new Date(Date.UTC(year, quarterEnd, 0)).getUTCDate(),
        },
      });
      return toJson(
        GetAttentionSummaryResponseSchema,
        await client.getAttentionSummary(
          create(GetAttentionSummaryRequestSchema, { ...context, asOfDate, reportingPeriod }),
        ),
      );
    }
    case "reporting.getReportingCapability": {
      const taxYear = month > 6 ? year + 1 : year;
      const capabilities = await Promise.all(
        [ReportKind.GST_WORKPAPER, ReportKind.BAS, ReportKind.COMPANY_TAX_RETURN].map(
          async (report) =>
            toJson(
              GetReportingCapabilityResponseSchema,
              await client.getReportingCapability(
                create(GetReportingCapabilityRequestSchema, {
                  report,
                  taxYear,
                  entityType: ReportingEntityType.AU_PRIVATE_COMPANY,
                }),
              ),
            ),
        ),
      );
      return { capabilities };
    }
    case "sbr.getSbrReadiness":
      return toJson(
        GetSbrReadinessResponseSchema,
        await client.getSbrReadiness(create(GetSbrReadinessRequestSchema, { authentication })),
      );
    default:
      throw new Error("LOCAL_READ_FAILED");
  }
}
