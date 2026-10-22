import { clone, create, type DescMessage, toBinary } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import {
  AcknowledgeBasWarningsRequestSchema,
  AcknowledgeBasWarningsResponseSchema,
  BasAmountInputSchema,
  BasAmountState,
  BasApplicability,
  BasCadence,
  BasCivilDateRangeSchema,
  type BasConfirmedPeriod,
  BasCorrectionKind,
  BasDisputeOverlayState,
  BasDisputeResolutionOutcome,
  BasDueDateKind,
  BasEffectiveIntervalSchema,
  BasExpectedIdentitySchema,
  type BasExportEventSummary,
  BasExportFormat,
  BasExportOutcome,
  BasExportPhase,
  type BasExportTarget,
  BasExportTargetSchema,
  BasExternalChannel,
  BasExternalDisputeInputSchema,
  type BasExternalDisputeResolutionSummary,
  BasExternalEventKind,
  BasExternalObservationCorrectionInputSchema,
  BasExternalObservationInputSchema,
  type BasExternalObservationSummary,
  BasFindingSeverity,
  type BasFrozenBasSummary,
  BasGstBasis,
  BasGstMethod,
  BasGstProfileInputSchema,
  type BasIncomeYearIdentity,
  BasIncomeYearIdentitySchema,
  type BasLabelTrace,
  BasLiveCurrentness,
  BasNoticeOperativeStatus,
  BasObligationAnswerSchema,
  type BasObligationsProfile,
  BasObligationsProfileInputSchema,
  type BasObligationsProfileSummary,
  BasOtherObligationInventorySchema,
  BasPaygiInputsSchema,
  BasPaygiMethod,
  BasPaygiProfileInputSchema,
  BasPaygiUnsupportedState,
  BasPaygwCategory,
  BasPaygwInputsSchema,
  BasPaygwProfileInputSchema,
  BasPeriodConfirmationInputSchema,
  type BasPeriodSuggestion,
  type BasPeriodSummary,
  BasPeriodSupportState,
  BasPrefillRequirement,
  type BasPreparation,
  BasPreparationState,
  type BasPreparationSummary,
  BasPriorVariationDisposition,
  BasRegistrationStatus,
  BasT1ReviewDecision,
  BasTurnoverCategory,
  type BasValidationFinding,
  BasValidationFindingSchema,
  BasWarningAcknowledgementInputSchema,
  ConfirmBasPeriodRequestSchema,
  ConfirmBasPeriodResponseSchema,
  CorrectExternalBasObservationRequestSchema,
  CorrectExternalBasObservationResponseSchema,
  CreateBasReplacementRequestSchema,
  CreateBasReplacementResponseSchema,
  CreateBasRevisionRequestSchema,
  CreateBasRevisionResponseSchema,
  FreezeBasForManualHandoffRequestSchema,
  FreezeBasForManualHandoffResponseSchema,
  GetBasCalendarRequestSchema,
  GetBasCalendarResponseSchema,
  GetBasObligationsProfileRequestSchema,
  GetBasObligationsProfileResponseSchema,
  GetBasPreparationRequestSchema,
  GetBasPreparationResponseSchema,
  GetFrozenBasRequestSchema,
  GetFrozenBasResponseSchema,
  ListBasExportEventsRequestSchema,
  ListBasExportEventsResponseSchema,
  ListBasExternalDisputeResolutionsRequestSchema,
  ListBasExternalDisputeResolutionsResponseSchema,
  ListBasExternalObservationsRequestSchema,
  ListBasExternalObservationsResponseSchema,
  ListBasObligationsProfilesRequestSchema,
  ListBasObligationsProfilesResponseSchema,
  ListBasPeriodsRequestSchema,
  ListBasPeriodsResponseSchema,
  ListBasPreparationsRequestSchema,
  ListBasPreparationsResponseSchema,
  ListFrozenBasRequestSchema,
  ListFrozenBasResponseSchema,
  RecordExternalBasObservationRequestSchema,
  RecordExternalBasObservationResponseSchema,
  RegenerateBasPreparationRequestSchema,
  RegenerateBasPreparationResponseSchema,
  ReplaceRejectedBasAttemptRequestSchema,
  ReplaceRejectedBasAttemptResponseSchema,
  ResolveExternalBasDisputeRequestSchema,
  ResolveExternalBasDisputeResponseSchema,
  SaveBasPaygInputsRequestSchema,
  SaveBasPaygInputsResponseSchema,
  SetBasObligationsProfileRequestSchema,
  SetBasObligationsProfileResponseSchema,
  ValidateBasPreparationRequestSchema,
  ValidateBasPreparationResponseSchema,
} from "@tammy/connect-client/tammy/v1/bas_preparation_pb.js";
import {
  CommandContextSchema,
  MoneySchema,
  PageRequestSchema,
  SourceRefSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  GetCurrentUserRequestSchema,
  GetCurrentUserResponseSchema,
  Role,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { TammyConveyorModule } from "../../../conveyor/client";
import { isBasPreparationCorrectionCode } from "../../../shared/bas-preparation-errors";
import { BAS_PREPARATION_RESPONSE_FRAME_LIMIT } from "../../../shared/bas-preparation-ipc";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import {
  type BasPreparationExportRequest,
  reportExportMessage,
} from "../../../shared/report-export";
import { Button } from "../../components/ui/button";
import { assertFreshFactor, authentication, uuidV7 } from "../sbr/sbr-form-support";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import { LegacyRuleReferenceRepair } from "./legacy-rule-reference-repair";

type Api = TammyConveyorModule<"basPreparation">;
type ExportApi = Pick<TammyConveyorModule<"reporting">, "exportBasPreparation">;
type IdentityApi = Pick<
  TammyConveyorModule<"organisation">,
  "assertTotp" | "getCurrentUser" | "getOrganisation" | "updateOrganisation"
>;
type Ref = {
  readonly type: string;
  readonly id: string;
  readonly revision: bigint;
  readonly contentHash: Uint8Array;
};
type EvidenceFields = {
  readonly type: string;
  readonly id: string;
  readonly revision: string;
  readonly hash: string;
};
type InitialPreparationTarget = {
  readonly id: string;
  readonly revision: bigint;
  readonly hash: string;
};
type Stage = "Prepare" | "Review" | "Freeze" | "Export" | "External record";

function queryCodec<Input extends DescMessage, Output extends DescMessage>(
  input: Input,
  output: Output,
) {
  return createProtoMethodCodec<Input, Output>({
    input,
    output,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: BAS_PREPARATION_RESPONSE_FRAME_LIMIT,
  });
}
function mutationCodec<Input extends DescMessage, Output extends DescMessage>(
  input: Input,
  output: Output,
) {
  return createProtoMethodCodec<Input, Output>({
    input,
    output,
    maximumRequestBytes: Math.floor(1.1 * 1024 * 1024),
    maximumResponseBytes: BAS_PREPARATION_RESPONSE_FRAME_LIMIT,
  });
}
const profilesCodec = queryCodec(
  ListBasObligationsProfilesRequestSchema,
  ListBasObligationsProfilesResponseSchema,
);
const periodsCodec = queryCodec(ListBasPeriodsRequestSchema, ListBasPeriodsResponseSchema);
const preparationsCodec = queryCodec(
  ListBasPreparationsRequestSchema,
  ListBasPreparationsResponseSchema,
);
const frozenCodec = queryCodec(ListFrozenBasRequestSchema, ListFrozenBasResponseSchema);
const calendarCodec = queryCodec(GetBasCalendarRequestSchema, GetBasCalendarResponseSchema);
const profileCodec = queryCodec(
  GetBasObligationsProfileRequestSchema,
  GetBasObligationsProfileResponseSchema,
);
const preparationCodec = queryCodec(
  GetBasPreparationRequestSchema,
  GetBasPreparationResponseSchema,
);
const frozenDetailCodec = queryCodec(GetFrozenBasRequestSchema, GetFrozenBasResponseSchema);
const observationsCodec = queryCodec(
  ListBasExternalObservationsRequestSchema,
  ListBasExternalObservationsResponseSchema,
);
const disputesCodec = queryCodec(
  ListBasExternalDisputeResolutionsRequestSchema,
  ListBasExternalDisputeResolutionsResponseSchema,
);
const exportEventsCodec = queryCodec(
  ListBasExportEventsRequestSchema,
  ListBasExportEventsResponseSchema,
);
const setProfileCodec = mutationCodec(
  SetBasObligationsProfileRequestSchema,
  SetBasObligationsProfileResponseSchema,
);
const confirmPeriodCodec = mutationCodec(
  ConfirmBasPeriodRequestSchema,
  ConfirmBasPeriodResponseSchema,
);
const regenerateCodec = mutationCodec(
  RegenerateBasPreparationRequestSchema,
  RegenerateBasPreparationResponseSchema,
);
const savePaygCodec = mutationCodec(
  SaveBasPaygInputsRequestSchema,
  SaveBasPaygInputsResponseSchema,
);
const validateCodec = mutationCodec(
  ValidateBasPreparationRequestSchema,
  ValidateBasPreparationResponseSchema,
);
const acknowledgeCodec = mutationCodec(
  AcknowledgeBasWarningsRequestSchema,
  AcknowledgeBasWarningsResponseSchema,
);
const freezeCodec = mutationCodec(
  FreezeBasForManualHandoffRequestSchema,
  FreezeBasForManualHandoffResponseSchema,
);
const replacementCodec = mutationCodec(
  CreateBasReplacementRequestSchema,
  CreateBasReplacementResponseSchema,
);
const revisionCodec = mutationCodec(
  CreateBasRevisionRequestSchema,
  CreateBasRevisionResponseSchema,
);
const recordObservationCodec = mutationCodec(
  RecordExternalBasObservationRequestSchema,
  RecordExternalBasObservationResponseSchema,
);
const correctObservationCodec = mutationCodec(
  CorrectExternalBasObservationRequestSchema,
  CorrectExternalBasObservationResponseSchema,
);
const resolveDisputeCodec = mutationCodec(
  ResolveExternalBasDisputeRequestSchema,
  ResolveExternalBasDisputeResponseSchema,
);
const rejectedReplacementCodec = mutationCodec(
  ReplaceRejectedBasAttemptRequestSchema,
  ReplaceRejectedBasAttemptResponseSchema,
);
const currentUserCodec = queryCodec(GetCurrentUserRequestSchema, GetCurrentUserResponseSchema);

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const HASH = /^[0-9a-f]{64}$/;
const OTHER_OBLIGATIONS = [
  ["fringeBenefitsTax", "Fringe benefits tax"],
  ["wineEqualisationTax", "Wine equalisation tax"],
  ["luxuryCarTax", "Luxury car tax"],
  ["fuelTaxCredits", "Fuel tax credits"],
  ["deferredCompanyInstalments", "Deferred company instalments"],
  ["deferredOrImportGst", "Deferred or import GST"],
  ["specialGstSchemesOrAdjustments", "Special GST schemes or adjustments"],
  ["otherActivityStatementObligations", "Other activity statement obligations"],
] as const;

function civil(value: string) {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  if (!match) throw new Error("invalid date");
  return { year: Number(match[1]), month: Number(match[2]), day: Number(match[3]) };
}
function iso(
  value: { readonly year: number; readonly month: number; readonly day: number } | undefined,
) {
  if (!value) return "—";
  return `${String(value.year).padStart(4, "0")}-${String(value.month).padStart(2, "0")}-${String(value.day).padStart(2, "0")}`;
}
function hex(bytes: Uint8Array | undefined) {
  return bytes ? [...bytes].map((byte) => byte.toString(16).padStart(2, "0")).join("") : "";
}
function fromHex(value: string): Uint8Array {
  if (!HASH.test(value)) throw new Error("Enter the complete SHA-256 hash of the retained source.");
  return Uint8Array.from(value.match(/.{2}/gu) ?? [], (pair) => Number.parseInt(pair, 16));
}
function exactRef(value: Ref) {
  return create(SourceRefSchema, value);
}
function refKey(value: Ref | undefined) {
  return value ? `${value.type}:${value.id}:${value.revision}:${hex(value.contentHash)}` : "";
}
function matchesFrozenTarget(
  preparation: BasPreparation | undefined,
  frozen: BasFrozenBasSummary | undefined,
) {
  // The frozen owner pins the reviewed preparation before freeze advances its
  // mutable version. Bind the current owner through its exact frozen ref.
  return Boolean(
    preparation?.frozenBas &&
      frozen?.frozenBas &&
      frozen.preparation?.id === preparation.preparationId &&
      refKey(preparation.frozenBas) === refKey(frozen.frozenBas),
  );
}
function profileRef(value: BasObligationsProfile): Ref {
  return {
    type: "bas_obligations_profile",
    id: value.profileId,
    revision: value.revision,
    contentHash: value.contentHash,
  };
}
function periodRef(value: BasConfirmedPeriod): Ref {
  return {
    type: "bas_period",
    id: value.periodId,
    revision: value.version,
    contentHash: value.contentHash,
  };
}
function preparationRef(value: BasPreparation): Ref {
  return {
    type: "bas_preparation",
    id: value.preparationId,
    revision: value.version,
    contentHash: value.contentHash,
  };
}
function hashField(label: string, bytes: Uint8Array | undefined) {
  return `${label}: ${hex(bytes) || "not available"}`;
}
function parseEvidence(fields: EvidenceFields): Ref[] {
  if (!fields.id && !fields.revision && !fields.hash && !fields.type) return [];
  if (!/^[a-z][a-z0-9_]{0,63}$/.test(fields.type) || !UUID.test(fields.id))
    throw new Error("Evidence must use the exact retained source type and UUID.");
  const revision = BigInt(fields.revision);
  if (revision < 1n || revision > 18446744073709551615n)
    throw new Error("Evidence revision must be a positive integer.");
  return [
    create(SourceRefSchema, {
      type: fields.type,
      id: fields.id,
      revision,
      contentHash: fromHex(fields.hash),
    }),
  ];
}
function paygiInputError(
  code:
    | "PAYGI_PROFILE_IDENTITY_UNVERIFIED"
    | "PAYGI_PROFILE_YEAR_MISSING"
    | "PAYGI_PROFILE_YEAR_MISMATCH",
) {
  return Object.assign(new Error(), { code });
}
function errorCopy(error: unknown) {
  const localCode =
    typeof error === "object" && error !== null && "code" in error && typeof error.code === "string"
      ? error.code
      : undefined;
  switch (localCode) {
    case "PAYGI_PROFILE_IDENTITY_UNVERIFIED":
      return "The calculation's retained profile identity could not be verified. Refresh the exact preparation before saving PAYG inputs.";
    case "PAYGI_PROFILE_YEAR_MISSING":
      return "PAYGI inputs require the exact confirmed income year in this calculation's retained profile. Select a calculation with a confirmed profile year or save the profile first.";
    case "PAYGI_PROFILE_YEAR_MISMATCH":
      return "PAYGI income-year fields differ from the retained profile. Restore the exact profile year or save a corrected profile before saving PAYG inputs.";
  }
  const code =
    typeof error === "object" &&
    error !== null &&
    "code" in error &&
    isBasPreparationCorrectionCode(error.code)
      ? error.code
      : undefined;
  switch (code) {
    case "BAS_SOURCE_CAPACITY":
      return "The complete BAS source exceeds the supported capture capacity. Retain the actual statement period and review source records or contact support.";
    case "BAS_CONFLICT":
      return "Core could not accept this BAS action. Refresh the exact history and review the current findings before retrying.";
    case "BAS_IDEMPOTENCY_CONFLICT":
      return "This BAS action key was already used for a different request. Refresh the history and start a new action.";
    case "BAS_PERMISSION_DENIED":
      return "Your current BAS role does not allow this action. Ask a workspace administrator to review your assigned roles.";
    case "BAS_AUTHENTICATION_REQUIRED":
      return "Your session could not be verified for this BAS action. Sign in again, then refresh the exact history.";
    case "BAS_REQUEST_TIMEOUT":
      return "The BAS request timed out. Refresh the exact history before retrying to check whether it was saved.";
    default:
      return "The BAS change was not confirmed. Refresh the exact history and review blockers before retrying.";
  }
}
function amountInput(state: BasAmountState, amount: string, evidence: Ref[], reason: string) {
  const parsed =
    state === BasAmountState.KNOWN || state === BasAmountState.KNOWN_ZERO
      ? parseAudCents(amount)
      : undefined;
  if (
    (state === BasAmountState.KNOWN || state === BasAmountState.KNOWN_ZERO) &&
    parsed === undefined
  )
    throw new Error("Enter an exact nonnegative AUD amount for a known label.");
  return create(BasAmountInputSchema, {
    state,
    ...(parsed === undefined
      ? {}
      : { amount: create(MoneySchema, { currencyCode: "AUD", minorUnits: parsed }) }),
    evidence,
    reason,
  });
}
function parseAudCents(value: string) {
  if (!/^(?:0|[1-9][0-9]{0,12})(?:\.[0-9]{1,2})?$/.test(value)) return undefined;
  const [units = "0", fraction = ""] = value.split(".");
  const cents = BigInt(units) * 100n + BigInt(fraction.padEnd(2, "0"));
  return cents;
}
function roleLabel(roles: readonly Role[] | undefined) {
  if (roles?.includes(Role.BUSINESS_LODGER)) return "Business lodger approval available";
  if (roles?.includes(Role.BUSINESS_PREPARER)) return "Business preparer · review and preparation";
  if (roles?.includes(Role.WORKSPACE_ADMIN)) return "Workspace admin · prepare and read only";
  return "Read only · ask a workspace admin to assign BAS roles";
}

export async function readAllPages<T>(options: {
  readonly call: (frame: Uint8Array) => Promise<Uint8Array>;
  readonly encode: (cursor?: string) => Uint8Array;
  readonly decode: (frame: Uint8Array) => {
    readonly values: readonly T[];
    readonly nextCursor?: string | undefined;
  };
}): Promise<T[]> {
  const values: T[] = [];
  const cursors = new Set<string>();
  let cursor: string | undefined;
  for (let page = 0; page < 1000; page += 1) {
    const response = options.decode(await options.call(options.encode(cursor)));
    values.push(...response.values);
    const next = response.nextCursor;
    if (!next) return values;
    if (cursors.has(next)) throw new Error("invalid page cursor");
    cursors.add(next);
    cursor = next;
  }
  throw new Error("page limit exceeded");
}

export function BasPreparationLifecycle({
  api,
  identityApi,
  reportApi,
  workspace,
  onChanged,
  initialPreparationTarget,
}: {
  readonly api: Api;
  readonly identityApi: IdentityApi;
  readonly reportApi: ExportApi;
  readonly workspace: AuthenticatedWorkspace | undefined;
  readonly onChanged?: () => Promise<void>;
  readonly initialPreparationTarget?: InitialPreparationTarget;
}) {
  const scope = `${workspace?.workspaceId ?? ""}:${workspace?.organisationId ?? ""}:${workspace?.userId ?? ""}:${workspace?.sessionId ?? ""}`;
  const [stage, setStage] = useState<Stage>(() =>
    initialPreparationTarget ? "Review" : "Prepare",
  );
  const [exportTargetKind, setExportTargetKind] = useState<"calculation" | "frozen_bas">(
    "frozen_bas",
  );
  const [exportFormat, setExportFormat] = useState<BasPreparationExportRequest["format"]>("pdf");
  const [profiles, setProfiles] = useState<BasObligationsProfileSummary[]>([]);
  const [periods, setPeriods] = useState<BasPeriodSummary[]>([]);
  const [suggestions, setSuggestions] = useState<BasPeriodSuggestion[]>([]);
  const [preparations, setPreparations] = useState<BasPreparationSummary[]>([]);
  const [frozen, setFrozen] = useState<BasFrozenBasSummary[]>([]);
  const [observations, setObservations] = useState<BasExternalObservationSummary[]>([]);
  const [disputes, setDisputes] = useState<BasExternalDisputeResolutionSummary[]>([]);
  const [exportEvents, setExportEvents] = useState<BasExportEventSummary[]>([]);
  const [exportEventsLoading, setExportEventsLoading] = useState(false);
  const [exportEventsError, setExportEventsError] = useState(false);
  const [initialPreparationStatus, setInitialPreparationStatus] = useState<
    "none" | "pending" | "resolved" | "stale" | "sources-unavailable" | "unavailable"
  >(() => (initialPreparationTarget ? "pending" : "none"));
  const [selectedProfile, setSelectedProfile] = useState("");
  const [selectedPeriod, setSelectedPeriod] = useState("");
  const [selectedPreparation, setSelectedPreparation] = useState("");
  const [selectedFrozen, setSelectedFrozen] = useState("");
  const [preparationDetail, setPreparationDetail] =
    useState<ReturnType<typeof preparationCodec.decodeResponse>>();
  const [frozenDetail, setFrozenDetail] =
    useState<ReturnType<typeof frozenDetailCodec.decodeResponse>>();
  const [, setProfileDetail] = useState<ReturnType<typeof profileCodec.decodeResponse>>();
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [reload, setReload] = useState(0);
  const [exportHistoryReload, setExportHistoryReload] = useState(0);
  const historyGeneration = `${scope}\u0000${reload}`;
  const historyRequestRef = useRef(historyGeneration);
  historyRequestRef.current = historyGeneration;
  const [actualDueDateEvidence, setActualDueDateEvidence] = useState<EvidenceFields>({
    type: "",
    id: "",
    revision: "",
    hash: "",
  });
  const currentUserInput = useMemo(
    () =>
      currentUserCodec.encodeRequest(
        create(GetCurrentUserRequestSchema, {
          authentication: workspace ? authentication(workspace) : undefined,
        }),
      ),
    [workspace],
  );
  const currentUserQuery = identityApi.getCurrentUser.useQuery({
    enabled: Boolean(workspace?.userId && workspace.sessionId),
    input: currentUserInput,
    retry: false,
    refetchOnMount: "always",
  });
  const currentUser = useMemo(() => {
    if (!currentUserQuery.data || currentUserQuery.isError) return undefined;
    try {
      return currentUserCodec.decodeResponse(currentUserQuery.data).user;
    } catch {
      return undefined;
    }
  }, [currentUserQuery.data, currentUserQuery.isError]);
  const [totp, setTotp] = useState("");
  const [approvalReason, setApprovalReason] = useState("");
  const [replacementReason, setReplacementReason] = useState("");
  const [revisionReason, setRevisionReason] = useState("");
  const [correctionReason, setCorrectionReason] = useState("");
  const [disputeReason, setDisputeReason] = useState("");
  const [rejectedReplacementReason, setRejectedReplacementReason] = useState("");
  const [effectiveStart, setEffectiveStart] = useState(todayDate());
  const [effectiveEnd, setEffectiveEnd] = useState("");
  const [gstRegistrationStart, setGstRegistrationStart] = useState("");
  const [gstRegistrationEnd, setGstRegistrationEnd] = useState("");
  const [profileReason, setProfileReason] = useState("");
  const [gstApplicability, setGstApplicability] = useState(BasApplicability.UNKNOWN);
  const [gstStatus, setGstStatus] = useState(BasRegistrationStatus.UNKNOWN);
  const [gstMethod, setGstMethod] = useState(BasGstMethod.UNKNOWN);
  const [turnover, setTurnover] = useState(BasTurnoverCategory.UNKNOWN);
  const [gstBasis, setGstBasis] = useState(BasGstBasis.UNKNOWN);
  const [gstCadence, setGstCadence] = useState(BasCadence.UNKNOWN);
  const [gstReason, setGstReason] = useState("");
  const [paygwApplicability, setPaygwApplicability] = useState(BasApplicability.UNKNOWN);
  const [paygwCategory, setPaygwCategory] = useState(BasPaygwCategory.UNKNOWN);
  const [paygwCadence, setPaygwCadence] = useState(BasCadence.UNKNOWN);
  const [paygwPrefill, setPaygwPrefill] = useState(BasPrefillRequirement.UNKNOWN);
  const [paygwReason, setPaygwReason] = useState("");
  const [paygiApplicability, setPaygiApplicability] = useState(BasApplicability.UNKNOWN);
  const [paygiMethod, setPaygiMethod] = useState(BasPaygiMethod.UNKNOWN);
  const [paygiCadence, setPaygiCadence] = useState(BasCadence.UNKNOWN);
  const [paygiNoticeStatus, setPaygiNoticeStatus] = useState(BasNoticeOperativeStatus.UNKNOWN);
  const [paygiPriorVariation, setPaygiPriorVariation] = useState(
    BasPriorVariationDisposition.UNKNOWN,
  );
  const [paygiReason, setPaygiReason] = useState("");
  const [otherAnswers, setOtherAnswers] = useState<Record<string, BasApplicability>>(() =>
    Object.fromEntries(OTHER_OBLIGATIONS.map(([key]) => [key, BasApplicability.UNKNOWN])),
  );
  const [evidence, setEvidence] = useState<EvidenceFields>({
    type: "",
    id: "",
    revision: "",
    hash: "",
  });
  const [supportState, setSupportState] = useState(BasPeriodSupportState.SUPPORTED);
  const [periodReason, setPeriodReason] = useState("");
  const [dueDateKind, setDueDateKind] = useState(BasDueDateKind.NORMAL_SUGGESTION);
  const [confirmedDueDate, setConfirmedDueDate] = useState("");
  const [paygwInputs, setPaygwInputs] = useState({ w1: "", w2: "", w3: "", w4: "", reason: "" });
  const [paygwStates, setPaygwStates] = useState({
    w1: BasAmountState.UNKNOWN,
    w2: BasAmountState.UNKNOWN,
    w3: BasAmountState.UNKNOWN,
    w4: BasAmountState.UNKNOWN,
  });
  const [paygiAmount, setPaygiAmount] = useState("");
  const [paygiAmountState, setPaygiAmountState] = useState(BasAmountState.UNKNOWN);
  const [paygiRate, setPaygiRate] = useState("");
  const [incomeYearId, setIncomeYearId] = useState("");
  const [incomeYearStart, setIncomeYearStart] = useState("");
  const [incomeYearEnd, setIncomeYearEnd] = useState("");
  const [acknowledged, setAcknowledged] = useState<string[]>([]);
  const [eventKind, setEventKind] = useState(BasExternalEventKind.PENDING);
  const [channel, setChannel] = useState(BasExternalChannel.ONLINE_PORTAL);
  const [claimedDate, setClaimedDate] = useState(todayDate());
  const [externalReference, setExternalReference] = useState("");
  const [eventReason, setEventReason] = useState("");
  const [correctionObservation, setCorrectionObservation] = useState("");
  const [correctionKind, setCorrectionKind] = useState(BasCorrectionKind.RETRACT_AS_INCORRECT);
  const [correctedEventKind, setCorrectedEventKind] = useState(BasExternalEventKind.UNKNOWN);
  const [disputeOutcome, setDisputeOutcome] = useState(
    BasDisputeResolutionOutcome.UPHOLD_RECORDED_CHAIN,
  );
  const active = useRef(true);
  const selectedRef = useRef({ profile: "", period: "", preparation: "", frozen: "" });
  selectedRef.current = {
    profile: selectedProfile,
    period: selectedPeriod,
    preparation: selectedPreparation,
    frozen: selectedFrozen,
  };
  const initialPreparationRef = useMemo(
    () =>
      initialPreparationTarget
        ? create(SourceRefSchema, {
            type: "bas_preparation",
            id: initialPreparationTarget.id,
            revision: initialPreparationTarget.revision,
            contentHash: fromHex(initialPreparationTarget.hash),
          })
        : undefined,
    [initialPreparationTarget],
  );
  useEffect(() => {
    if (initialPreparationRef) {
      setStage("Review");
      setInitialPreparationStatus("pending");
    } else {
      setInitialPreparationStatus("none");
    }
  }, [initialPreparationRef]);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
    };
  }, []);

  const auth = useMemo(() => {
    if (!workspace) return undefined;
    return authentication(workspace);
  }, [workspace]);
  const canPrepare = Boolean(
    currentUser &&
      currentUser.id === workspace?.userId &&
      currentUser.roles.some(
        (role) => role === Role.WORKSPACE_ADMIN || role === Role.BUSINESS_PREPARER,
      ),
  );
  const canApprove = Boolean(
    currentUser &&
      currentUser.id === workspace?.userId &&
      currentUser.roles.includes(Role.BUSINESS_LODGER),
  );
  const canManageRuleReference = Boolean(
    currentUser &&
      currentUser.id === workspace?.userId &&
      currentUser.roles.includes(Role.WORKSPACE_ADMIN),
  );
  const identity = useCallback(() => {
    if (!workspace?.organisationId || !auth) throw new Error("workspace unavailable");
    return { authentication: auth, organisationId: workspace.organisationId };
  }, [auth, workspace?.organisationId]);

  useEffect(() => {
    if (!workspace?.organisationId || !auth) return;
    let live = true;
    const generation = historyGeneration;
    const isCurrent = () => live && historyRequestRef.current === generation;
    setLoading(true);
    setError("");
    const base = identity();
    const load = async () => {
      const [nextProfiles, nextPeriods, nextPreparations, nextFrozen, nextDisputes] =
        await Promise.all([
          readAllPages<BasObligationsProfileSummary>({
            call: api.listBasObligationsProfiles,
            encode: (cursor) =>
              profilesCodec.encodeRequest(
                create(ListBasObligationsProfilesRequestSchema, {
                  ...base,
                  page: create(PageRequestSchema, { pageSize: 200, ...(cursor ? { cursor } : {}) }),
                }),
              ),
            decode: (frame) => {
              const response = profilesCodec.decodeResponse(frame);
              return { values: response.profiles, nextCursor: response.page?.nextCursor };
            },
          }),
          readAllPages<BasPeriodSummary>({
            call: api.listBasPeriods,
            encode: (cursor) =>
              periodsCodec.encodeRequest(
                create(ListBasPeriodsRequestSchema, {
                  ...base,
                  page: create(PageRequestSchema, { pageSize: 200, ...(cursor ? { cursor } : {}) }),
                }),
              ),
            decode: (frame) => {
              const response = periodsCodec.decodeResponse(frame);
              return { values: response.periods, nextCursor: response.page?.nextCursor };
            },
          }),
          readAllPages<BasPreparationSummary>({
            call: api.listBasPreparations,
            encode: (cursor) =>
              preparationsCodec.encodeRequest(
                create(ListBasPreparationsRequestSchema, {
                  ...base,
                  page: create(PageRequestSchema, { pageSize: 200, ...(cursor ? { cursor } : {}) }),
                }),
              ),
            decode: (frame) => {
              const response = preparationsCodec.decodeResponse(frame);
              return { values: response.preparations, nextCursor: response.page?.nextCursor };
            },
          }),
          readAllPages<BasFrozenBasSummary>({
            call: api.listFrozenBas,
            encode: (cursor) =>
              frozenCodec.encodeRequest(
                create(ListFrozenBasRequestSchema, {
                  ...base,
                  page: create(PageRequestSchema, { pageSize: 200, ...(cursor ? { cursor } : {}) }),
                }),
              ),
            decode: (frame) => {
              const response = frozenCodec.decodeResponse(frame);
              return { values: response.frozenBas, nextCursor: response.page?.nextCursor };
            },
          }),
          readAllPages<BasExternalDisputeResolutionSummary>({
            call: api.listBasExternalDisputeResolutions,
            encode: (cursor) =>
              disputesCodec.encodeRequest(
                create(ListBasExternalDisputeResolutionsRequestSchema, {
                  ...base,
                  page: create(PageRequestSchema, { pageSize: 200, ...(cursor ? { cursor } : {}) }),
                }),
              ),
            decode: (frame) => {
              const response = disputesCodec.decodeResponse(frame);
              return { values: response.resolutions, nextCursor: response.page?.nextCursor };
            },
          }),
        ]);
      if (!isCurrent()) return;
      setProfiles(nextProfiles);
      setPeriods(nextPeriods);
      setPreparations(nextPreparations);
      setFrozen(nextFrozen);
      setDisputes(nextDisputes);
      const prev = selectedRef.current;
      const linkedPreparation = initialPreparationRef
        ? nextPreparations.find(
            (item) => refKey(item.preparation) === refKey(initialPreparationRef),
          )
        : undefined;
      const linkedProfileKey = linkedPreparation?.profile ? refKey(linkedPreparation.profile) : "";
      const linkedPeriodKey = linkedPreparation?.period ? refKey(linkedPreparation.period) : "";
      const linkedFrozenKey = linkedPreparation?.frozenBas
        ? refKey(linkedPreparation.frozenBas)
        : "";
      const linkedProfileAvailable = Boolean(
        linkedProfileKey && nextProfiles.some((item) => refKey(item.profile) === linkedProfileKey),
      );
      const linkedPeriodAvailable = Boolean(
        linkedPeriodKey && nextPeriods.some((item) => refKey(item.period) === linkedPeriodKey),
      );
      const linkedFrozenAvailable =
        !linkedFrozenKey || nextFrozen.some((item) => refKey(item.frozenBas) === linkedFrozenKey);
      const profileKey = initialPreparationRef
        ? linkedProfileAvailable
          ? linkedProfileKey
          : ""
        : nextProfiles.some((item) => refKey(item.profile) === prev.profile)
          ? prev.profile
          : refKey(nextProfiles[0]?.profile);
      const periodKey = initialPreparationRef
        ? linkedPeriodAvailable
          ? linkedPeriodKey
          : ""
        : nextPeriods.some((item) => refKey(item.period) === prev.period)
          ? prev.period
          : refKey(nextPeriods[0]?.period);
      const preparationKey = initialPreparationRef
        ? linkedPreparation
          ? refKey(linkedPreparation.preparation)
          : ""
        : nextPreparations.some((item) => refKey(item.preparation) === prev.preparation)
          ? prev.preparation
          : refKey(nextPreparations[0]?.preparation);
      const frozenKey = initialPreparationRef
        ? linkedFrozenAvailable
          ? linkedFrozenKey
          : ""
        : nextFrozen.some((item) => refKey(item.frozenBas) === prev.frozen)
          ? prev.frozen
          : refKey(nextFrozen[0]?.frozenBas);
      setSelectedProfile(profileKey);
      setSelectedPeriod(periodKey || "");
      setSelectedPreparation(preparationKey || "");
      setSelectedFrozen(frozenKey || "");
      if (initialPreparationRef) {
        setInitialPreparationStatus(
          !linkedPreparation
            ? "stale"
            : linkedProfileAvailable && linkedPeriodAvailable && linkedFrozenAvailable
              ? "resolved"
              : "sources-unavailable",
        );
      } else {
        setInitialPreparationStatus("none");
      }
      const year = new Date().getUTCFullYear();
      const calendarStart = Math.max(1, year - 4);
      const calendarEnd = Math.min(9999, calendarStart + 9);
      const chosenProfile = nextProfiles.find(
        (item) => refKey(item.profile) === profileKey,
      )?.profile;
      if (!chosenProfile) {
        setSuggestions([]);
        if (!periodKey && !initialPreparationRef) setSelectedPeriod("");
        return;
      }
      const calendarFrame = calendarCodec.encodeRequest(
        create(GetBasCalendarRequestSchema, {
          ...base,
          asOfDate: civil(todayDate()),
          horizon: create(BasCivilDateRangeSchema, {
            startDate: { year: calendarStart, month: 1, day: 1 },
            endDate: { year: calendarEnd, month: 12, day: 31 },
          }),
          profile: exactRef(chosenProfile),
        }),
      );
      const nextCalendar = calendarCodec.decodeResponse(await api.getBasCalendar(calendarFrame));
      if (isCurrent()) {
        setSuggestions(nextCalendar.suggestions);
        if (!periodKey && !initialPreparationRef)
          setSelectedPeriod(
            nextCalendar.suggestions[0] ? suggestionKey(nextCalendar.suggestions[0]) : "",
          );
      }
    };
    void load()
      .catch(() => {
        if (isCurrent()) {
          setError("BAS history could not be loaded. Refresh and try again.");
          if (initialPreparationRef) setInitialPreparationStatus("unavailable");
        }
      })
      .finally(() => {
        if (isCurrent()) setLoading(false);
      });
    return () => {
      live = false;
    };
  }, [api, auth, historyGeneration, identity, initialPreparationRef, workspace?.organisationId]);

  useEffect(() => {
    if (!auth || !workspace?.organisationId) return;
    const profileRef = profiles.find((entry) => refKey(entry.profile) === selectedProfile)?.profile;
    if (!profileRef) {
      setProfileDetail(undefined);
      return;
    }
    let live = true;
    const request = profileCodec.encodeRequest(
      create(GetBasObligationsProfileRequestSchema, {
        ...identity(),
        asOfDate: civil(todayDate()),
        profileId: profileRef.id,
        profileHash: profileRef.contentHash,
      }),
    );
    void api
      .getBasObligationsProfile(request)
      .then((frame) => {
        if (live) setProfileDetail(profileCodec.decodeResponse(frame));
      })
      .catch(() => {
        if (live) setProfileDetail(undefined);
      });
    return () => {
      live = false;
    };
  }, [api, auth, identity, profiles, selectedProfile, workspace?.organisationId]);

  useEffect(() => {
    if (!auth || !workspace?.organisationId) {
      setPreparationDetail(undefined);
      return;
    }
    const summary = preparations.find((entry) => refKey(entry.preparation) === selectedPreparation);
    if (!summary?.preparation) {
      setPreparationDetail(undefined);
      return;
    }
    let live = true;
    const request = preparationCodec.encodeRequest(
      create(GetBasPreparationRequestSchema, {
        ...identity(),
        preparationId: summary.preparation.id,
        preparationHash: summary.preparation.contentHash,
      }),
    );
    void api
      .getBasPreparation(request)
      .then((frame) => {
        if (live) setPreparationDetail(preparationCodec.decodeResponse(frame));
      })
      .catch(() => {
        if (live) setPreparationDetail(undefined);
      });
    return () => {
      live = false;
    };
  }, [api, auth, identity, preparations, selectedPreparation, workspace?.organisationId]);

  useEffect(() => {
    if (!auth || !workspace?.organisationId) {
      setFrozenDetail(undefined);
      return;
    }
    const summary = frozen.find((entry) => refKey(entry.frozenBas) === selectedFrozen);
    if (!summary?.frozenBas) {
      setFrozenDetail(undefined);
      return;
    }
    let live = true;
    const request = frozenDetailCodec.encodeRequest(
      create(GetFrozenBasRequestSchema, {
        ...identity(),
        frozenId: summary.frozenBas.id,
        frozenHash: summary.frozenBas.contentHash,
      }),
    );
    void api
      .getFrozenBas(request)
      .then((frame) => {
        if (live) setFrozenDetail(frozenDetailCodec.decodeResponse(frame));
      })
      .catch(() => {
        if (live) setFrozenDetail(undefined);
      });
    return () => {
      live = false;
    };
  }, [api, auth, frozen, identity, selectedFrozen, workspace?.organisationId]);

  useEffect(() => {
    if (!auth || !workspace?.organisationId) {
      setObservations([]);
      return;
    }
    const ref = frozen.find((entry) => refKey(entry.frozenBas) === selectedFrozen)?.frozenBas;
    if (!ref) {
      setObservations([]);
      return;
    }
    let live = true;
    void readAllPages<BasExternalObservationSummary>({
      call: api.listBasExternalObservations,
      encode: (cursor) =>
        observationsCodec.encodeRequest(
          create(ListBasExternalObservationsRequestSchema, {
            ...identity(),
            frozenId: ref.id,
            frozenHash: ref.contentHash,
            page: create(PageRequestSchema, { pageSize: 200, ...(cursor ? { cursor } : {}) }),
          }),
        ),
      decode: (frame) => {
        const response = observationsCodec.decodeResponse(frame);
        return { values: response.observations, nextCursor: response.page?.nextCursor };
      },
    })
      .then((values) => {
        if (live) setObservations(values);
      })
      .catch(() => {
        if (live) setObservations([]);
      });
    return () => {
      live = false;
    };
  }, [api, auth, frozen, identity, selectedFrozen, workspace?.organisationId]);

  const selectedProfileSummary = profiles.find(
    (entry) => refKey(entry.profile) === selectedProfile,
  );
  const selectedPeriodSummary = periods.find((entry) => refKey(entry.period) === selectedPeriod);
  const selectedSuggestion = suggestions.find((entry) => suggestionKey(entry) === selectedPeriod);
  const selectedPreparationSummary = preparations.find(
    (entry) => refKey(entry.preparation) === selectedPreparation,
  );
  const selectedFrozenSummary = frozen.find((entry) => refKey(entry.frozenBas) === selectedFrozen);
  const selectedExportTarget =
    exportTargetKind === "frozen_bas"
      ? selectedFrozenSummary?.frozenBas
      : selectedPreparationSummary?.currentCalculation;
  const selectedExportTargetKey = selectedExportTarget
    ? `${exportTargetKind}:${refKey(selectedExportTarget)}`
    : "";
  const exportHistoryGeneration = `${selectedExportTargetKey}\u0000${exportHistoryReload}`;
  const exportHistoryRequestRef = useRef(exportHistoryGeneration);
  exportHistoryRequestRef.current = exportHistoryGeneration;

  useEffect(() => {
    if (!auth || !workspace?.organisationId || !selectedExportTarget || !selectedExportTargetKey) {
      setExportEvents([]);
      setExportEventsError(false);
      setExportEventsLoading(false);
      return;
    }
    let live = true;
    const generation = exportHistoryGeneration;
    const isCurrent = () => live && exportHistoryRequestRef.current === generation;
    setExportEventsLoading(true);
    setExportEventsError(false);
    const target = create(BasExportTargetSchema, {
      exactTarget:
        exportTargetKind === "calculation"
          ? { case: "calculation", value: exactRef(selectedExportTarget) }
          : { case: "frozenBas", value: exactRef(selectedExportTarget) },
    });
    void readAllPages<BasExportEventSummary>({
      call: api.listBasExportEvents,
      encode: (cursor) =>
        exportEventsCodec.encodeRequest(
          create(ListBasExportEventsRequestSchema, {
            ...identity(),
            target,
            page: create(PageRequestSchema, { pageSize: 200, ...(cursor ? { cursor } : {}) }),
          }),
        ),
      decode: (frame) => {
        const response = exportEventsCodec.decodeResponse(frame);
        return { values: response.events, nextCursor: response.page?.nextCursor };
      },
    })
      .then((events) => {
        if (isCurrent())
          setExportEvents(
            events.filter((event) => exportTargetKey(event.target) === selectedExportTargetKey),
          );
      })
      .catch(() => {
        if (isCurrent()) {
          setExportEvents([]);
          setExportEventsError(true);
        }
      })
      .finally(() => {
        if (isCurrent()) setExportEventsLoading(false);
      });
    return () => {
      live = false;
    };
  }, [
    api,
    auth,
    exportTargetKind,
    exportHistoryGeneration,
    identity,
    selectedExportTarget,
    selectedExportTargetKey,
    workspace?.organisationId,
  ]);

  const prep = preparationDetail?.preparation;
  const calculation = preparationDetail?.currentCalculation;
  const validation = preparationDetail?.currentValidation;
  const currentChain = preparationDetail?.currentChain;
  const lodgerCopy =
    currentUserQuery.isError || !currentUser
      ? "Current BAS roles could not be verified. Preparation and approval controls are unavailable."
      : roleLabel(currentUser.roles);

  const execute = async (work: () => Promise<void>) => {
    if (busy) return;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      await work();
      await onChanged?.();
    } catch (cause) {
      setError(errorCopy(cause));
    } finally {
      setBusy(false);
    }
  };
  const refresh = () => {
    setLoading(true);
    setReload((value) => value + 1);
    setExportHistoryReload((value) => value + 1);
  };
  const exportExactTarget = async () => {
    if (busy) return;
    const targetRef =
      exportTargetKind === "frozen_bas"
        ? selectedFrozenSummary?.frozenBas
        : selectedPreparationSummary?.currentCalculation;
    if (
      !workspace?.organisationId ||
      !targetRef?.id ||
      targetRef.revision < 1n ||
      targetRef.contentHash.byteLength !== 32
    ) {
      setError(
        "Choose a complete retained calculation or frozen snapshot identity before exporting.",
      );
      return;
    }
    setBusy(true);
    setError("");
    setMessage("");
    try {
      const exactTarget =
        exportTargetKind === "frozen_bas"
          ? {
              kind: "frozen_bas" as const,
              id: targetRef.id,
              revision: "1" as const,
              hash: hex(targetRef.contentHash),
            }
          : {
              kind: "calculation" as const,
              id: targetRef.id,
              revision: String(targetRef.revision),
              hash: hex(targetRef.contentHash),
            };
      const auth = authentication(workspace);
      const result = await reportApi.exportBasPreparation({
        authentication: { actorUserId: auth.actorUserId, sessionId: auth.sessionId },
        organisationId: workspace.organisationId,
        target: exactTarget,
        format: exportFormat,
      });
      setMessage(
        result.saved
          ? `Exact ${exportTargetKind === "frozen_bas" ? "frozen BAS" : "calculation"} ${exportFormat.toUpperCase()} export saved. The BAS has not been lodged.`
          : "BAS export cancelled.",
      );
    } catch (cause) {
      setError(reportExportMessage(cause));
    } finally {
      setBusy(false);
      setExportHistoryReload((value) => value + 1);
    }
  };
  const ordinaryCommand = () => {
    if (!workspace) throw new Error("workspace unavailable");
    return create(CommandContextSchema, {
      authentication: authentication(workspace),
      idempotencyKey: uuidV7(),
    });
  };
  const freshCommand = async (purpose: string) => {
    if (!workspace) throw new Error("workspace unavailable");
    const factor = await assertFreshFactor(identityApi, workspace, totp, purpose);
    setTotp("");
    return create(CommandContextSchema, {
      authentication: authentication(workspace),
      idempotencyKey: uuidV7(),
      freshFactor: factor,
    });
  };
  const refreshSelectedPreparation = async (selected: Ref) => {
    if (!workspace) throw new Error("workspace unavailable");
    const frame = preparationCodec.encodeRequest(
      create(GetBasPreparationRequestSchema, {
        ...identity(),
        preparationId: selected.id,
        preparationHash: selected.contentHash,
      }),
    );
    const exact = preparationCodec.decodeResponse(await api.getBasPreparation(frame));
    const current = preparations.find(
      (entry) => entry.preparation?.id === selected.id,
    )?.preparation;
    if (
      !current ||
      !sameBytes(current.contentHash, selected.contentHash) ||
      exact.preparation?.version !== selected.revision
    )
      throw new Error("stale preparation");
    return exact;
  };

  const saveProfile = () =>
    execute(async () => {
      if (!workspace || !canPrepare || !profileReason.trim()) throw new Error("invalid profile");
      if (gstApplicability !== BasApplicability.UNKNOWN && !gstReason.trim())
        throw new Error("GST reason required");
      const refs = parseEvidence(evidence);
      const paygiMethodNeedsIncomeYear =
        paygiMethod === BasPaygiMethod.AMOUNT || paygiMethod === BasPaygiMethod.RATE;
      if (
        (paygiApplicability === BasApplicability.NOT_APPLICABLE &&
          paygiMethod !== BasPaygiMethod.NOT_APPLICABLE) ||
        (paygiApplicability === BasApplicability.ACTIVE &&
          paygiMethod === BasPaygiMethod.NOT_APPLICABLE)
      )
        throw new Error("PAYGI applicability and method do not agree");
      if (
        paygiMethodNeedsIncomeYear &&
        (!UUID.test(incomeYearId) || !incomeYearStart || !incomeYearEnd || !refs[0])
      )
        throw new Error(
          "PAYGI amount and rate methods require a confirmed income year and evidence",
        );
      const answer = (applicability: BasApplicability, answerReason = "") =>
        create(BasObligationAnswerSchema, {
          applicability,
          evidence: applicability === BasApplicability.UNKNOWN ? [] : refs,
          reason: applicability === BasApplicability.UNKNOWN ? "" : answerReason.trim(),
        });
      const other = create(
        BasOtherObligationInventorySchema,
        Object.fromEntries(
          OTHER_OBLIGATIONS.map(([key]) => [
            key,
            answer(otherAnswers[key] ?? BasApplicability.UNKNOWN, profileReason),
          ]),
        ),
      );
      const confirmedIncomeYear = paygiMethodNeedsIncomeYear
        ? create(BasIncomeYearIdentitySchema, {
            incomeYearId,
            dates: create(BasCivilDateRangeSchema, {
              startDate: civil(incomeYearStart),
              endDate: civil(incomeYearEnd),
            }),
            confirmation: refs[0],
          })
        : undefined;
      const profile = create(BasObligationsProfileInputSchema, {
        effectiveInterval: create(BasEffectiveIntervalSchema, {
          startDate: civil(effectiveStart),
          ...(effectiveEnd ? { endDate: civil(effectiveEnd) } : {}),
        }),
        gst: create(BasGstProfileInputSchema, {
          applicability: gstApplicability,
          registrationStatus: gstStatus,
          ...(gstStatus === BasRegistrationStatus.REGISTERED
            ? {
                registrationInterval: create(BasEffectiveIntervalSchema, {
                  startDate: civil(gstRegistrationStart),
                  ...(gstRegistrationEnd ? { endDate: civil(gstRegistrationEnd) } : {}),
                }),
              }
            : {}),
          reportingMethod: gstMethod,
          turnoverCategory: turnover,
          gstBasis,
          cadence: gstCadence,
          evidence: refs,
          reason: gstReason,
        }),
        paygw: create(BasPaygwProfileInputSchema, {
          applicability: answer(paygwApplicability, paygwReason),
          category: paygwCategory,
          cadence: paygwCadence,
          prefillRequirement: paygwPrefill,
          confirmedStatementLabels: [],
          statementRequirementEvidence: refs,
          reason: paygwReason,
        }),
        paygi: create(BasPaygiProfileInputSchema, {
          applicability: answer(paygiApplicability, paygiReason),
          method: paygiMethod,
          cadence: paygiCadence,
          ...(confirmedIncomeYear ? { confirmedIncomeYear } : {}),
          evidence: refs,
          reason: paygiReason,
        }),
        otherObligations: other,
        evidence: refs,
        reason: profileReason.trim(),
      });
      const latestRevision = profiles.reduce((value, item) => {
        const revision = item.profile?.revision;
        return revision !== undefined && revision > value ? revision : value;
      }, 0n);
      const request = create(SetBasObligationsProfileRequestSchema, {
        commandContext: ordinaryCommand(),
        organisationId: identity().organisationId,
        expectedVersion: latestRevision,
        profile,
      });
      const response = setProfileCodec.decodeResponse(
        await api.setBasObligationsProfile(setProfileCodec.encodeRequest(request)),
      );
      if (!response.profile) throw new Error("missing profile");
      setSelectedProfile(refKey(profileRef(response.profile)));
      setMessage(`Saved BAS obligations profile revision ${response.profile.revision}.`);
      refresh();
    });

  const confirmSelectedPeriod = () =>
    execute(async () => {
      if (
        !workspace ||
        !canPrepare ||
        !selectedProfileSummary?.profile ||
        !selectedSuggestion ||
        !periodReason.trim()
      )
        throw new Error("period unavailable");
      const dueDate = confirmedDueDate || iso(selectedSuggestion.nominalDueDate);
      const refs = parseEvidence(evidence);
      const actualDueDateRefs = parseEvidence(actualDueDateEvidence);
      if (dueDateKind !== BasDueDateKind.NORMAL_SUGGESTION && actualDueDateRefs.length === 0)
        throw new Error("actual due date evidence required");
      const period = create(BasPeriodConfirmationInputSchema, {
        period: selectedSuggestion.period,
        window: selectedSuggestion.window,
        profile: exactRef(selectedProfileSummary.profile),
        nominalDueDate: selectedSuggestion.nominalDueDate,
        dueDateRuleVersion: selectedSuggestion.dueDateRuleVersion,
        dueDateRuleHash: selectedSuggestion.dueDateRuleHash,
        confirmedDueDate: civil(dueDate),
        dueDateKind,
        dueDateEvidence: actualDueDateRefs,
        supportState,
        supportFindings: [],
        registrationEvidence: refs,
        reason: periodReason.trim(),
      });
      const prior = periods.find((item) => samePeriodRangeAndWindow(item, selectedSuggestion));
      const request = create(ConfirmBasPeriodRequestSchema, {
        commandContext: ordinaryCommand(),
        organisationId: identity().organisationId,
        expectedVersion: prior?.period?.revision ?? 0n,
        period,
      });
      const response = confirmPeriodCodec.decodeResponse(
        await api.confirmBasPeriod(confirmPeriodCodec.encodeRequest(request)),
      );
      if (!response.period?.periodId) throw new Error("missing period");
      setSelectedPeriod(refKey(periodRef(response.period)));
      setMessage(`Confirmed BAS period ${response.period.periodId}.`);
      refresh();
    });

  const regenerate = () =>
    execute(async () => {
      if (
        !workspace ||
        !canPrepare ||
        !selectedPeriodSummary?.period ||
        !selectedProfileSummary?.profile
      )
        throw new Error("period/profile required");
      const previous = preparations
        .filter((item) => item.period?.id === selectedPeriodSummary.period?.id)
        // Pages use append order; exact timestamp ties must prefer the newest append.
        .reverse()
        .sort(
          (left, right) =>
            Number((right.updatedAt?.seconds ?? 0n) - (left.updatedAt?.seconds ?? 0n)) ||
            (right.updatedAt?.nanos ?? 0) - (left.updatedAt?.nanos ?? 0),
        )[0];
      const chain = preparationDetail?.currentChain;
      const correctiveGenesis = Boolean(
        preparationDetail?.currentBranchResolution &&
          preparationDetail.currentDisputeOverlay === BasDisputeOverlayState.CORRECTION_REQUIRED &&
          chain?.family &&
          chain.branch?.expectedState.case === "retained" &&
          chain.activeAttempt?.expectedState.case === "absent",
      );
      const family = correctiveGenesis ? chain?.family : previous?.family;
      const preparation = correctiveGenesis ? undefined : previous?.preparation;
      const expectedVersion = preparation?.revision ?? 0n;
      const request = create(RegenerateBasPreparationRequestSchema, {
        commandContext: ordinaryCommand(),
        organisationId: identity().organisationId,
        expectedVersion,
        familyExpectation: family
          ? create(BasExpectedIdentitySchema, {
              expectedState: { case: "retained", value: exactRef(family) },
              expectedVersion: family.revision,
            })
          : create(BasExpectedIdentitySchema, { expectedState: { case: "absent", value: true } }),
        period: exactRef(selectedPeriodSummary.period),
        profile: exactRef(selectedProfileSummary.profile),
        preparationExpectation: preparation
          ? create(BasExpectedIdentitySchema, {
              expectedState: { case: "retained", value: exactRef(preparation) },
              expectedVersion: expectedVersion,
            })
          : create(BasExpectedIdentitySchema, { expectedState: { case: "absent", value: true } }),
      });
      const response = regenerateCodec.decodeResponse(
        await api.regenerateBasPreparation(regenerateCodec.encodeRequest(request)),
      );
      if (!response.preparation) throw new Error("missing preparation");
      setSelectedPreparation(
        refKey({
          type: "bas_preparation",
          id: response.preparation.preparationId,
          revision: response.preparation.version,
          contentHash: response.preparation.contentHash,
        }),
      );
      setMessage(`Prepared exact BAS version ${response.preparation.version} for review.`);
      refresh();
    });

  const saveInputs = () =>
    execute(async () => {
      const currentCalculation = preparationDetail?.currentCalculation;
      if (
        !workspace ||
        !canPrepare ||
        !prep?.preparationId ||
        !prep.contentHash ||
        !currentCalculation
      )
        throw new Error("preparation required");
      const paygiMethodNeedsIncomeYear =
        paygiMethod === BasPaygiMethod.AMOUNT || paygiMethod === BasPaygiMethod.RATE;
      const profileSnapshot = currentCalculation.profileSnapshot;
      const snapshotProfileRef = profileSnapshot
        ? {
            type: "bas_obligations_profile",
            id: profileSnapshot.profileId,
            revision: profileSnapshot.revision,
            contentHash: profileSnapshot.contentHash,
          }
        : undefined;
      if (
        !profileSnapshot?.profile ||
        !currentCalculation.profile ||
        !prep.profile ||
        refKey(snapshotProfileRef) !== refKey(currentCalculation.profile) ||
        refKey(currentCalculation.profile) !== refKey(prep.profile)
      )
        throw paygiInputError("PAYGI_PROFILE_IDENTITY_UNVERIFIED");
      const refs = parseEvidence(evidence);
      const amounts = (key: "w1" | "w2" | "w3" | "w4") =>
        amountInput(paygwStates[key], paygwInputs[key], refs, paygwInputs.reason);
      const retainedPaygwProfile = profileSnapshot.profile.paygw;
      const paygw = create(BasPaygwInputsSchema, {
        category: retainedPaygwProfile?.category ?? BasPaygwCategory.UNKNOWN,
        w1TotalPayments: amounts("w1"),
        w2AmountWithheld: amounts("w2"),
        w3OtherWithholding: amounts("w3"),
        w4NoAbnWithholding: amounts("w4"),
        payrollStatementEvidence: refs,
        prefillReview: retainedPaygwProfile?.prefillRequirement ?? BasPrefillRequirement.UNKNOWN,
        reviewReason: paygwInputs.reason,
      });
      const profileIncomeYear = profileSnapshot?.profile?.paygi?.confirmedIncomeYear;
      let confirmedIncomeYear: BasIncomeYearIdentity | undefined;
      if (paygiMethodNeedsIncomeYear) {
        if (
          !profileIncomeYear?.incomeYearId ||
          !UUID.test(profileIncomeYear.incomeYearId) ||
          !profileIncomeYear.dates?.startDate ||
          !profileIncomeYear.dates.endDate ||
          !profileIncomeYear.confirmation
        )
          throw paygiInputError("PAYGI_PROFILE_YEAR_MISSING");
        if (
          incomeYearId !== profileIncomeYear.incomeYearId ||
          incomeYearStart !== iso(profileIncomeYear.dates.startDate) ||
          incomeYearEnd !== iso(profileIncomeYear.dates.endDate)
        )
          throw paygiInputError("PAYGI_PROFILE_YEAR_MISMATCH");
        if (!refs[0])
          throw new Error("PAYGI requires current notice evidence before saving inputs.");
        confirmedIncomeYear = clone(BasIncomeYearIdentitySchema, profileIncomeYear);
      }
      const paygi = create(BasPaygiInputsSchema, {
        method: paygiMethod,
        ...(refs[0] ? { notice: refs[0] } : {}),
        noticeOperativeStatus: paygiNoticeStatus,
        priorVariation: paygiPriorVariation,
        unsupportedState: BasPaygiUnsupportedState.NONE,
        t1ReviewDecision:
          paygiMethod === BasPaygiMethod.RATE &&
          (paygiAmountState === BasAmountState.KNOWN ||
            paygiAmountState === BasAmountState.KNOWN_ZERO)
            ? BasT1ReviewDecision.REVIEWED
            : BasT1ReviewDecision.UNKNOWN,
        ...(paygiMethod === BasPaygiMethod.AMOUNT
          ? { t7Amount: amountInput(paygiAmountState, paygiAmount, refs, paygwInputs.reason) }
          : {}),
        ...(paygiMethod === BasPaygiMethod.RATE
          ? {
              rateT1: amountInput(paygiAmountState, paygiAmount, refs, paygwInputs.reason),
              rateT2: parseDecimal(paygiRate),
            }
          : {}),
        ...(confirmedIncomeYear ? { confirmedIncomeYear } : {}),
        evidence: refs,
        reviewReason: paygwInputs.reason,
      });
      const request = create(SaveBasPaygInputsRequestSchema, {
        commandContext: ordinaryCommand(),
        organisationId: identity().organisationId,
        expectedVersion: prep.version,
        preparationId: prep.preparationId,
        preparationHash: prep.contentHash,
        paygw,
        paygi,
      });
      const response = savePaygCodec.decodeResponse(
        await api.saveBasPaygInputs(savePaygCodec.encodeRequest(request)),
      );
      if (!response.preparation) throw new Error("missing preparation");
      setSelectedPreparation(refKey(preparationRef(response.preparation)));
      setMessage("Saved PAYG inputs against the displayed preparation revision.");
      refresh();
    });

  const validatePreparation = () =>
    execute(async () => {
      const selectedPreparation = selectedPreparationSummary?.preparation;
      if (!workspace || !canPrepare || !prep || !calculation || !selectedPreparation)
        throw new Error("calculation required");
      const exact = await refreshSelectedPreparation(selectedPreparation);
      if (
        !exact.preparation?.preparationId ||
        !exact.preparation.contentHash ||
        !exact.currentCalculation ||
        exact.currentCalculation.calculationId !== calculation.calculationId ||
        !sameBytes(exact.currentCalculation.contentHash, calculation.contentHash)
      )
        throw new Error("calculation changed");
      const request = create(ValidateBasPreparationRequestSchema, {
        commandContext: ordinaryCommand(),
        organisationId: identity().organisationId,
        expectedVersion: exact.preparation?.version ?? 0n,
        preparationId: exact.preparation.preparationId,
        preparationHash: exact.preparation.contentHash,
        calculationId: exact.currentCalculation.calculationId,
        calculationHash: exact.currentCalculation.contentHash,
      });
      const result = validateCodec.decodeResponse(
        await api.validateBasPreparation(validateCodec.encodeRequest(request)),
      );
      if (!result.preparation) throw new Error("missing validated preparation");
      setSelectedPreparation(refKey(preparationRef(result.preparation)));
      setMessage(`Created review validation ${result.validation?.validationId ?? ""}.`);
      setAcknowledged([]);
      refresh();
    });

  const ackWarnings = () =>
    execute(async () => {
      if (
        !workspace ||
        !canPrepare ||
        !prep ||
        !validation ||
        !preparationDetail?.currentCalculation ||
        acknowledged.length === 0 ||
        !selectedPreparationSummary?.preparation
      )
        throw new Error("warnings required");
      const exact = await refreshSelectedPreparation(selectedPreparationSummary.preparation);
      const currentValidation = exact.currentValidation;
      if (
        !exact.preparation?.preparationId ||
        !exact.preparation.contentHash ||
        !currentValidation ||
        currentValidation.validationId !== validation.validationId ||
        !sameBytes(currentValidation.contentHash, validation.contentHash) ||
        !sameBytes(currentValidation.sourceSnapshotHash, validation.sourceSnapshotHash)
      )
        throw new Error("validation changed");
      const selected = currentValidation.findings.filter(
        (item) =>
          acknowledged.includes(item.findingId) && item.severity === BasFindingSeverity.WARNING,
      );
      const warnings = await Promise.all(
        selected.map(async (finding) =>
          create(BasWarningAcknowledgementInputSchema, {
            findingId: finding.findingId,
            findingHash: await canonicalFindingHash(finding),
          }),
        ),
      );
      const request = create(AcknowledgeBasWarningsRequestSchema, {
        commandContext: ordinaryCommand(),
        organisationId: identity().organisationId,
        expectedVersion: exact.preparation?.version ?? 0n,
        preparationId: exact.preparation.preparationId,
        preparationHash: exact.preparation.contentHash,
        validationId: currentValidation.validationId,
        validationHash: currentValidation.contentHash,
        warnings,
        sourceSnapshotHash: currentValidation.sourceSnapshotHash,
      });
      const response = acknowledgeCodec.decodeResponse(
        await api.acknowledgeBasWarnings(acknowledgeCodec.encodeRequest(request)),
      );
      if (!response.preparation) throw new Error("missing acknowledged preparation");
      setSelectedPreparation(refKey(preparationRef(response.preparation)));
      setMessage(`Recorded ${response.acknowledgements.length} exact warning acknowledgement(s).`);
      setAcknowledged([]);
      refresh();
    });

  const freezePreparation = () =>
    execute(async () => {
      if (
        !workspace ||
        !canApprove ||
        !prep ||
        !calculation ||
        !validation ||
        !currentChain ||
        !selectedPreparationSummary?.preparation ||
        !approvalReason.trim()
      )
        throw new Error("approval unavailable");
      const exact = await refreshSelectedPreparation(selectedPreparationSummary.preparation);
      if (
        !exact.preparation?.preparationId ||
        !exact.preparation.contentHash ||
        !exact.currentCalculation ||
        !exact.currentValidation ||
        exact.currentCalculation.calculationId !== calculation.calculationId ||
        exact.currentValidation.validationId !== validation.validationId ||
        !sameBytes(exact.currentCalculation.contentHash, calculation.contentHash) ||
        !sameBytes(exact.currentValidation.sourceSnapshotHash, validation.sourceSnapshotHash)
      )
        throw new Error("stale review");
      const command = await freshCommand("bas_freeze_manual_handoff");
      const request = create(FreezeBasForManualHandoffRequestSchema, {
        commandContext: command,
        organisationId: identity().organisationId,
        expectedVersion: exact.preparation?.version ?? 0n,
        chain: exact.currentChain,
        preparationId: exact.preparation.preparationId,
        preparationHash: exact.preparation.contentHash,
        calculationId: exact.currentCalculation.calculationId,
        calculationHash: exact.currentCalculation.contentHash,
        validationId: exact.currentValidation.validationId,
        validationHash: exact.currentValidation.contentHash,
        approvalReason: approvalReason.trim(),
      });
      const response = freezeCodec.decodeResponse(
        await api.freezeBasForManualHandoff(freezeCodec.encodeRequest(request)),
      );
      if (!response.frozenBas || !response.preparation) throw new Error("missing frozen snapshot");
      setSelectedPreparation(refKey(preparationRef(response.preparation)));
      setSelectedFrozen(
        refKey({
          type: "bas_frozen",
          id: response.frozenBas.frozenId,
          revision: 1n,
          contentHash: response.frozenBas.contentHash,
        }),
      );
      setMessage(
        `Frozen exact handoff ${response.frozenBas.frozenId}. Tammy has not lodged this BAS.`,
      );
      setTotp("");
      refresh();
    });

  const replaceUnusedAttempt = () =>
    execute(async () => {
      const selectedPreparation = selectedPreparationSummary?.preparation;
      if (
        !workspace ||
        !canApprove ||
        !prep ||
        !currentChain ||
        !selectedFrozenSummary?.frozenBas ||
        !selectedPreparation ||
        !replacementReason.trim()
      )
        throw new Error("replacement unavailable");
      const exact = await refreshSelectedPreparation(selectedPreparation);
      if (
        !exact.currentChain ||
        !exact.preparation ||
        exact.preparation.state !== BasPreparationState.FROZEN_FOR_MANUAL_HANDOFF ||
        !matchesFrozenTarget(exact.preparation, selectedFrozenSummary)
      )
        throw new Error("attempt is not replaceable");
      const command = await freshCommand("bas_replace_unused_attempt");
      const request = create(CreateBasReplacementRequestSchema, {
        commandContext: command,
        organisationId: identity().organisationId,
        expectedVersion: exact.currentChain.expectedFamilyVersion,
        chain: exact.currentChain,
        predecessorFrozenId: selectedFrozenSummary.frozenBas.id,
        predecessorFrozenHash: selectedFrozenSummary.frozenBas.contentHash,
        reason: replacementReason.trim(),
      });
      const response = replacementCodec.decodeResponse(
        await api.createBasReplacement(replacementCodec.encodeRequest(request)),
      );
      if (!response.replacement) throw new Error("missing replacement preparation");
      setSelectedPreparation(refKey(preparationRef(response.replacement)));
      setMessage(`Created replacement preparation ${response.replacement?.preparationId ?? ""}.`);
      setTotp("");
      refresh();
    });

  const createRevision = () =>
    execute(async () => {
      const selectedPreparation = selectedPreparationSummary?.preparation;
      if (
        !workspace ||
        !canPrepare ||
        !prep ||
        !currentChain ||
        !selectedPreparation ||
        !selectedProfileSummary?.profile ||
        !selectedPeriodSummary?.period ||
        !revisionReason.trim()
      )
        throw new Error("revision unavailable");
      const exact = await refreshSelectedPreparation(selectedPreparation);
      if (
        !exact.preparation ||
        exact.effectiveOriginal?.type !== "bas_preparation" ||
        !exact.effectiveOriginal.id ||
        exact.effectiveOriginal.contentHash.byteLength !== 32 ||
        !exact.currentChain?.latestHead ||
        exact.currentChain.latestHead.expectedState.case !== "retained"
      )
        throw new Error("chain unavailable");
      const command = ordinaryCommand();
      const request = create(CreateBasRevisionRequestSchema, {
        commandContext: command,
        organisationId: identity().organisationId,
        expectedVersion: exact.currentChain.expectedFamilyVersion,
        chain: exact.currentChain,
        effectiveOriginalPreparationId: exact.effectiveOriginal.id,
        effectiveOriginalPreparationHash: exact.effectiveOriginal.contentHash,
        latestConfirmedHeadId: exact.currentChain.latestHead.expectedState.value.id,
        latestConfirmedHeadHash: exact.currentChain.latestHead.expectedState.value.contentHash,
        period: exactRef(selectedPeriodSummary.period),
        profile: exactRef(selectedProfileSummary.profile),
        reason: revisionReason.trim(),
      });
      const response = revisionCodec.decodeResponse(
        await api.createBasRevision(revisionCodec.encodeRequest(request)),
      );
      if (!response.revision?.preparationId || response.revision.contentHash.byteLength !== 32)
        throw new Error("missing retained revision");
      setSelectedPreparation(
        refKey({
          type: "bas_preparation",
          id: response.revision.preparationId,
          revision: response.revision.version,
          contentHash: response.revision.contentHash,
        }),
      );
      setMessage(
        `Created a BAS revision ${response.revision?.preparationId ?? ""} linked to the selected confirmed head.`,
      );
      refresh();
    });

  const recordObservation = () =>
    execute(async () => {
      const requiresLodger =
        eventKind === BasExternalEventKind.COMPLETED_LODGEMENT ||
        eventKind === BasExternalEventKind.ACCEPTANCE;
      const selectedPreparation = selectedPreparationSummary?.preparation;
      if (
        !workspace ||
        !(requiresLodger ? canApprove : canPrepare) ||
        !selectedFrozenSummary?.frozenBas ||
        !prep ||
        !currentChain ||
        !selectedPreparation ||
        !eventReason.trim()
      )
        throw new Error("frozen target or required role unavailable");
      const exact = await refreshSelectedPreparation(selectedPreparation);
      if (
        !exact.currentChain ||
        exact.preparation?.version !== prep.version ||
        !matchesFrozenTarget(exact.preparation, selectedFrozenSummary)
      )
        throw new Error("chain changed");
      const refs = parseEvidence(evidence);
      const factorCommand = requiresLodger
        ? await freshCommand("bas_record_external_confirmation")
        : ordinaryCommand();
      const observation = create(BasExternalObservationInputSchema, {
        eventKind,
        channel,
        claimedEventAt: create(TimestampSchema, {
          seconds: BigInt(Math.floor(new Date(`${claimedDate}T00:00:00Z`).getTime() / 1000)),
          nanos: 0,
        }),
        ...(externalReference.trim() ? { externalReference: externalReference.trim() } : {}),
        evidence: refs,
        reason: eventReason.trim(),
      });
      const request = create(RecordExternalBasObservationRequestSchema, {
        commandContext: factorCommand,
        organisationId: identity().organisationId,
        expectedVersion: exact.currentChain.expectedFamilyVersion,
        chain: exact.currentChain,
        frozenId: selectedFrozenSummary.frozenBas.id,
        frozenHash: selectedFrozenSummary.frozenBas.contentHash,
        observation,
      });
      const response = recordObservationCodec.decodeResponse(
        await api.recordExternalBasObservation(recordObservationCodec.encodeRequest(request)),
      );
      if (!response.preparation) throw new Error("missing preparation after external record");
      setSelectedPreparation(refKey(preparationRef(response.preparation)));
      setMessage(
        `Recorded ${eventLabel(eventKind)} as USER_RECORDED_UNVERIFIED. This is not ATO verification.`,
      );
      setTotp("");
      refresh();
    });

  const correctObservation = () =>
    execute(async () => {
      const selectedPreparation = selectedPreparationSummary?.preparation;
      if (
        !workspace ||
        !canApprove ||
        !selectedFrozenSummary?.frozenBas ||
        !prep ||
        !currentChain ||
        !selectedPreparation ||
        !correctionObservation ||
        !correctionReason.trim()
      )
        throw new Error("correction unavailable");
      const disputed = observations.find(
        (entry) => refKey(entry.observation) === correctionObservation,
      )?.observation;
      if (!disputed) throw new Error("observation unavailable");
      const exact = await refreshSelectedPreparation(selectedPreparation);
      if (!exact.currentChain) throw new Error("chain unavailable");
      const input = create(BasExternalObservationCorrectionInputSchema, {
        disputedObservation: exactRef(disputed),
        correctionKind,
        ...(correctionKind === BasCorrectionKind.REPLACE_ASSERTION ? { correctedEventKind } : {}),
        evidence: parseEvidence(evidence),
        reason: correctionReason.trim(),
      });
      const request = create(CorrectExternalBasObservationRequestSchema, {
        commandContext: ordinaryCommand(),
        organisationId: identity().organisationId,
        expectedVersion: exact.currentChain.expectedFamilyVersion,
        chain: exact.currentChain,
        frozenId: selectedFrozenSummary.frozenBas.id,
        frozenHash: selectedFrozenSummary.frozenBas.contentHash,
        correction: input,
      });
      const result = correctObservationCodec.decodeResponse(
        await api.correctExternalBasObservation(correctObservationCodec.encodeRequest(request)),
      );
      setMessage(`Recorded correction ${result.correction?.observationId ?? ""}.`);
      refresh();
    });

  const resolveDispute = () =>
    execute(async () => {
      const selectedPreparation = selectedPreparationSummary?.preparation;
      if (
        !workspace ||
        !canApprove ||
        !prep ||
        !currentChain ||
        !selectedPreparation ||
        !disputeReason.trim()
      )
        throw new Error("dispute details required");
      const exact = await refreshSelectedPreparation(selectedPreparation);
      const dispute = exact.currentDispute;
      if (
        !exact.currentChain ||
        exact.currentDisputeOverlay !== BasDisputeOverlayState.DISPUTED ||
        !dispute ||
        dispute.disputedObservations.length === 0 ||
        dispute.dependentChainManifestHash.byteLength !== 32
      )
        throw new Error("no exact contested external history");
      const input = create(BasExternalDisputeInputSchema, {
        outcome: disputeOutcome,
        disputedObservations: dispute.disputedObservations.map(exactRef),
        affectedConfirmedChain: dispute.affectedConfirmedChain.map(exactRef),
        dependentChainManifestHash: Uint8Array.from(dispute.dependentChainManifestHash),
        evidence: parseEvidence(evidence),
        reason: disputeReason.trim(),
      });
      const command = await freshCommand("bas_resolve_external_dispute");
      const request = create(ResolveExternalBasDisputeRequestSchema, {
        commandContext: command,
        organisationId: identity().organisationId,
        expectedVersion: exact.currentChain.expectedFamilyVersion,
        chain: exact.currentChain,
        resolution: input,
      });
      const response = resolveDisputeCodec.decodeResponse(
        await api.resolveExternalBasDispute(resolveDisputeCodec.encodeRequest(request)),
      );
      setMessage(`Recorded dispute resolution ${response.resolution?.resolutionId ?? ""}.`);
      setTotp("");
      refresh();
    });

  const replaceRejected = () =>
    execute(async () => {
      if (
        !workspace ||
        !canApprove ||
        !selectedFrozenSummary?.frozenBas ||
        !selectedFrozenSummary.preparation ||
        !rejectedReplacementReason.trim()
      )
        throw new Error("rejected attempt required");
      const rejected = preparations.find(
        (item) => item.preparation?.id === selectedFrozenSummary.preparation?.id,
      )?.preparation;
      const rejection = observations.find(
        (entry) =>
          entry.eventKind === BasExternalEventKind.REJECTION &&
          entry.frozenBas?.id === selectedFrozenSummary.frozenBas?.id &&
          sameBytes(entry.frozenBas?.contentHash, selectedFrozenSummary.frozenBas?.contentHash),
      )?.observation;
      if (!rejected || !rejection) throw new Error("rejection evidence unavailable");
      const exact = await refreshSelectedPreparation(rejected);
      if (
        !exact.currentChain ||
        exact.preparation?.state !== BasPreparationState.FROZEN_FOR_MANUAL_HANDOFF ||
        !matchesFrozenTarget(exact.preparation, selectedFrozenSummary)
      )
        throw new Error("chain unavailable");
      const command = await freshCommand("bas_replace_rejected_attempt");
      const request = create(ReplaceRejectedBasAttemptRequestSchema, {
        commandContext: command,
        organisationId: identity().organisationId,
        expectedVersion: exact.currentChain.expectedFamilyVersion,
        chain: exact.currentChain,
        rejectedPreparationId: rejected.id,
        rejectedPreparationHash: rejected.contentHash,
        resolvedRejection: exactRef(rejection),
        reason: rejectedReplacementReason.trim(),
      });
      const response = rejectedReplacementCodec.decodeResponse(
        await api.replaceRejectedBasAttempt(rejectedReplacementCodec.encodeRequest(request)),
      );
      setMessage(
        `Created replacement for rejected attempt ${response.replacementAttempt?.preparationId ?? ""}.`,
      );
      setTotp("");
      refresh();
    });

  const tabs: Stage[] = ["Prepare", "Review", "Freeze", "Export", "External record"];
  if (!workspace?.organisationId || !auth)
    return (
      <section
        aria-label="BAS lifecycle"
        className="rounded-lg border border-border bg-surface p-4"
      >
        Sign in to prepare a BAS.
      </section>
    );

  return (
    <section aria-label="BAS preparation lifecycle" className="grid gap-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-[18px] font-semibold tracking-[-0.02em]">BAS preparation</h1>
          <p className="mt-1 max-w-[760px] text-[11px] leading-5 text-muted-foreground">
            Period-led preparation, exact review snapshots and purpose-bound manual approval. Tammy
            never submits BAS information to the ATO.
          </p>
        </div>
        <p className="rounded border border-border bg-background px-3 py-2 text-[10px]">
          {lodgerCopy}
        </p>
      </div>
      <nav aria-label="BAS lifecycle stages" className="flex flex-wrap gap-2">
        {tabs.map((tab) => (
          <Button
            aria-current={stage === tab ? "step" : undefined}
            className="h-8 text-[10px]"
            key={tab}
            onClick={() => setStage(tab)}
            type="button"
            variant={stage === tab ? "default" : "outline"}
          >
            {tab}
          </Button>
        ))}
        <Button
          className="ml-auto h-8 text-[10px]"
          disabled={loading || busy}
          onClick={refresh}
          type="button"
          variant="outline"
        >
          {loading ? "Refreshing…" : "Refresh exact history"}
        </Button>
      </nav>
      {prep ? (
        <p
          className="break-words rounded border border-border bg-background px-3 py-2 text-[9px]"
          data-testid="bas-preparation-creation-reason"
        >
          <strong>Retained preparation reason:</strong>{" "}
          {prep.creationReason || "No reason was recorded."}
        </p>
      ) : null}
      {initialPreparationStatus === "pending" ? (
        <p
          className="rounded border border-border bg-background px-3 py-2 text-[10px]"
          role="status"
        >
          Resolving the exact BAS preparation link…
        </p>
      ) : null}
      {initialPreparationStatus === "stale" ? (
        <p
          className="rounded border border-warning-border bg-warning-soft px-3 py-2 text-[10px]"
          role="alert"
        >
          This exact BAS preparation link is stale or unavailable. No newer preparation was
          selected. Return to Overview or choose a retained version from history.
        </p>
      ) : null}
      {initialPreparationStatus === "sources-unavailable" ? (
        <p
          className="rounded border border-warning-border bg-warning-soft px-3 py-2 text-[10px]"
          role="alert"
        >
          The exact preparation is retained, but one or more exact profile, period, or frozen source
          identities could not be resolved. No substitute source was selected.
        </p>
      ) : null}
      {initialPreparationStatus === "unavailable" ? (
        <p
          className="rounded border border-warning-border bg-warning-soft px-3 py-2 text-[10px]"
          role="alert"
        >
          The exact BAS preparation could not be checked right now. No newer preparation was
          selected; refresh to retry.
        </p>
      ) : null}
      {error ? (
        <p
          className="rounded border border-warning-border bg-warning-soft px-3 py-2 text-[10px]"
          role="alert"
        >
          {error}
        </p>
      ) : null}
      {message ? (
        <p
          className="rounded border border-success-border bg-success-soft px-3 py-2 text-[10px]"
          role="status"
        >
          {message}
        </p>
      ) : null}
      {stage === "Prepare" ? (
        <PrepareStage
          canPrepare={canPrepare}
          canManageRuleReference={canManageRuleReference}
          identityApi={identityApi}
          onRuleReferenceUpdated={() => {
            refresh();
            void onChanged?.();
          }}
          workspace={workspace}
          profiles={profiles}
          selectedProfile={selectedProfile}
          periods={periods}
          selectedPeriod={selectedPeriod}
          suggestions={suggestions}
          selectedSuggestion={selectedSuggestion}
          setSelectedProfile={setSelectedProfile}
          setSelectedPeriod={setSelectedPeriod}
          effectiveStart={effectiveStart}
          setEffectiveStart={setEffectiveStart}
          effectiveEnd={effectiveEnd}
          setEffectiveEnd={setEffectiveEnd}
          gstRegistrationStart={gstRegistrationStart}
          setGstRegistrationStart={setGstRegistrationStart}
          gstRegistrationEnd={gstRegistrationEnd}
          setGstRegistrationEnd={setGstRegistrationEnd}
          profileReason={profileReason}
          setProfileReason={setProfileReason}
          gstApplicability={gstApplicability}
          setGstApplicability={setGstApplicability}
          gstStatus={gstStatus}
          setGstStatus={setGstStatus}
          gstMethod={gstMethod}
          setGstMethod={setGstMethod}
          turnover={turnover}
          setTurnover={setTurnover}
          gstBasis={gstBasis}
          setGstBasis={setGstBasis}
          gstCadence={gstCadence}
          setGstCadence={setGstCadence}
          gstReason={gstReason}
          setGstReason={setGstReason}
          paygwApplicability={paygwApplicability}
          setPaygwApplicability={setPaygwApplicability}
          paygwCategory={paygwCategory}
          setPaygwCategory={setPaygwCategory}
          paygwCadence={paygwCadence}
          setPaygwCadence={setPaygwCadence}
          paygwPrefill={paygwPrefill}
          setPaygwPrefill={setPaygwPrefill}
          paygwReason={paygwReason}
          setPaygwReason={setPaygwReason}
          paygiApplicability={paygiApplicability}
          setPaygiApplicability={setPaygiApplicability}
          paygiMethod={paygiMethod}
          setPaygiMethod={setPaygiMethod}
          paygiCadence={paygiCadence}
          setPaygiCadence={setPaygiCadence}
          paygiNoticeStatus={paygiNoticeStatus}
          setPaygiNoticeStatus={setPaygiNoticeStatus}
          paygiPriorVariation={paygiPriorVariation}
          setPaygiPriorVariation={setPaygiPriorVariation}
          paygiReason={paygiReason}
          setPaygiReason={setPaygiReason}
          otherAnswers={otherAnswers}
          setOtherAnswers={setOtherAnswers}
          evidence={evidence}
          setEvidence={setEvidence}
          actualDueDateEvidence={actualDueDateEvidence}
          setActualDueDateEvidence={setActualDueDateEvidence}
          supportState={supportState}
          setSupportState={setSupportState}
          periodReason={periodReason}
          setPeriodReason={setPeriodReason}
          dueDateKind={dueDateKind}
          setDueDateKind={setDueDateKind}
          confirmedDueDate={confirmedDueDate}
          setConfirmedDueDate={setConfirmedDueDate}
          paygwInputs={paygwInputs}
          setPaygwInputs={setPaygwInputs}
          paygwStates={paygwStates}
          setPaygwStates={setPaygwStates}
          paygiAmount={paygiAmount}
          setPaygiAmount={setPaygiAmount}
          paygiAmountState={paygiAmountState}
          setPaygiAmountState={setPaygiAmountState}
          paygiRate={paygiRate}
          setPaygiRate={setPaygiRate}
          incomeYearId={incomeYearId}
          setIncomeYearId={setIncomeYearId}
          incomeYearStart={incomeYearStart}
          setIncomeYearStart={setIncomeYearStart}
          incomeYearEnd={incomeYearEnd}
          setIncomeYearEnd={setIncomeYearEnd}
          selectedPreparation={prep}
          onSaveProfile={saveProfile}
          onConfirmPeriod={confirmSelectedPeriod}
          onRegenerate={regenerate}
          onSaveInputs={saveInputs}
          busy={busy || loading}
        />
      ) : null}
      {stage === "Review" ? (
        <ReviewStage
          canPrepare={canPrepare}
          preparation={prep}
          calculation={calculation}
          validation={validation}
          liveCurrentness={preparationDetail?.liveCurrentness}
          summaries={preparations}
          selected={selectedPreparation}
          setSelected={setSelectedPreparation}
          acknowledged={acknowledged}
          setAcknowledged={setAcknowledged}
          onValidate={validatePreparation}
          onAcknowledge={ackWarnings}
          busy={busy}
        />
      ) : null}
      {stage === "Freeze" ? (
        <FreezeStage
          canApprove={canApprove}
          preparations={preparations}
          selected={selectedPreparation}
          setSelected={setSelectedPreparation}
          preparation={prep}
          liveCurrentness={preparationDetail?.liveCurrentness}
          frozen={frozen}
          selectedFrozen={selectedFrozen}
          setSelectedFrozen={setSelectedFrozen}
          frozenLiveCurrentness={frozenDetail?.liveCurrentness}
          approvalReason={approvalReason}
          setApprovalReason={setApprovalReason}
          replacementReason={replacementReason}
          setReplacementReason={setReplacementReason}
          totp={totp}
          setTotp={setTotp}
          onFreeze={freezePreparation}
          onReplace={replaceUnusedAttempt}
          busy={busy}
        />
      ) : null}
      {stage === "Export" ? (
        <ExportStage
          exportKind={exportTargetKind}
          setExportKind={setExportTargetKind}
          format={exportFormat}
          setFormat={setExportFormat}
          preparations={preparations}
          selectedPreparation={selectedPreparation}
          setSelectedPreparation={setSelectedPreparation}
          selectedCalculation={selectedPreparationSummary?.currentCalculation}
          frozen={frozen}
          selectedFrozen={selectedFrozen}
          setSelectedFrozen={setSelectedFrozen}
          detail={frozenDetail?.frozenBas}
          frozenLiveCurrentness={frozenDetail?.liveCurrentness}
          events={exportEvents}
          eventsLoading={exportEventsLoading}
          eventsError={exportEventsError}
          onRefreshHistory={() => setExportHistoryReload((value) => value + 1)}
          onExport={() => void exportExactTarget()}
          busy={busy}
        />
      ) : null}
      {stage === "External record" ? (
        <ExternalStage
          frozen={frozen}
          selectedFrozen={selectedFrozen}
          setSelectedFrozen={setSelectedFrozen}
          observations={observations}
          disputes={disputes}
          eventKind={eventKind}
          setEventKind={setEventKind}
          channel={channel}
          setChannel={setChannel}
          claimedDate={claimedDate}
          setClaimedDate={setClaimedDate}
          externalReference={externalReference}
          setExternalReference={setExternalReference}
          eventReason={eventReason}
          setEventReason={setEventReason}
          evidence={evidence}
          setEvidence={setEvidence}
          correctionObservation={correctionObservation}
          setCorrectionObservation={setCorrectionObservation}
          correctionKind={correctionKind}
          setCorrectionKind={setCorrectionKind}
          correctedEventKind={correctedEventKind}
          setCorrectedEventKind={setCorrectedEventKind}
          currentDispute={preparationDetail?.currentDispute}
          currentDisputeOverlay={preparationDetail?.currentDisputeOverlay}
          currentBranchResolution={preparationDetail?.currentBranchResolution}
          currentFrozenBarrier={preparationDetail?.currentFrozenBarrier}
          disputeOutcome={disputeOutcome}
          setDisputeOutcome={setDisputeOutcome}
          preparation={prep}
          currentChain={currentChain}
          selectedProfile={selectedProfileSummary?.profile}
          selectedPeriod={selectedPeriodSummary?.period}
          onCreateRevision={createRevision}
          revisionReason={revisionReason}
          setRevisionReason={setRevisionReason}
          correctionReason={correctionReason}
          setCorrectionReason={setCorrectionReason}
          disputeReason={disputeReason}
          setDisputeReason={setDisputeReason}
          rejectedReplacementReason={rejectedReplacementReason}
          setRejectedReplacementReason={setRejectedReplacementReason}
          totp={totp}
          setTotp={setTotp}
          canPrepare={canPrepare}
          canApprove={canApprove}
          onRecord={recordObservation}
          onCorrect={correctObservation}
          onResolve={resolveDispute}
          onReplaceRejected={replaceRejected}
          busy={busy}
        />
      ) : null}
    </section>
  );
}

// The form below is intentionally explicit: unanswered areas remain UNKNOWN until a user decides otherwise.
function PrepareStage(props: any) {
  const {
    canPrepare,
    canManageRuleReference,
    identityApi,
    onRuleReferenceUpdated,
    workspace,
    profiles,
    selectedProfile,
    periods,
    selectedPeriod,
    suggestions,
    selectedSuggestion,
    setSelectedProfile,
    setSelectedPeriod,
    effectiveStart,
    setEffectiveStart,
    effectiveEnd,
    setEffectiveEnd,
    gstRegistrationStart,
    setGstRegistrationStart,
    gstRegistrationEnd,
    setGstRegistrationEnd,
    profileReason,
    setProfileReason,
    gstApplicability,
    setGstApplicability,
    gstStatus,
    setGstStatus,
    gstMethod,
    setGstMethod,
    turnover,
    setTurnover,
    gstBasis,
    setGstBasis,
    gstCadence,
    setGstCadence,
    gstReason,
    setGstReason,
    paygwApplicability,
    setPaygwApplicability,
    paygwCategory,
    setPaygwCategory,
    paygwCadence,
    setPaygwCadence,
    paygwPrefill,
    setPaygwPrefill,
    paygwReason,
    setPaygwReason,
    paygiApplicability,
    setPaygiApplicability,
    paygiMethod,
    setPaygiMethod,
    paygiCadence,
    setPaygiCadence,
    paygiNoticeStatus,
    setPaygiNoticeStatus,
    paygiPriorVariation,
    setPaygiPriorVariation,
    paygiReason,
    setPaygiReason,
    otherAnswers,
    setOtherAnswers,
    evidence,
    setEvidence,
    actualDueDateEvidence,
    setActualDueDateEvidence,
    supportState,
    setSupportState,
    periodReason,
    setPeriodReason,
    dueDateKind,
    setDueDateKind,
    confirmedDueDate,
    setConfirmedDueDate,
    paygwInputs,
    setPaygwInputs,
    paygwStates,
    setPaygwStates,
    paygiAmount,
    setPaygiAmount,
    paygiAmountState,
    setPaygiAmountState,
    paygiRate,
    setPaygiRate,
    incomeYearId,
    setIncomeYearId,
    incomeYearStart,
    setIncomeYearStart,
    incomeYearEnd,
    setIncomeYearEnd,
    selectedPreparation,
    onSaveProfile,
    onConfirmPeriod,
    onRegenerate,
    onSaveInputs,
    busy,
  } = props;
  const profileEvidenceComplete =
    /^[a-z][a-z0-9_]{0,63}$/.test(evidence.type) &&
    UUID.test(evidence.id) &&
    /^[1-9]\d{0,19}$/.test(evidence.revision) &&
    BigInt(evidence.revision || "0") <= 18446744073709551615n &&
    HASH.test(evidence.hash);
  const paygiMethodNeedsIncomeYear =
    paygiMethod === BasPaygiMethod.AMOUNT || paygiMethod === BasPaygiMethod.RATE;
  const hasDecidedObligation =
    [gstApplicability, paygwApplicability, paygiApplicability, ...Object.values(otherAnswers)].some(
      (value) => value !== BasApplicability.UNKNOWN,
    ) ||
    gstStatus !== BasRegistrationStatus.UNKNOWN ||
    paygiMethodNeedsIncomeYear;
  const profileIntervalValid =
    Boolean(effectiveStart) && (!effectiveEnd || effectiveEnd >= effectiveStart);
  const gstRegistrationIntervalValid =
    gstStatus !== BasRegistrationStatus.REGISTERED ||
    (Boolean(gstRegistrationStart) &&
      (!gstRegistrationEnd || gstRegistrationEnd >= gstRegistrationStart));
  const incomeYearDatesOrdered =
    !paygiMethodNeedsIncomeYear ||
    !incomeYearStart ||
    !incomeYearEnd ||
    incomeYearEnd >= incomeYearStart;
  const gstStatusMatchesApplicability =
    (gstApplicability === BasApplicability.UNKNOWN ||
      (gstApplicability === BasApplicability.ACTIVE &&
        gstStatus === BasRegistrationStatus.REGISTERED) ||
      (gstApplicability === BasApplicability.NOT_APPLICABLE &&
        gstStatus === BasRegistrationStatus.NOT_REGISTERED)) &&
    (gstStatus !== BasRegistrationStatus.REGISTERED || Boolean(gstRegistrationStart));
  const incomeYearIdentityComplete =
    UUID.test(incomeYearId) && Boolean(incomeYearStart) && Boolean(incomeYearEnd);
  const paygiProfileIncomeYearComplete = !paygiMethodNeedsIncomeYear || incomeYearIdentityComplete;
  const paygiProfileMethodConsistent =
    (paygiApplicability !== BasApplicability.NOT_APPLICABLE ||
      paygiMethod === BasPaygiMethod.NOT_APPLICABLE) &&
    (paygiApplicability !== BasApplicability.ACTIVE ||
      paygiMethod !== BasPaygiMethod.NOT_APPLICABLE);
  const decidedAnswerReasonsPresent =
    (gstApplicability === BasApplicability.UNKNOWN || Boolean(gstReason.trim())) &&
    (paygwApplicability === BasApplicability.UNKNOWN || Boolean(paygwReason.trim())) &&
    (paygiApplicability === BasApplicability.UNKNOWN || Boolean(paygiReason.trim()));
  const canSaveProfile =
    canPrepare &&
    !busy &&
    Boolean(profileReason.trim()) &&
    decidedAnswerReasonsPresent &&
    profileIntervalValid &&
    gstRegistrationIntervalValid &&
    gstStatusMatchesApplicability &&
    (!hasDecidedObligation || profileEvidenceComplete) &&
    paygiProfileIncomeYearComplete &&
    incomeYearDatesOrdered &&
    paygiProfileMethodConsistent;
  const selectedPeriodItem = periods.find(
    (item: BasPeriodSummary) => refKey(item.period) === selectedPeriod,
  );
  return (
    <div className="grid gap-4">
      <LegacyRuleReferenceRepair
        api={identityApi}
        canManage={canManageRuleReference}
        onUpdated={onRuleReferenceUpdated}
        workspace={workspace}
      />
      <section className="grid gap-3 rounded border border-border bg-surface p-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-[12px] font-semibold">Obligations profile</h2>
          <label className="grid gap-1 text-[9px]">
            Selected immutable profile
            <select
              className="h-8 min-w-[240px] rounded border border-border bg-background px-2"
              onChange={(event) => setSelectedProfile(event.target.value)}
              value={selectedProfile ?? ""}
            >
              <option value="">No profile yet</option>
              {profiles.map((item: BasObligationsProfileSummary) => (
                <option key={refKey(item.profile)} value={refKey(item.profile)}>
                  Revision {item.profile?.revision} ·{" "}
                  {item.effectiveInterval?.startDate
                    ? iso(item.effectiveInterval.startDate)
                    : "unknown start"}
                </option>
              ))}
            </select>
          </label>
        </div>
        <p className="text-[9px] leading-4 text-muted-foreground">
          Profiles are retained as immutable revisions. Unknown means the question is still
          unresolved; it does not mean inactive.
        </p>
        <div className="grid grid-cols-2 gap-3 md:grid-cols-3">
          <DateInput label="Effective from" value={effectiveStart} setValue={setEffectiveStart} />
          <DateInput
            label="Effective through (optional)"
            value={effectiveEnd}
            setValue={setEffectiveEnd}
          />
          <EnumField
            label="GST obligation"
            value={gstApplicability}
            setValue={setGstApplicability}
            options={applicabilityOptions}
          />
          <EnumField
            label="GST registration status"
            value={gstStatus}
            setValue={setGstStatus}
            options={enumOptions(BasRegistrationStatus, [
              "UNKNOWN",
              "REGISTERED",
              "NOT_REGISTERED",
            ])}
          />
          <EnumField
            label="GST reporting method"
            value={gstMethod}
            setValue={setGstMethod}
            options={enumOptions(BasGstMethod, [
              "UNKNOWN",
              "SIMPLER",
              "FULL",
              "INSTALMENT",
              "UNSUPPORTED",
            ])}
          />
          <EnumField
            label="GST turnover category"
            value={turnover}
            setValue={setTurnover}
            options={enumOptions(BasTurnoverCategory, [
              "UNKNOWN",
              "SMALL",
              "MEDIUM",
              "LARGE",
              "UNSUPPORTED",
            ])}
          />
          <EnumField
            label="GST basis"
            value={gstBasis}
            setValue={setGstBasis}
            options={enumOptions(BasGstBasis, ["UNKNOWN", "CASH", "ACCRUAL", "UNSUPPORTED"])}
          />
          <EnumField
            label="GST cadence"
            value={gstCadence}
            setValue={setGstCadence}
            options={enumOptions(BasCadence, [
              "UNKNOWN",
              "MONTHLY",
              "QUARTERLY",
              "ANNUAL",
              "MIXED",
              "UNSUPPORTED",
            ])}
          />
          <TextInput label="GST decision reason" value={gstReason} setValue={setGstReason} />
        </div>
        {!effectiveStart ? (
          <p className="text-[9px] text-warning" role="note">
            Enter the profile effective start date before saving.
          </p>
        ) : effectiveEnd && effectiveEnd < effectiveStart ? (
          <p className="text-[9px] text-warning" role="note">
            The profile end date must be on or after its start date.
          </p>
        ) : null}
        {gstStatus === BasRegistrationStatus.REGISTERED ? (
          <div className="grid gap-2">
            <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
              <DateInput
                label="GST registration effective from"
                value={gstRegistrationStart}
                setValue={setGstRegistrationStart}
              />
              <DateInput
                label="GST registration effective through (optional)"
                value={gstRegistrationEnd}
                setValue={setGstRegistrationEnd}
              />
            </div>
            {!gstRegistrationStart ? (
              <p className="text-[9px] text-warning" role="note">
                Enter the retained GST registration start date. This is separate from the profile
                effective date and is required before a registered profile can be saved.
              </p>
            ) : gstRegistrationEnd && gstRegistrationEnd < gstRegistrationStart ? (
              <p className="text-[9px] text-warning" role="note">
                The GST registration end date must be on or after its start date.
              </p>
            ) : null}
          </div>
        ) : null}
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <EnumField
            label="PAYGW obligation"
            value={paygwApplicability}
            setValue={setPaygwApplicability}
            options={applicabilityOptions}
          />
          <EnumField
            label="PAYGW category"
            value={paygwCategory}
            setValue={setPaygwCategory}
            options={enumOptions(BasPaygwCategory, [
              "UNKNOWN",
              "SMALL",
              "MEDIUM",
              "LARGE",
              "UNSUPPORTED",
            ])}
          />
          <EnumField
            label="PAYGW cadence"
            value={paygwCadence}
            setValue={setPaygwCadence}
            options={enumOptions(BasCadence, [
              "UNKNOWN",
              "MONTHLY",
              "QUARTERLY",
              "ANNUAL",
              "MIXED",
              "UNSUPPORTED",
            ])}
          />
          <EnumField
            label="PAYGW prefill requirement"
            value={paygwPrefill}
            setValue={setPaygwPrefill}
            options={enumOptions(BasPrefillRequirement, [
              "UNKNOWN",
              "NOT_REQUIRED",
              "REVIEWED",
              "UNAVAILABLE",
              "UNSUPPORTED",
            ])}
          />
          <TextInput label="PAYGW reason" value={paygwReason} setValue={setPaygwReason} />
          <EnumField
            label="PAYGI obligation"
            value={paygiApplicability}
            setValue={setPaygiApplicability}
            options={applicabilityOptions}
          />
          <EnumField
            label="PAYGI method"
            value={paygiMethod}
            setValue={setPaygiMethod}
            options={enumOptions(BasPaygiMethod, [
              "UNKNOWN",
              "AMOUNT",
              "RATE",
              "NOT_APPLICABLE",
              "UNSUPPORTED",
            ])}
          />
          <EnumField
            label="PAYGI cadence"
            value={paygiCadence}
            setValue={setPaygiCadence}
            options={enumOptions(BasCadence, [
              "UNKNOWN",
              "MONTHLY",
              "QUARTERLY",
              "ANNUAL",
              "MIXED",
              "UNSUPPORTED",
            ])}
          />
          <TextInput label="PAYGI reason" value={paygiReason} setValue={setPaygiReason} />
        </div>
        {!paygiProfileMethodConsistent ? (
          <p className="text-[9px] text-warning" role="note">
            Not-applicable PAYGI must use the not-applicable method. Active PAYGI cannot use that
            method.
          </p>
        ) : null}
        <div className="grid gap-2 rounded border border-border bg-background p-3">
          <h3 className="text-[10px] font-semibold">Other activity statement obligations</h3>
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            {OTHER_OBLIGATIONS.map(([key, label]) => (
              <EnumField
                key={key}
                label={label}
                value={otherAnswers[key] ?? BasApplicability.UNKNOWN}
                setValue={(value: BasApplicability) =>
                  setOtherAnswers((current: Record<string, BasApplicability>) => ({
                    ...current,
                    [key]: value,
                  }))
                }
                options={applicabilityOptions}
              />
            ))}
          </div>
        </div>
        {paygiMethodNeedsIncomeYear ? (
          <div className="grid gap-2 rounded border border-border bg-background p-3">
            <h3 className="text-[10px] font-semibold">PAYGI confirmed income year</h3>
            <p className="text-[9px] leading-4 text-muted-foreground">
              This income-year identity is retained in the obligations profile and reused for the
              later PAYG input review. The first shared evidence identity below confirms this year.
            </p>
            <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
              <TextInput
                label="Confirmed income year UUID"
                value={incomeYearId}
                setValue={setIncomeYearId}
              />
              <DateInput
                label="Income year start"
                value={incomeYearStart}
                setValue={setIncomeYearStart}
              />
              <DateInput
                label="Income year end"
                value={incomeYearEnd}
                setValue={setIncomeYearEnd}
              />
            </div>
            {!incomeYearIdentityComplete ? (
              <p className="text-[9px] text-warning" role="note">
                Enter a canonical income-year UUID and both calendar dates before saving this amount
                or rate method or its later PAYG input.
              </p>
            ) : incomeYearEnd < incomeYearStart ? (
              <p className="text-[9px] text-warning" role="note">
                The income year end date must be on or after its start date.
              </p>
            ) : null}
          </div>
        ) : null}
        <EvidenceEditor
          value={evidence}
          setValue={setEvidence}
          label="Shared retained profile evidence identity"
        />
        <p className="text-[9px] leading-4 text-muted-foreground">
          This exact source is attached to GST, PAYGW, PAYGI and each selected active or not
          applicable answer in the other-obligations inventory. It also confirms the PAYGI income
          year. Unknown answers remain unresolved and receive no reason or evidence.
        </p>
        {hasDecidedObligation && !profileEvidenceComplete ? (
          <p className="text-[9px] text-warning" role="note">
            Enter a complete retained source identity before saving a decided obligation or selected
            PAYGI amount or rate method.
          </p>
        ) : null}
        {!gstStatusMatchesApplicability ? (
          <p className="text-[9px] text-warning" role="note">
            Active GST requires a registered status and registration start date. Not-applicable GST
            requires a not-registered status.
          </p>
        ) : null}
        <TextInput
          label="Profile decision reason"
          value={profileReason}
          setValue={setProfileReason}
        />
        <p className="text-[9px] leading-4 text-muted-foreground">
          This reviewed profile reason is also retained as the reason for every selected active or
          not applicable answer in the other-obligations inventory.
        </p>
        <Button
          className="h-8 w-fit text-[10px]"
          disabled={!canSaveProfile}
          onClick={onSaveProfile}
          type="button"
        >
          Save immutable profile revision
        </Button>
      </section>

      <section className="grid gap-3 rounded border border-border bg-surface p-3">
        <h2 className="text-[12px] font-semibold">Period selection and due date</h2>
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <label className="grid gap-1 text-[9px]">
            Confirmed period history
            <select
              className="h-8 rounded border border-border bg-background px-2"
              onChange={(event) => setSelectedPeriod(event.target.value)}
              value={selectedPeriod ?? ""}
            >
              <option value="">Choose a calendar suggestion</option>
              {periods.map((item: BasPeriodSummary) => (
                <option key={refKey(item.period)} value={refKey(item.period)}>
                  Confirmed · {iso(item.dates?.startDate)} – {iso(item.dates?.endDate)} ·{" "}
                  {item.period?.id}
                </option>
              ))}
              {suggestions.map((item: BasPeriodSuggestion) => (
                <option key={suggestionKey(item)} value={suggestionKey(item)}>
                  Suggestion · {iso(item.period?.startDate)} – {iso(item.period?.endDate)} · due{" "}
                  {iso(item.nominalDueDate)}
                </option>
              ))}
            </select>
          </label>
          <EnumField
            label="Period support state"
            value={supportState}
            setValue={setSupportState}
            options={enumOptions(BasPeriodSupportState, [
              "SUPPORTED",
              "PART_PERIOD_REGISTRATION",
              "CADENCE_CHANGE",
              "MIXED_WINDOW",
              "ANNUAL_METHOD",
              "OTHER_UNSUPPORTED",
            ])}
          />
          <EnumField
            label="Due-date basis"
            value={dueDateKind}
            setValue={setDueDateKind}
            options={enumOptions(BasDueDateKind, [
              "NORMAL_SUGGESTION",
              "USER_RECORDED_EXTERNAL_DATE",
              "USER_RECORDED_DEFERRAL",
            ])}
          />
          <DateInput
            label="Confirmed due date"
            value={
              confirmedDueDate || (selectedSuggestion ? iso(selectedSuggestion.nominalDueDate) : "")
            }
            setValue={setConfirmedDueDate}
          />
        </div>
        {selectedSuggestion ? (
          <p className="text-[9px] leading-4 text-muted-foreground">
            Calendar suggestion uses retained rule {selectedSuggestion.dueDateRuleVersion} ·{" "}
            {hex(selectedSuggestion.dueDateRuleHash)}. Confirm an externally sourced date or
            deferral only with exact retained evidence. The calendar date is guidance; an exact
            actual-deadline source is needed for the period to remain supported. Without it, Core
            records the confirmation as unsupported.
          </p>
        ) : null}
        <EvidenceEditor
          value={actualDueDateEvidence}
          setValue={setActualDueDateEvidence}
          label="Actual statement due-date evidence identity"
        />
        <TextInput
          label="Period confirmation reason"
          value={periodReason}
          setValue={setPeriodReason}
        />
        <Button
          className="h-8 w-fit text-[10px]"
          disabled={!canPrepare || busy || !selectedSuggestion || !periodReason.trim()}
          onClick={onConfirmPeriod}
          type="button"
        >
          Confirm selected period
        </Button>
        {selectedPeriodItem ? (
          <p className="text-[9px] text-muted-foreground">
            Exact selected period {selectedPeriodItem.period?.id} · revision{" "}
            {selectedPeriodItem.period?.revision} · confirmed due{" "}
            {iso(selectedPeriodItem.confirmedDueDate)}.
          </p>
        ) : null}
      </section>

      <section className="grid gap-3 rounded border border-border bg-surface p-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <h2 className="text-[12px] font-semibold">Prepare this period</h2>
            <p className="mt-1 text-[9px] text-muted-foreground">
              Regeneration retains an immutable exact calculation and reports incomplete coverage as
              blockers or warnings.
            </p>
          </div>
          <Button
            className="h-8 text-[10px]"
            disabled={!canPrepare || busy || !selectedPeriodItem || !selectedProfile}
            onClick={onRegenerate}
            type="button"
          >
            Regenerate exact calculation
          </Button>
        </div>
        {selectedPreparation ? (
          <p className="break-all text-[9px] text-muted-foreground">
            Selected preparation {selectedPreparation.preparationId} · version{" "}
            {selectedPreparation.version} · {hashField("input hash", selectedPreparation.inputHash)}
          </p>
        ) : (
          <p className="text-[9px] text-muted-foreground">
            Select a confirmed period and profile, then prepare the first retained calculation.
          </p>
        )}
      </section>

      {selectedPreparation ? (
        <section className="grid gap-3 rounded border border-border bg-surface p-3">
          <h2 className="text-[12px] font-semibold">PAYGW and PAYGI labels</h2>
          <p className="text-[9px] text-muted-foreground">
            Each label is entered as unknown, not applicable, known zero or an exact positive
            amount. Evidence stays linked to the saved input snapshot.
          </p>
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            {(
              [
                ["w1", "W1 total payments"],
                ["w2", "W2 amount withheld"],
                ["w3", "W3 other withholding"],
                ["w4", "W4 no-ABN withholding"],
              ] as const
            ).map(([key, label]) => (
              <div className="grid gap-2" key={key}>
                <EnumField
                  label={`${label} state`}
                  value={paygwStates[key]}
                  setValue={(value: BasAmountState) =>
                    setPaygwStates((current: typeof paygwStates) => ({ ...current, [key]: value }))
                  }
                  options={amountOptions}
                />
                {paygwStates[key] === BasAmountState.KNOWN ||
                paygwStates[key] === BasAmountState.KNOWN_ZERO ? (
                  <TextInput
                    label={`${label} exact AUD`}
                    value={paygwInputs[key]}
                    setValue={(value: string) =>
                      setPaygwInputs((current: typeof paygwInputs) => ({
                        ...current,
                        [key]: value,
                      }))
                    }
                  />
                ) : null}
              </div>
            ))}
          </div>
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            <EnumField
              label="PAYGI method for this input"
              value={paygiMethod}
              setValue={setPaygiMethod}
              options={enumOptions(BasPaygiMethod, [
                "UNKNOWN",
                "AMOUNT",
                "RATE",
                "NOT_APPLICABLE",
                "UNSUPPORTED",
              ])}
            />
            <EnumField
              label="PAYGI amount state"
              value={paygiAmountState}
              setValue={setPaygiAmountState}
              options={amountOptions}
            />
            {paygiAmountState === BasAmountState.KNOWN ||
            paygiAmountState === BasAmountState.KNOWN_ZERO ? (
              <TextInput
                label="PAYGI exact AUD (T7 or T1)"
                value={paygiAmount}
                setValue={setPaygiAmount}
              />
            ) : null}
            {paygiMethod === BasPaygiMethod.RATE ? (
              <TextInput
                label="PAYGI exact T2 rate (e.g. 15.5)"
                value={paygiRate}
                setValue={setPaygiRate}
              />
            ) : null}
          </div>
          {paygiMethod === BasPaygiMethod.AMOUNT || paygiMethod === BasPaygiMethod.RATE ? (
            <div className="grid gap-2">
              <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
                <EnumField
                  label="PAYGI notice status"
                  value={paygiNoticeStatus}
                  setValue={setPaygiNoticeStatus}
                  options={enumOptions(BasNoticeOperativeStatus, [
                    "UNKNOWN",
                    "OPERATIVE",
                    "INOPERATIVE",
                  ])}
                />
                <EnumField
                  label="PAYGI prior variation"
                  value={paygiPriorVariation}
                  setValue={setPaygiPriorVariation}
                  options={enumOptions(BasPriorVariationDisposition, [
                    "UNKNOWN",
                    "NONE",
                    "REFLECTED_IN_CURRENT_NOTICE",
                    "UNCERTAIN",
                    "NEW",
                  ])}
                />
              </div>
              <p className="text-[8px] leading-4 text-muted-foreground">
                An active PAYGI notice can be used only after its operativeness and prior variation
                status are confirmed from the retained evidence. Unknown stays blocked; uncertain or
                new variation handling remains unsupported.
              </p>
            </div>
          ) : null}
          <TextInput
            label="PAYG review reason"
            value={paygwInputs.reason}
            setValue={(value: string) =>
              setPaygwInputs((current: typeof paygwInputs) => ({ ...current, reason: value }))
            }
          />
          <Button
            className="h-8 w-fit text-[10px]"
            disabled={!canPrepare || busy}
            onClick={onSaveInputs}
            type="button"
          >
            Save exact PAYG inputs
          </Button>
        </section>
      ) : null}
    </div>
  );
}

function ReviewStage(props: any) {
  const {
    canPrepare,
    preparation,
    calculation,
    validation,
    liveCurrentness,
    summaries,
    selected,
    setSelected,
    acknowledged,
    setAcknowledged,
    onValidate,
    onAcknowledge,
    busy,
  } = props;
  const selectOptions = summaries
    .map((item: BasPreparationSummary) => item.preparation)
    .filter(Boolean) as Ref[];
  return (
    <div className="grid gap-4">
      <section className="grid gap-3 rounded border border-border bg-surface p-3">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 className="text-[12px] font-semibold">Exact review snapshot</h2>
            <p className="mt-1 max-w-3xl text-[9px] leading-4 text-muted-foreground">
              Validation is pinned to the calculation, input hash and source snapshot. Freeze
              re-reads this exact identity before asking for the fresh factor.
            </p>
          </div>
          <label className="grid gap-1 text-[9px]">
            Preparation history
            <select
              className="h-8 min-w-[340px] rounded border border-border bg-background px-2"
              onChange={(event) => setSelected(event.target.value)}
              value={selected ?? ""}
            >
              {selectOptions.map((item) => (
                <option key={refKey(item)} value={refKey(item)}>
                  {item.id} · v{item.revision}
                </option>
              ))}
            </select>
          </label>
        </div>
        {preparation ? (
          <dl className="grid grid-cols-[160px_minmax(0,1fr)] gap-x-3 gap-y-1 break-all text-[9px]">
            <dt>Retained state</dt>
            <dd>{preparationStateLabel(preparation.state)}</dd>
            <dt>Live source currentness</dt>
            <dd>{currentnessLabel(liveCurrentness)}</dd>
            <dt>Preparation</dt>
            <dd>
              {preparation.preparationId} · v{preparation.version} · {hex(preparation.contentHash)}
            </dd>
            <dt>Input identity</dt>
            <dd>
              {preparation.inputRevision} · {hex(preparation.inputHash)}
            </dd>
            <dt>Source snapshot</dt>
            <dd>{hex(preparation.sourceSnapshotHash)}</dd>
            <dt>Calculation</dt>
            <dd>
              {calculation
                ? `${calculation.calculationId} · ${hex(calculation.contentHash)}`
                : "No calculation is attached."}
            </dd>
            <dt>Current validation</dt>
            <dd>
              {validation
                ? `${validation.validationId} · ${hex(validation.contentHash)}`
                : "Not validated yet."}
            </dd>
          </dl>
        ) : (
          <p className="text-[10px]">Select a retained preparation to review.</p>
        )}
        <Button
          className="h-8 w-fit text-[10px]"
          disabled={!canPrepare || busy || !preparation || !calculation}
          onClick={onValidate}
          type="button"
        >
          Run validation on this exact calculation
        </Button>
      </section>
      {calculation ? (
        <section className="overflow-hidden rounded border border-border bg-surface">
          <div className="border-b border-border p-3">
            <h3 className="text-[11px] font-semibold">Calculated BAS labels</h3>
            <p className="mt-1 text-[9px] text-muted-foreground">
              Exact cents and separately reported whole dollars come from the retained calculation
              traces.
            </p>
          </div>
          <div className="overflow-x-auto">
            <table
              aria-label="Exact BAS calculation labels"
              className="w-full text-left text-[9px]"
            >
              <thead>
                <tr className="border-b border-border">
                  <th className="p-2">Label</th>
                  <th className="p-2">State</th>
                  <th className="p-2">Exact cents</th>
                  <th className="p-2">Reported whole dollars</th>
                  <th className="p-2">Rounding difference</th>
                  <th className="p-2">Rule</th>
                </tr>
              </thead>
              <tbody>
                {calculation.result?.labels.map((item: BasLabelTrace) => (
                  <tr
                    className="border-b border-border last:border-0"
                    key={`${item.label}:${item.ruleId}`}
                  >
                    <td className="p-2 font-semibold">{formLabel(item.label)}</td>
                    <td className="p-2">{amountStateLabel(item.state)}</td>
                    <td className="p-2">
                      {item.exactCentsNumerator
                        ? `${item.exactCentsNumerator}/${item.exactCentsDenominator}`
                        : item.exactRateNumerator
                          ? `${item.exactRateNumerator}/${item.exactRateDenominator} rate`
                          : "—"}
                    </td>
                    <td className="p-2">{item.reportedWholeDollars?.toString() ?? "—"}</td>
                    <td className="p-2">
                      {item.roundingDifferenceCentsNumerator
                        ? `${item.roundingDifferenceCentsNumerator}/${item.roundingDifferenceCentsDenominator} cents`
                        : "—"}
                    </td>
                    <td className="p-2">{item.ruleId}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      ) : null}
      {validation ? (
        <section className="grid gap-3 rounded border border-border bg-surface p-3">
          <h3 className="text-[11px] font-semibold">Review findings</h3>
          {validation.findings.length === 0 ? (
            <p className="text-[9px] text-muted-foreground">
              No findings were recorded in this exact validation.
            </p>
          ) : (
            validation.findings.map((finding: BasValidationFinding) => (
              <label
                className="flex items-start gap-2 rounded border border-border bg-background p-2 text-[9px]"
                key={finding.findingId}
              >
                <input
                  checked={acknowledged.includes(finding.findingId)}
                  disabled={finding.severity !== BasFindingSeverity.WARNING}
                  onChange={(event) =>
                    setAcknowledged((current: string[]) =>
                      event.target.checked
                        ? [...current, finding.findingId]
                        : current.filter((id) => id !== finding.findingId),
                    )
                  }
                  type="checkbox"
                />
                <span>
                  <strong>
                    {finding.severity === BasFindingSeverity.BLOCKER
                      ? "Blocker"
                      : finding.severity === BasFindingSeverity.WARNING
                        ? "Warning"
                        : finding.severity === BasFindingSeverity.UNSUPPORTED
                          ? "Unsupported"
                          : "Information"}{" "}
                    · {finding.stableCode}
                  </strong>
                  <br />
                  {finding.safeMessage}
                  <br />
                  {finding.stableCode === "MANUAL_TAX_FACT_UNSUPPORTED" ? (
                    <span className="font-semibold text-amber-700">
                      External review required: this manual accounting tax treatment is unsupported
                      and must be reviewed externally before inclusion. Tammy will not infer, carry
                      forward, or reclassify it.
                    </span>
                  ) : null}
                  {finding.stableCode === "MANUAL_TAX_FACT_UNSUPPORTED" ? <br /> : null}
                  <span className="break-all text-muted-foreground">
                    Finding {finding.findingId} · source snapshot{" "}
                    {hex(validation.sourceSnapshotHash)}
                  </span>
                </span>
              </label>
            ))
          )}
          <p className="text-[9px] leading-4 text-muted-foreground">
            Warning acknowledgement hashes are calculated from the selected protobuf finding and
            bound to this validation and source snapshot. Blockers and unsupported findings cannot
            be acknowledged away.
          </p>
          <Button
            className="h-8 w-fit text-[10px]"
            disabled={!canPrepare || busy || acknowledged.length === 0}
            onClick={onAcknowledge}
            type="button"
          >
            Acknowledge selected exact warnings
          </Button>
        </section>
      ) : null}
    </div>
  );
}

function FreezeStage(props: any) {
  const {
    canApprove,
    preparations,
    selected,
    setSelected,
    preparation,
    liveCurrentness,
    frozen,
    selectedFrozen,
    setSelectedFrozen,
    frozenLiveCurrentness,
    approvalReason,
    setApprovalReason,
    replacementReason,
    setReplacementReason,
    totp,
    setTotp,
    onFreeze,
    onReplace,
    busy,
  } = props;
  return (
    <div className="grid gap-4">
      <section className="grid gap-3 rounded border border-border bg-surface p-3">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 className="text-[12px] font-semibold">Freeze for manual handoff</h2>
            <p className="mt-1 max-w-3xl text-[9px] leading-4 text-muted-foreground">
              Only a Business Lodger can approve this action. Workspace admins can prepare and read
              BAS records, but cannot use approval actions. The command is bound to a fresh TOTP
              assertion for this exact freeze purpose.
            </p>
          </div>
          <label className="grid gap-1 text-[9px]">
            Preparation
            <select
              className="h-8 min-w-[340px] rounded border border-border bg-background px-2"
              onChange={(event) => setSelected(event.target.value)}
              value={selected ?? ""}
            >
              {preparations.map((item: BasPreparationSummary) => (
                <option key={refKey(item.preparation)} value={refKey(item.preparation)}>
                  {item.preparation?.id} · {preparationStateLabel(item.state)}
                </option>
              ))}
            </select>
          </label>
        </div>
        {preparation ? (
          <p className="break-all text-[9px] text-muted-foreground">
            Exact review target {preparation.preparationId} · v{preparation.version} ·{" "}
            {hex(preparation.contentHash)} · retained state{" "}
            {preparationStateLabel(preparation.state)} · live source currentness{" "}
            {currentnessLabel(liveCurrentness)}
          </p>
        ) : null}
        {liveCurrentness !== BasLiveCurrentness.CURRENT ? (
          <p className="rounded border border-warning-border bg-warning-soft p-2 text-[9px]">
            The live source set is not confirmed current. Refresh the exact record before approval;
            a retained review-ready state does not establish currentness.
          </p>
        ) : null}
        <TextInput label="Approval reason" value={approvalReason} setValue={setApprovalReason} />
        <TextInput
          label="Authenticator code"
          value={totp}
          setValue={setTotp}
          inputMode="numeric"
          maxLength={6}
        />
        <p className="text-[9px] text-muted-foreground">
          This local action creates a frozen handoff snapshot. It does not lodge or submit a BAS.
        </p>
        <Button
          className="h-8 w-fit text-[10px]"
          disabled={
            !canApprove ||
            busy ||
            !preparation ||
            preparation.state !== BasPreparationState.REVIEW_READY ||
            liveCurrentness !== BasLiveCurrentness.CURRENT ||
            !/^[0-9]{6}$/.test(totp) ||
            !approvalReason.trim()
          }
          onClick={onFreeze}
          type="button"
        >
          Approve and freeze exact snapshot
        </Button>
      </section>
      <section className="grid gap-3 rounded border border-border bg-surface p-3">
        <h2 className="text-[12px] font-semibold">Frozen identity and replacements</h2>
        <label className="grid gap-1 text-[9px]">
          Frozen history
          <select
            className="h-8 rounded border border-border bg-background px-2"
            onChange={(event) => setSelectedFrozen(event.target.value)}
            value={selectedFrozen ?? ""}
          >
            <option value="">Select frozen handoff</option>
            {frozen.map((item: BasFrozenBasSummary) => (
              <option key={refKey(item.frozenBas)} value={refKey(item.frozenBas)}>
                {item.frozenBas?.id} · {item.frozenBas?.revision}
              </option>
            ))}
          </select>
        </label>
        {frozen.map((item: BasFrozenBasSummary) => (
          <p className="break-all text-[9px] text-muted-foreground" key={refKey(item.frozenBas)}>
            {item.frozenBas?.id} · {hex(item.frozenBas?.contentHash)} · preparation{" "}
            {item.preparation?.id}
          </p>
        ))}
        {selectedFrozen ? (
          <p className="text-[9px] text-muted-foreground">
            Selected frozen snapshot live source currentness:{" "}
            {currentnessLabel(frozenLiveCurrentness)}. This is a new read status and does not change
            the frozen approval record.
          </p>
        ) : null}
        <p className="text-[9px] text-muted-foreground">
          An unused frozen attempt can be replaced only while no external completion has been
          recorded. The original frozen identity remains in history.
        </p>
        <TextInput
          label="Replacement reason"
          value={replacementReason}
          setValue={setReplacementReason}
        />
        <Button
          className="h-8 w-fit text-[10px]"
          disabled={
            !canApprove ||
            busy ||
            !preparation ||
            !selectedFrozen ||
            !/^[0-9]{6}$/.test(totp) ||
            !replacementReason.trim()
          }
          onClick={onReplace}
          type="button"
        >
          Replace unused frozen attempt (fresh TOTP)
        </Button>
      </section>
    </div>
  );
}

function ExportStage(props: {
  readonly exportKind: "calculation" | "frozen_bas";
  readonly setExportKind: (value: "calculation" | "frozen_bas") => void;
  readonly format: "pdf" | "csv";
  readonly setFormat: (value: "pdf" | "csv") => void;
  readonly preparations: readonly BasPreparationSummary[];
  readonly selectedPreparation: string;
  readonly setSelectedPreparation: (value: string) => void;
  readonly selectedCalculation: Ref | undefined;
  readonly frozen: readonly BasFrozenBasSummary[];
  readonly selectedFrozen: string;
  readonly setSelectedFrozen: (value: string) => void;
  readonly detail: ReturnType<typeof frozenDetailCodec.decodeResponse>["frozenBas"];
  readonly frozenLiveCurrentness?: BasLiveCurrentness | undefined;
  readonly events: readonly BasExportEventSummary[];
  readonly eventsLoading: boolean;
  readonly eventsError: boolean;
  readonly onRefreshHistory: () => void;
  readonly onExport: () => void;
  readonly busy: boolean;
}) {
  const {
    exportKind,
    setExportKind,
    format,
    setFormat,
    preparations,
    selectedPreparation,
    setSelectedPreparation,
    selectedCalculation,
    frozen,
    selectedFrozen,
    setSelectedFrozen,
    detail,
    frozenLiveCurrentness,
    events,
    eventsLoading,
    eventsError,
    onRefreshHistory,
    onExport,
    busy,
  } = props;
  return (
    <div className="grid gap-4">
      <section className="grid gap-3 rounded border border-border bg-surface p-3">
        <h2 className="text-[12px] font-semibold">Export an exact retained BAS version</h2>
        <p className="text-[9px] leading-4 text-muted-foreground">
          The report reads only the selected immutable calculation or frozen snapshot identity. It
          does not read current profile or workpaper data and does not lodge the BAS.
        </p>
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <label className="grid gap-1 text-[9px]">
            Exact report source
            <select
              className="h-8 rounded border border-border bg-background px-2"
              onChange={(event) =>
                setExportKind(event.target.value as "calculation" | "frozen_bas")
              }
              value={exportKind}
            >
              <option value="frozen_bas">Frozen manual handoff</option>
              <option value="calculation">Retained calculation</option>
            </select>
          </label>
          <label className="grid gap-1 text-[9px]">
            Format
            <select
              className="h-8 rounded border border-border bg-background px-2"
              onChange={(event) => setFormat(event.target.value as "pdf" | "csv")}
              value={format}
            >
              <option value="pdf">PDF</option>
              <option value="csv">CSV</option>
            </select>
          </label>
          {exportKind === "frozen_bas" ? (
            <label className="grid gap-1 text-[9px]">
              Frozen BAS identity
              <select
                className="h-8 rounded border border-border bg-background px-2"
                onChange={(event) => setSelectedFrozen(event.target.value)}
                value={selectedFrozen}
              >
                <option value="">Choose frozen version</option>
                {frozen.map((item) => (
                  <option key={refKey(item.frozenBas)} value={refKey(item.frozenBas)}>
                    {item.frozenBas?.id} · {hex(item.frozenBas?.contentHash)}
                  </option>
                ))}
              </select>
            </label>
          ) : (
            <label className="grid gap-1 text-[9px]">
              Calculation preparation
              <select
                className="h-8 rounded border border-border bg-background px-2"
                onChange={(event) => setSelectedPreparation(event.target.value)}
                value={selectedPreparation}
              >
                <option value="">Choose retained preparation</option>
                {preparations
                  .filter((item) => item.currentCalculation)
                  .map((item) => (
                    <option key={refKey(item.preparation)} value={refKey(item.preparation)}>
                      {item.preparation?.id} · {item.currentCalculation?.id}
                    </option>
                  ))}
              </select>
            </label>
          )}
        </div>
        {exportKind === "frozen_bas" && detail ? (
          <dl className="grid grid-cols-[140px_minmax(0,1fr)] gap-x-3 gap-y-1 break-all text-[9px]">
            <dt>Frozen BAS</dt>
            <dd>
              {detail.frozenId} · {hex(detail.contentHash)}
            </dd>
            <dt>Calculation</dt>
            <dd>
              {detail.calculation?.id} · {hex(detail.calculation?.contentHash)}
            </dd>
            <dt>Approved</dt>
            <dd>
              {detail.approvalActorUserId} · {dateTimeLabel(detail.approvedAt)}
            </dd>
            <dt>Approval reason</dt>
            <dd>{detail.approvalReason}</dd>
            <dt>Live source currentness</dt>
            <dd>{currentnessLabel(frozenLiveCurrentness)}</dd>
          </dl>
        ) : null}
        {exportKind === "calculation" && selectedCalculation ? (
          <p className="break-all text-[9px] text-muted-foreground">
            Exact calculation target {selectedCalculation.id} · revision{" "}
            {selectedCalculation.revision} · {hex(selectedCalculation.contentHash)}.
          </p>
        ) : null}
        <p className="rounded border border-warning-border bg-warning-soft p-2 text-[9px]">
          A frozen report is labelled “manual handoff — not lodged.” Live source currentness is a
          separate read and does not alter the retained frozen approval. Export history stores the
          exact target, format, result and file hash; it does not store the destination path or file
          contents.
        </p>
        <Button
          className="h-8 w-fit text-[10px]"
          disabled={busy || (exportKind === "frozen_bas" ? !selectedFrozen : !selectedCalculation)}
          onClick={onExport}
          type="button"
        >
          {busy ? "Preparing exact report…" : `Export ${format.toUpperCase()}`}
        </Button>
      </section>
      <section
        aria-label="Exact-target export history"
        className="grid gap-2 rounded border border-border bg-surface p-3"
      >
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <h2 className="text-[12px] font-semibold">Exact-target export history</h2>
            <p className="text-[9px] text-muted-foreground">
              Only events for the selected retained target are shown. Destination paths and report
              bytes are not stored.
            </p>
          </div>
          <Button
            className="h-7 text-[9px]"
            disabled={eventsLoading}
            onClick={onRefreshHistory}
            type="button"
            variant="outline"
          >
            {eventsLoading ? "Loading history…" : "Refresh event history"}
          </Button>
        </div>
        {eventsError ? (
          <p className="text-[9px] text-warning-foreground" role="alert">
            Export history could not be loaded. Retry without changing the selected target.
          </p>
        ) : eventsLoading ? (
          <p className="text-[9px] text-muted-foreground">
            Reading all pages for this exact target…
          </p>
        ) : events.length === 0 ? (
          <p className="text-[9px] text-muted-foreground">
            No export events for this exact target.
          </p>
        ) : (
          <ul className="grid gap-1">
            {events.map((event) => (
              <li
                className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 border-t border-border py-2 text-[9px]"
                key={refKey(event.exportEvent)}
              >
                <span className="break-all">
                  <strong>
                    {exportFormatLabel(event.format)} ·{" "}
                    {exportEventStatus(event.phase, event.outcome)}
                  </strong>{" "}
                  · event {event.exportEvent?.id ?? "identity unavailable"}
                </span>
                <time dateTime={timestampIso(event.recordedAt)}>
                  {dateTimeLabel(event.recordedAt)}
                </time>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}

function ExternalStage(props: any) {
  const {
    frozen,
    selectedFrozen,
    setSelectedFrozen,
    observations,
    disputes,
    eventKind,
    setEventKind,
    channel,
    setChannel,
    claimedDate,
    setClaimedDate,
    externalReference,
    setExternalReference,
    eventReason,
    setEventReason,
    evidence,
    setEvidence,
    correctionObservation,
    setCorrectionObservation,
    correctionKind,
    setCorrectionKind,
    correctedEventKind,
    setCorrectedEventKind,
    currentDispute,
    currentDisputeOverlay,
    currentBranchResolution,
    currentFrozenBarrier,
    disputeOutcome,
    setDisputeOutcome,
    preparation,
    correctionReason,
    setCorrectionReason,
    disputeReason,
    setDisputeReason,
    rejectedReplacementReason,
    setRejectedReplacementReason,
    revisionReason,
    setRevisionReason,
    selectedProfile,
    selectedPeriod,
    currentChain,
    onCreateRevision,
    totp,
    setTotp,
    canPrepare,
    canApprove,
    onRecord,
    onCorrect,
    onResolve,
    onReplaceRejected,
    busy,
  } = props;
  const needsFactor =
    eventKind === BasExternalEventKind.COMPLETED_LODGEMENT ||
    eventKind === BasExternalEventKind.ACCEPTANCE;
  const canRecordEvent = needsFactor ? canApprove : canPrepare;
  const hasCurrentDispute =
    currentDisputeOverlay === BasDisputeOverlayState.DISPUTED &&
    currentDispute?.disputedObservations?.length > 0 &&
    currentDispute?.dependentChainManifestHash?.byteLength === 32;
  const hasConfirmedHead = currentChain?.latestHead?.expectedState?.case === "retained";
  const hasCorrectionTarget = observations.some(
    (item: BasExternalObservationSummary) => refKey(item.observation) === correctionObservation,
  );
  const hasRejectedObservation = observations.some(
    (item: BasExternalObservationSummary) => item.eventKind === BasExternalEventKind.REJECTION,
  );

  return (
    <div className="grid gap-4">
      <section className="grid gap-3 rounded border border-border bg-surface p-3">
        <h2 className="text-[12px] font-semibold">
          Record what the user says happened outside Tammy
        </h2>
        <p className="text-[9px] leading-4 text-muted-foreground">
          Every event is stored as USER_RECORDED_UNVERIFIED. Tammy cannot confirm that the ATO
          received, processed or accepted anything. Pending, unknown, rejection, reviewer handoff,
          payment and refund events do not advance the confirmed chain.
        </p>
        <p className="text-[9px] leading-4 text-muted-foreground">
          Completed lodgement and acceptance require a Business Lodger and a fresh confirmation
          factor. Other user-recorded events require a preparer or workspace admin. Corrections
          remain Business Lodger-only.
        </p>
        <label className="grid gap-1 text-[9px]">
          Frozen BAS
          <select
            className="h-8 rounded border border-border bg-background px-2"
            onChange={(event) => setSelectedFrozen(event.target.value)}
            value={selectedFrozen ?? ""}
          >
            <option value="">Choose frozen version</option>
            {frozen.map((item: BasFrozenBasSummary) => (
              <option key={refKey(item.frozenBas)} value={refKey(item.frozenBas)}>
                {item.frozenBas?.id} · {item.frozenBas?.revision}
              </option>
            ))}
          </select>
        </label>
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <EnumField
            label="User-recorded event"
            value={eventKind}
            setValue={setEventKind}
            options={enumOptions(BasExternalEventKind, [
              "COMPLETED_LODGEMENT",
              "ACCEPTANCE",
              "PENDING",
              "UNKNOWN",
              "REJECTION",
              "REVIEWER_HANDOFF",
              "PAYMENT",
              "REFUND",
            ])}
          />
          <EnumField
            label="Channel"
            value={channel}
            setValue={setChannel}
            options={enumOptions(BasExternalChannel, [
              "ONLINE_PORTAL",
              "TAX_AGENT",
              "PAPER",
              "PHONE",
              "OTHER",
            ])}
          />
          <DateInput label="Claimed event date" value={claimedDate} setValue={setClaimedDate} />
          <TextInput
            label="External reference (optional)"
            value={externalReference}
            setValue={setExternalReference}
          />
        </div>
        <TextInput label="User-recorded reason" value={eventReason} setValue={setEventReason} />
        <EvidenceEditor
          value={evidence}
          setValue={setEvidence}
          label="Exact retained evidence identity (optional)"
        />
        {needsFactor ? (
          <TextInput
            label="Fresh authenticator code for external confirmation"
            value={totp}
            setValue={setTotp}
            inputMode="numeric"
            maxLength={6}
          />
        ) : null}
        <Button
          className="h-8 w-fit text-[10px]"
          disabled={
            !canRecordEvent ||
            busy ||
            !selectedFrozen ||
            !eventReason.trim() ||
            (needsFactor && !/^[0-9]{6}$/.test(totp))
          }
          onClick={onRecord}
          type="button"
        >
          Record user-reported event as unverified
        </Button>
        <p className="break-all text-[9px] text-muted-foreground">
          Current preparation {preparation?.preparationId ?? "—"} ·{" "}
          {preparation ? preparationStateLabel(preparation.state) : "no exact selected preparation"}
        </p>
      </section>

      <section className="grid gap-3 rounded border border-border bg-surface p-3">
        <h2 className="text-[12px] font-semibold">External history and corrections</h2>
        {observations.length === 0 ? (
          <p className="text-[9px] text-muted-foreground">
            No external observations for this frozen identity. Refresh will preserve the selected
            frozen target.
          </p>
        ) : (
          observations.map((item: BasExternalObservationSummary) => (
            <div className="border-b border-border py-2 text-[9px]" key={refKey(item.observation)}>
              <strong>
                {item.eventKind === undefined ? "Correction" : eventLabel(item.eventKind)} ·
                USER_RECORDED_UNVERIFIED
              </strong>
              <br />
              <span className="break-all">
                {item.observation?.id} · {hex(item.observation?.contentHash)} ·{" "}
                {dateTimeLabel(item.recordedAt)}
              </span>
            </div>
          ))
        )}
        <label className="grid gap-1 text-[9px]">
          Correct a retained observation
          <select
            className="h-8 rounded border border-border bg-background px-2"
            onChange={(event) => setCorrectionObservation(event.target.value)}
            value={correctionObservation ?? ""}
          >
            <option value="">Choose exact observation</option>
            {observations.map((item: BasExternalObservationSummary) => (
              <option key={refKey(item.observation)} value={refKey(item.observation)}>
                {item.observation?.id} ·{" "}
                {item.eventKind === undefined ? "correction" : eventLabel(item.eventKind)}
              </option>
            ))}
          </select>
        </label>
        <div className="grid grid-cols-2 gap-3">
          <EnumField
            label="Correction type"
            value={correctionKind}
            setValue={setCorrectionKind}
            options={enumOptions(BasCorrectionKind, ["RETRACT_AS_INCORRECT", "REPLACE_ASSERTION"])}
          />
          {correctionKind === BasCorrectionKind.REPLACE_ASSERTION ? (
            <EnumField
              label="Corrected event kind"
              value={correctedEventKind}
              setValue={setCorrectedEventKind}
              options={enumOptions(BasExternalEventKind, [
                "COMPLETED_LODGEMENT",
                "ACCEPTANCE",
                "PENDING",
                "UNKNOWN",
                "REJECTION",
                "REVIEWER_HANDOFF",
                "PAYMENT",
                "REFUND",
              ])}
            />
          ) : null}
        </div>
        <TextInput
          label="Correction reason"
          value={correctionReason}
          setValue={setCorrectionReason}
        />
        <p className="text-[9px] text-muted-foreground">
          Correcting a retained assertion is a Business Lodger action. It preserves the original
          observation and does not verify what the ATO did.
        </p>
        <Button
          className="h-8 w-fit text-[10px]"
          disabled={!canApprove || busy || !hasCorrectionTarget || !correctionReason.trim()}
          onClick={onCorrect}
          type="button"
        >
          Record correction (retains prior event)
        </Button>
      </section>

      <section className="grid gap-3 rounded border border-border bg-surface p-3">
        <h2 className="text-[12px] font-semibold">Create a revision from the confirmed chain</h2>
        <p className="text-[9px] text-muted-foreground">
          A revision starts from the exact latest confirmed head and effective original supplied by
          the server. Choose the applicable retained profile and period in Prepare.
        </p>
        <p className="break-all text-[9px] text-muted-foreground">
          Selected profile {selectedProfile?.id ?? "—"} · period {selectedPeriod?.id ?? "—"} ·{" "}
          {hasConfirmedHead
            ? `confirmed head ${currentChain.latestHead.expectedState.value.id}`
            : "no confirmed head available"}
        </p>
        <TextInput label="Revision reason" value={revisionReason} setValue={setRevisionReason} />
        <Button
          className="h-8 w-fit text-[10px]"
          disabled={
            !canPrepare ||
            busy ||
            !hasConfirmedHead ||
            !selectedProfile ||
            !selectedPeriod ||
            !revisionReason.trim()
          }
          onClick={onCreateRevision}
          type="button"
        >
          Create revision from exact confirmed head
        </Button>
      </section>

      <section className="grid gap-3 rounded border border-border bg-surface p-3">
        <h2 className="text-[12px] font-semibold">Resolve a contested external history</h2>
        <p className="text-[9px] text-muted-foreground">
          Current family overlay: {disputeOverlayLabel(currentDisputeOverlay)} · branch resolution:{" "}
          {currentBranchResolution?.id ?? "none"} · frozen barrier:{" "}
          {currentFrozenBarrier?.id ?? "none"}.
        </p>
        {hasCurrentDispute ? (
          <div className="grid gap-2 rounded border border-border bg-background p-2 text-[9px]">
            <p>
              The server derived these exact dispute subjects and dependency proof from the full
              retained history. Tammy will submit this ordered set unchanged.
            </p>
            <div>
              <strong>Disputed observations</strong>
              <ul className="list-disc pl-5">
                {currentDispute.disputedObservations.map((ref: Ref) => (
                  <li className="break-all" key={refKey(ref)}>
                    {ref.type} · {ref.id} · {hex(ref.contentHash)}
                  </li>
                ))}
              </ul>
            </div>
            <div>
              <strong>Affected confirmed chain</strong>
              <ul className="list-disc pl-5">
                {currentDispute.affectedConfirmedChain.map((ref: Ref) => (
                  <li className="break-all" key={refKey(ref)}>
                    {ref.type} · {ref.id} · {hex(ref.contentHash)}
                  </li>
                ))}
              </ul>
            </div>
            <p className="break-all">
              Dependency manifest SHA-256: {hex(currentDispute.dependentChainManifestHash)}
            </p>
          </div>
        ) : (
          <p className="text-[9px] text-muted-foreground">
            No current contested history is available for this exact preparation. Record or correct
            a user-reported external event, then refresh the retained history.
          </p>
        )}
        <EnumField
          label="Resolution outcome"
          value={disputeOutcome}
          setValue={setDisputeOutcome}
          options={enumOptions(BasDisputeResolutionOutcome, [
            "UPHOLD_RECORDED_CHAIN",
            "REQUIRE_CORRECTIVE_REVISION",
          ])}
        />
        <TextInput
          label="Dispute resolution reason"
          value={disputeReason}
          setValue={setDisputeReason}
        />
        <TextInput
          label="Fresh authenticator code for dispute resolution"
          value={totp}
          setValue={setTotp}
          inputMode="numeric"
          maxLength={6}
        />
        <Button
          className="h-8 w-fit text-[10px]"
          disabled={
            !canApprove ||
            busy ||
            !hasCurrentDispute ||
            !disputeReason.trim() ||
            !/^[0-9]{6}$/.test(totp)
          }
          onClick={onResolve}
          type="button"
        >
          Resolve exact contested history (fresh TOTP)
        </Button>
      </section>

      <section className="grid gap-3 rounded border border-border bg-surface p-3">
        <h2 className="text-[12px] font-semibold">Replace a rejected attempt</h2>
        <p className="text-[9px] text-muted-foreground">
          Replacement is enabled only when the selected exact attempt has a retained rejection
          observation; it does not erase that attempt.
        </p>
        <TextInput
          label="Rejected-attempt replacement reason"
          value={rejectedReplacementReason}
          setValue={setRejectedReplacementReason}
        />
        <TextInput
          label="Fresh authenticator code"
          value={totp}
          setValue={setTotp}
          inputMode="numeric"
          maxLength={6}
        />
        <Button
          className="h-8 w-fit text-[10px]"
          disabled={
            !canApprove ||
            busy ||
            !selectedFrozen ||
            !hasRejectedObservation ||
            !/^[0-9]{6}$/.test(totp) ||
            !rejectedReplacementReason.trim()
          }
          onClick={onReplaceRejected}
          type="button"
        >
          Replace selected rejected attempt (fresh TOTP)
        </Button>
        {disputes.map((item: BasExternalDisputeResolutionSummary) => (
          <p className="break-all text-[9px] text-muted-foreground" key={refKey(item.resolution)}>
            {item.resolution?.id} · {item.outcome} · {dateTimeLabel(item.resolvedAt)}
          </p>
        ))}
      </section>
    </div>
  );
}
function DateInput({
  label,
  value,
  setValue,
}: {
  readonly label: string;
  readonly value: string;
  readonly setValue: (value: string) => void;
}) {
  return (
    <label className="grid gap-1 text-[9px]">
      {label}
      <input
        className="h-8 rounded border border-border bg-background px-2"
        onChange={(event) => setValue(event.target.value)}
        type="date"
        value={value}
      />
    </label>
  );
}
function TextInput({
  label,
  value,
  setValue,
  inputMode,
  maxLength,
}: {
  readonly label: string;
  readonly value: string;
  readonly setValue: (value: string) => void;
  readonly inputMode?: "numeric" | "text";
  readonly maxLength?: number;
}) {
  return (
    <label className="grid gap-1 text-[9px]">
      {label}
      <input
        className="h-8 min-w-0 rounded border border-border bg-background px-2 text-[10px]"
        inputMode={inputMode ?? "text"}
        maxLength={maxLength}
        onChange={(event) => setValue(event.target.value)}
        value={value}
      />
    </label>
  );
}
function EnumField<E extends number>({
  label,
  value,
  setValue,
  options,
}: {
  readonly label: string;
  readonly value: E;
  readonly setValue: (value: E) => void;
  readonly options: readonly { readonly value: number; readonly label: string }[];
}) {
  return (
    <label className="grid gap-1 text-[9px]">
      {label}
      <select
        className="h-8 min-w-0 rounded border border-border bg-background px-2 text-[10px]"
        onChange={(event) => setValue(Number(event.target.value) as E)}
        value={value}
      >
        {options.map((option) => (
          <option key={`${option.value}:${option.label}`} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  );
}
function EvidenceEditor({
  value,
  setValue,
  label,
}: {
  readonly value: EvidenceFields;
  readonly setValue: (value: EvidenceFields) => void;
  readonly label: string;
}) {
  return (
    <fieldset className="grid gap-2 rounded border border-border p-2">
      <legend className="px-1 text-[9px] font-semibold">{label}</legend>
      <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
        <TextInput
          label="Source type"
          value={value.type}
          setValue={(type) => setValue({ ...value, type })}
        />
        <TextInput
          label="Source UUID"
          value={value.id}
          setValue={(id) => setValue({ ...value, id })}
        />
        <TextInput
          label="Revision"
          value={value.revision}
          setValue={(revision) => setValue({ ...value, revision })}
        />
        <TextInput
          label="SHA-256 hex"
          value={value.hash}
          setValue={(hash) => setValue({ ...value, hash })}
        />
      </div>
      <p className="text-[8px] leading-4 text-muted-foreground">
        Paste an exact identity already retained in Tammy. Do not enter a local file path or SBR
        credential.
      </p>
    </fieldset>
  );
}
function enumOptions<E extends Record<string, string | number>>(
  enumObject: E,
  names: readonly string[],
) {
  return names
    .map((name) => ({
      value: enumObject[name] as number,
      label: name.replaceAll("_", " ").toLowerCase(),
    }))
    .filter((option) => typeof option.value === "number");
}
const applicabilityOptions = enumOptions(BasApplicability, ["UNKNOWN", "ACTIVE", "NOT_APPLICABLE"]);
const amountOptions = enumOptions(BasAmountState, [
  "UNSET",
  "UNKNOWN",
  "NOT_APPLICABLE",
  "KNOWN_ZERO",
  "KNOWN",
]);
function todayDate() {
  return new Date().toISOString().slice(0, 10);
}
function suggestionKey(item: BasPeriodSuggestion) {
  return `suggestion:${iso(item.period?.startDate)}:${iso(item.period?.endDate)}:${item.window}`;
}
function samePeriodRangeAndWindow(left: BasPeriodSummary, right: BasPeriodSuggestion) {
  return (
    left.window === right.window &&
    Boolean(
      left.dates?.startDate &&
        right.period?.startDate &&
        JSON.stringify(left.dates.startDate) === JSON.stringify(right.period.startDate) &&
        JSON.stringify(left.dates.endDate) === JSON.stringify(right.period.endDate),
    )
  );
}
function exportTargetKey(target: BasExportTarget | undefined) {
  const exact = target?.exactTarget;
  return exact?.case === "calculation"
    ? `calculation:${refKey(exact.value)}`
    : exact?.case === "frozenBas"
      ? `frozen_bas:${refKey(exact.value)}`
      : "";
}
function sameBytes(left: Uint8Array | undefined, right: Uint8Array | undefined) {
  if (!left || !right || left.byteLength !== right.byteLength) return false;
  return left.every((value, index) => value === right[index]);
}
function parseDecimal(value: string) {
  const match = /^(0|[1-9][0-9]{0,15})(?:\.([0-9]{1,8}))?$/.exec(value);
  if (!match) throw new Error("Enter an exact nonnegative decimal rate.");
  const fraction = match[2] ?? "";
  return { coefficient: BigInt(`${match[1]}${fraction}`), scale: fraction.length };
}
async function canonicalFindingHash(finding: BasValidationFinding) {
  const bytes = toBinary(BasValidationFindingSchema, finding, { writeUnknownFields: false });
  return new Uint8Array(await crypto.subtle.digest("SHA-256", bytes));
}
function preparationStateLabel(state: BasPreparationState | undefined) {
  return state === BasPreparationState.COLLECTING
    ? "Collecting"
    : state === BasPreparationState.BLOCKED
      ? "Blocked"
      : state === BasPreparationState.REVIEW_READY
        ? "Review ready"
        : state === BasPreparationState.FROZEN_FOR_MANUAL_HANDOFF
          ? "Frozen for manual handoff"
          : state === BasPreparationState.EXTERNAL_CONFIRMATION_RECORDED
            ? "External confirmation recorded"
            : state === BasPreparationState.SUPERSEDED_BEFORE_CONFIRMATION
              ? "Superseded before confirmation"
              : state === BasPreparationState.SUPERSEDED_AFTER_RECORDED_REJECTION
                ? "Superseded after recorded rejection"
                : state === BasPreparationState.SUPERSEDED_BY_CONFIRMED_REVISION
                  ? "Superseded by confirmed revision"
                  : "Unknown";
}
function disputeOverlayLabel(state: BasDisputeOverlayState | undefined) {
  return state === BasDisputeOverlayState.NONE
    ? "No active dispute"
    : state === BasDisputeOverlayState.DISPUTED
      ? "Dispute requires review"
      : state === BasDisputeOverlayState.CORRECTION_REQUIRED
        ? "Corrective revision required"
        : state === BasDisputeOverlayState.RESOLVED_UPHELD
          ? "Dispute resolved; recorded chain upheld"
          : "Unknown";
}
function amountStateLabel(state: BasAmountState) {
  return state === BasAmountState.KNOWN
    ? "Known"
    : state === BasAmountState.KNOWN_ZERO
      ? "Known zero"
      : state === BasAmountState.NOT_APPLICABLE
        ? "Not applicable"
        : state === BasAmountState.UNKNOWN
          ? "Unknown"
          : "Unset";
}
function formLabel(value: number) {
  const labels = [
    "",
    "G1",
    "1A",
    "1B",
    "W1",
    "W2",
    "W3",
    "W4",
    "W5",
    "4",
    "T1",
    "T2",
    "T7",
    "T11",
    "5A",
    "5B",
    "8A",
    "8B",
    "9",
  ];
  return labels[value] ?? `Label ${value}`;
}
function eventLabel(kind: BasExternalEventKind) {
  return kind === BasExternalEventKind.COMPLETED_LODGEMENT
    ? "completed lodgement"
    : kind === BasExternalEventKind.ACCEPTANCE
      ? "acceptance"
      : kind === BasExternalEventKind.PENDING
        ? "pending"
        : kind === BasExternalEventKind.REJECTION
          ? "rejection"
          : kind === BasExternalEventKind.REVIEWER_HANDOFF
            ? "reviewer handoff"
            : kind === BasExternalEventKind.PAYMENT
              ? "payment"
              : kind === BasExternalEventKind.REFUND
                ? "refund"
                : "unknown event";
}
function dateTimeLabel(value: { readonly seconds: bigint; readonly nanos: number } | undefined) {
  return value
    ? new Date(
        Number(value.seconds * 1000n + BigInt(Math.floor(value.nanos / 1_000_000))),
      ).toLocaleString()
    : "—";
}
function timestampIso(value: { readonly seconds: bigint; readonly nanos: number } | undefined) {
  return value
    ? new Date(
        Number(value.seconds * 1000n + BigInt(Math.floor(value.nanos / 1_000_000))),
      ).toISOString()
    : undefined;
}
function exportFormatLabel(format: BasExportFormat) {
  return format === BasExportFormat.PDF
    ? "PDF"
    : format === BasExportFormat.CSV
      ? "CSV"
      : "Unknown format";
}
function exportEventStatus(phase: BasExportPhase, outcome: BasExportOutcome | undefined) {
  if (phase === BasExportPhase.STARTED) return "Started";
  if (outcome === BasExportOutcome.SUCCEEDED) return "Succeeded";
  if (outcome === BasExportOutcome.CANCELLED) return "Cancelled";
  if (outcome === BasExportOutcome.FAILED) return "Failed";
  return "Finished with unknown result";
}
function currentnessLabel(value: BasLiveCurrentness | undefined) {
  return value === BasLiveCurrentness.CURRENT
    ? "Current at this read"
    : value === BasLiveCurrentness.SOURCE_CHANGED
      ? "Source changed since this snapshot"
      : value === BasLiveCurrentness.UNAVAILABLE
        ? "Source currentness unavailable"
        : value === BasLiveCurrentness.NOT_ASSESSED
          ? "Not assessed"
          : "Unknown";
}
