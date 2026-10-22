import { create, type DescMessage } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import {
  BasApplicability,
  BasCalculationSchema,
  BasChainExpectationSchema,
  BasCivilDateRangeSchema,
  BasConfirmedPeriodSchema,
  BasDisputeOverlayState,
  BasExpectedIdentitySchema,
  BasExportEventSummarySchema,
  BasExportFormat,
  BasExportOutcome,
  BasExportPhase,
  BasExportTargetSchema,
  BasExternalDisputeSummarySchema,
  BasExternalEventKind,
  BasFindingSeverity,
  BasFrozenBasSummarySchema,
  BasLiveCurrentness,
  BasObligationsProfileSchema,
  BasObligationsProfileSummarySchema,
  BasPaygiMethod,
  BasPeriodSuggestionSchema,
  BasPeriodSummarySchema,
  BasPeriodWindow,
  BasPreparationSchema,
  BasPreparationState,
  BasPreparationSummarySchema,
  BasValidationFindingSchema,
  BasValidationSnapshotSchema,
  ConfirmBasPeriodRequestSchema,
  ConfirmBasPeriodResponseSchema,
  GetBasCalendarRequestSchema,
  GetBasCalendarResponseSchema,
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
  RegenerateBasPreparationRequestSchema,
  RegenerateBasPreparationResponseSchema,
  ResolveExternalBasDisputeRequestSchema,
  ResolveExternalBasDisputeResponseSchema,
  SetBasObligationsProfileRequestSchema,
  SetBasObligationsProfileResponseSchema,
} from "@tammy/connect-client/tammy/v1/bas_preparation_pb.js";
import {
  AuthenticationContextSchema,
  FreshFactorContextSchema,
  PageInfoSchema,
  SourceRefSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  AssertTOTPRequestSchema,
  AssertTOTPResponseSchema,
  GetCurrentUserRequestSchema,
  GetCurrentUserResponseSchema,
  Role,
  UserSchema,
  UserState,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";
import { BAS_PREPARATION_RESPONSE_FRAME_LIMIT } from "../../../shared/bas-preparation-ipc";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { createConveyorFixture, renderWithConveyor } from "../../test/conveyor-fixtures";
import { BasPreparationLifecycle, readAllPages } from "./bas-lifecycle";

function responseCodec<Input extends DescMessage, Output extends DescMessage>(
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
const codecs = {
  user: responseCodec(GetCurrentUserRequestSchema, GetCurrentUserResponseSchema),
  preparationDetail: responseCodec(GetBasPreparationRequestSchema, GetBasPreparationResponseSchema),
  assertTotp: responseCodec(AssertTOTPRequestSchema, AssertTOTPResponseSchema),
  resolveDispute: responseCodec(
    ResolveExternalBasDisputeRequestSchema,
    ResolveExternalBasDisputeResponseSchema,
  ),
  frozenDetail: responseCodec(GetFrozenBasRequestSchema, GetFrozenBasResponseSchema),
  exportEvents: responseCodec(ListBasExportEventsRequestSchema, ListBasExportEventsResponseSchema),
  confirmPeriod: responseCodec(ConfirmBasPeriodRequestSchema, ConfirmBasPeriodResponseSchema),
  regenerate: responseCodec(
    RegenerateBasPreparationRequestSchema,
    RegenerateBasPreparationResponseSchema,
  ),
  profiles: responseCodec(
    ListBasObligationsProfilesRequestSchema,
    ListBasObligationsProfilesResponseSchema,
  ),
  periods: responseCodec(ListBasPeriodsRequestSchema, ListBasPeriodsResponseSchema),
  preparations: responseCodec(ListBasPreparationsRequestSchema, ListBasPreparationsResponseSchema),
  frozen: responseCodec(ListFrozenBasRequestSchema, ListFrozenBasResponseSchema),
  disputes: responseCodec(
    ListBasExternalDisputeResolutionsRequestSchema,
    ListBasExternalDisputeResolutionsResponseSchema,
  ),
  observations: responseCodec(
    ListBasExternalObservationsRequestSchema,
    ListBasExternalObservationsResponseSchema,
  ),
  calendar: responseCodec(GetBasCalendarRequestSchema, GetBasCalendarResponseSchema),
  setProfile: responseCodec(
    SetBasObligationsProfileRequestSchema,
    SetBasObligationsProfileResponseSchema,
  ),
};

it("continues paging when pages are empty but still carry a cursor", async () => {
  const pages = [
    { values: [] as string[], nextCursor: "cursor-2" },
    { values: [] as string[], nextCursor: "cursor-3" },
    { values: ["exact-target"], nextCursor: undefined },
  ];
  const calls: string[] = [];
  const result = await readAllPages({
    encode: (cursor) => new TextEncoder().encode(cursor ?? "first"),
    call: async (frame) => {
      calls.push(new TextDecoder().decode(frame));
      const page = pages.shift();
      if (!page) throw new Error("unexpected page read");
      return new TextEncoder().encode(JSON.stringify(page));
    },
    decode: (frame) =>
      JSON.parse(new TextDecoder().decode(frame)) as { values: string[]; nextCursor?: string },
  });

  expect(result).toEqual(["exact-target"]);
  expect(calls).toEqual(["first", "cursor-2", "cursor-3"]);
});

it("mounts all five lifecycle stages and keeps workspace admins read-only for approval", async () => {
  const fixture = createConveyorFixture();
  const workspace = {
    workspaceId: "018f0000-0000-7000-8000-000000000001",
    userId: "018f0000-0000-7000-8000-000000000002",
    sessionId: "018f0000-0000-7000-8000-000000000003",
    organisationId: "018f0000-0000-7000-8000-000000000004",
    roles: [Role.WORKSPACE_ADMIN],
  };
  fixture.procedures.organisation.getCurrentUser.mockResolvedValue(
    codecs.user.encodeResponse(
      create(GetCurrentUserResponseSchema, {
        user: create(UserSchema, {
          id: workspace.userId,
          version: 4n,
          username: "operator",
          displayName: "Operator",
          state: UserState.ACTIVE,
          roles: [Role.WORKSPACE_ADMIN],
        }),
      }),
    ),
  );
  fixture.procedures.basPreparation.listBasObligationsProfiles.mockResolvedValue(
    codecs.profiles.encodeResponse(create(ListBasObligationsProfilesResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasPeriods.mockResolvedValue(
    codecs.periods.encodeResponse(create(ListBasPeriodsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasPreparations.mockResolvedValue(
    codecs.preparations.encodeResponse(create(ListBasPreparationsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listFrozenBas.mockResolvedValue(
    codecs.frozen.encodeResponse(create(ListFrozenBasResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasExternalDisputeResolutions.mockResolvedValue(
    codecs.disputes.encodeResponse(create(ListBasExternalDisputeResolutionsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.getBasCalendar.mockResolvedValue(
    codecs.calendar.encodeResponse(create(GetBasCalendarResponseSchema, {})),
  );

  renderWithConveyor(
    <BasPreparationLifecycle
      api={fixture.conveyor.basPreparation}
      identityApi={fixture.conveyor.organisation}
      reportApi={fixture.conveyor.reporting}
      workspace={workspace}
    />,
  );

  expect(await screen.findByRole("heading", { name: "BAS preparation" })).toBeTruthy();
  expect(await screen.findByText("Workspace admin · prepare and read only")).toBeTruthy();
  const user = userEvent.setup();
  for (const [stage, heading] of [
    ["Review", "Exact review snapshot"],
    ["Freeze", "Freeze for manual handoff"],
    ["Export", "Export an exact retained BAS version"],
    ["External record", "Record what the user says happened outside Tammy"],
    ["Prepare", "Obligations profile"],
  ] as const) {
    await user.click(screen.getByRole("button", { name: stage }));
    expect(await screen.findByRole("heading", { name: heading })).toBeTruthy();
  }
  await user.click(screen.getByRole("button", { name: "External record" }));
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "Record correction (retains prior event)" }),
    ).toHaveProperty("disabled", true),
  );
  expect(
    screen.getByRole("button", { name: "Resolve exact contested history (fresh TOTP)" }),
  ).toHaveProperty("disabled", true);
  await user.click(screen.getByRole("button", { name: "Freeze" }));
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "Approve and freeze exact snapshot" }),
    ).toHaveProperty("disabled", true),
  );
  expect(fixture.procedures.basPreparation.freezeBasForManualHandoff).not.toHaveBeenCalled();
});

it("keeps profile setup usable before a calendar can resolve an effective profile", async () => {
  const fixture = createConveyorFixture();
  const workspace = {
    workspaceId: "018f0000-0000-7000-8000-000000000021",
    userId: "018f0000-0000-7000-8000-000000000022",
    sessionId: "018f0000-0000-7000-8000-000000000023",
    organisationId: "018f0000-0000-7000-8000-000000000024",
    roles: [Role.WORKSPACE_ADMIN],
  };
  fixture.procedures.organisation.getCurrentUser.mockResolvedValue(
    codecs.user.encodeResponse(
      create(GetCurrentUserResponseSchema, {
        user: create(UserSchema, {
          id: workspace.userId,
          version: 1n,
          username: "fresh-workspace-admin",
          displayName: "Fresh workspace admin",
          state: UserState.ACTIVE,
          roles: [Role.WORKSPACE_ADMIN],
        }),
      }),
    ),
  );
  fixture.procedures.basPreparation.listBasObligationsProfiles.mockResolvedValue(
    codecs.profiles.encodeResponse(create(ListBasObligationsProfilesResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasPeriods.mockResolvedValue(
    codecs.periods.encodeResponse(create(ListBasPeriodsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasPreparations.mockResolvedValue(
    codecs.preparations.encodeResponse(create(ListBasPreparationsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listFrozenBas.mockResolvedValue(
    codecs.frozen.encodeResponse(create(ListFrozenBasResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasExternalDisputeResolutions.mockResolvedValue(
    codecs.disputes.encodeResponse(create(ListBasExternalDisputeResolutionsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.getBasCalendar.mockRejectedValue(
    new Error("NOT_FOUND without an effective obligations profile"),
  );

  renderWithConveyor(
    <BasPreparationLifecycle
      api={fixture.conveyor.basPreparation}
      identityApi={fixture.conveyor.organisation}
      reportApi={fixture.conveyor.reporting}
      workspace={workspace}
    />,
  );

  const user = userEvent.setup();
  await user.type(
    await screen.findByLabelText("Profile decision reason"),
    "Fresh profile setup retains explicit business answers.",
  );
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Save immutable profile revision" })).toHaveProperty(
      "disabled",
      false,
    ),
  );
  expect(screen.queryByText("BAS history could not be loaded. Refresh and try again.")).toBeNull();
  expect(fixture.procedures.basPreparation.getBasCalendar).not.toHaveBeenCalled();
});

it("saves explicit registration dates and evidence-backed decided obligations", async () => {
  const fixture = createConveyorFixture();
  const workspace = {
    workspaceId: "018f0000-0000-7000-8000-000000000081",
    userId: "018f0000-0000-7000-8000-000000000082",
    sessionId: "018f0000-0000-7000-8000-000000000083",
    organisationId: "018f0000-0000-7000-8000-000000000084",
    roles: [Role.BUSINESS_PREPARER],
  };
  const evidenceId = "018f0000-0000-7000-8000-000000000085";
  const profileId = "018f0000-0000-7000-8000-000000000086";
  const savedHash = new Uint8Array(32).fill(0x86);
  fixture.procedures.organisation.getCurrentUser.mockResolvedValue(
    codecs.user.encodeResponse(
      create(GetCurrentUserResponseSchema, {
        user: create(UserSchema, {
          id: workspace.userId,
          version: 1n,
          username: "profile-preparer",
          displayName: "Profile preparer",
          state: UserState.ACTIVE,
          roles: [Role.BUSINESS_PREPARER],
        }),
      }),
    ),
  );
  fixture.procedures.basPreparation.listBasObligationsProfiles.mockResolvedValue(
    codecs.profiles.encodeResponse(create(ListBasObligationsProfilesResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasPeriods.mockResolvedValue(
    codecs.periods.encodeResponse(create(ListBasPeriodsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasPreparations.mockResolvedValue(
    codecs.preparations.encodeResponse(create(ListBasPreparationsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listFrozenBas.mockResolvedValue(
    codecs.frozen.encodeResponse(create(ListFrozenBasResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasExternalDisputeResolutions.mockResolvedValue(
    codecs.disputes.encodeResponse(create(ListBasExternalDisputeResolutionsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.setBasObligationsProfile.mockImplementation(async (frame) => {
    const request = codecs.setProfile.decodeRequest(frame);
    return codecs.setProfile.encodeResponse(
      create(SetBasObligationsProfileResponseSchema, {
        profile: create(BasObligationsProfileSchema, {
          profileId,
          organisationId: workspace.organisationId,
          revision: 1n,
          contentHash: savedHash,
          profile: request.profile,
          actorUserId: workspace.userId,
        }),
      }),
    );
  });

  renderWithConveyor(
    <BasPreparationLifecycle
      api={fixture.conveyor.basPreparation}
      identityApi={fixture.conveyor.organisation}
      reportApi={fixture.conveyor.reporting}
      workspace={workspace}
    />,
  );
  const user = userEvent.setup();
  await user.selectOptions(await screen.findByLabelText("GST obligation"), "2");
  await user.selectOptions(screen.getByLabelText("GST registration status"), "2");
  await user.selectOptions(screen.getByLabelText("PAYGW obligation"), "2");
  await user.selectOptions(screen.getByLabelText("PAYGI obligation"), "2");
  await user.selectOptions(screen.getByLabelText("PAYGI method"), String(BasPaygiMethod.RATE));
  await user.selectOptions(screen.getByLabelText("Fringe benefits tax"), "3");
  await user.selectOptions(screen.getByLabelText("Wine equalisation tax"), "2");
  await user.type(
    screen.getByLabelText("GST decision reason"),
    "Registration and quarterly GST reviewed.",
  );
  await user.type(screen.getByLabelText("PAYGW reason"), "Quarterly payroll confirmed.");
  await user.type(screen.getByLabelText("PAYGI reason"), "Quarterly instalment notice confirmed.");
  await user.type(
    screen.getByLabelText("Profile decision reason"),
    "Reviewed all listed obligations.",
  );
  const save = screen.getByRole("button", { name: "Save immutable profile revision" });
  expect(save).toHaveProperty("disabled", true);
  expect(screen.getByText(/Enter the retained GST registration start date/)).toBeTruthy();
  expect(screen.getByText(/Enter a complete retained source identity/)).toBeTruthy();
  expect(screen.getByText(/Enter a canonical income-year UUID/)).toBeTruthy();

  fireEvent.change(screen.getByLabelText("Effective from"), { target: { value: "2026-07-01" } });
  fireEvent.change(screen.getByLabelText("GST registration effective from"), {
    target: { value: "2025-07-01" },
  });
  await user.type(
    screen.getByLabelText("Confirmed income year UUID"),
    "018f0000-0000-7000-8000-000000000087",
  );
  fireEvent.change(screen.getByLabelText("Income year start"), {
    target: { value: "2026-07-01" },
  });
  fireEvent.change(screen.getByLabelText("Income year end"), {
    target: { value: "2027-06-30" },
  });
  const sharedEvidence = within(
    screen.getByRole("group", { name: "Shared retained profile evidence identity" }),
  );
  await user.type(sharedEvidence.getByLabelText("Source type"), "document");
  await user.type(sharedEvidence.getByLabelText("Source UUID"), evidenceId);
  await user.type(sharedEvidence.getByLabelText("Revision"), "1");
  await user.type(sharedEvidence.getByLabelText("SHA-256 hex"), "85".repeat(32));
  await waitFor(() => expect(save).toHaveProperty("disabled", false));
  await user.selectOptions(screen.getByLabelText("PAYGI obligation"), "3");
  await waitFor(() => expect(save).toHaveProperty("disabled", true));
  expect(screen.getByText(/Not-applicable PAYGI must use the not-applicable method/)).toBeTruthy();
  await user.selectOptions(screen.getByLabelText("PAYGI obligation"), "1");
  await waitFor(() => expect(save).toHaveProperty("disabled", false));

  fireEvent.change(screen.getByLabelText("Effective from"), { target: { value: "" } });
  await waitFor(() => expect(save).toHaveProperty("disabled", true));
  expect(screen.getByText(/enter the profile effective start date/i)).toBeTruthy();
  fireEvent.change(screen.getByLabelText("Effective from"), {
    target: { value: "2026-07-01" },
  });
  await waitFor(() => expect(save).toHaveProperty("disabled", false));

  fireEvent.change(screen.getByLabelText("Effective through (optional)"), {
    target: { value: "2026-06-30" },
  });
  await waitFor(() => expect(save).toHaveProperty("disabled", true));
  expect(screen.getByText(/profile end date must be on or after its start date/i)).toBeTruthy();
  fireEvent.change(screen.getByLabelText("Effective through (optional)"), {
    target: { value: "" },
  });
  await waitFor(() => expect(save).toHaveProperty("disabled", false));

  fireEvent.change(screen.getByLabelText("GST registration effective through (optional)"), {
    target: { value: "2025-06-30" },
  });
  await waitFor(() => expect(save).toHaveProperty("disabled", true));
  expect(
    screen.getByText(/GST registration end date must be on or after its start date/i),
  ).toBeTruthy();
  fireEvent.change(screen.getByLabelText("GST registration effective through (optional)"), {
    target: { value: "" },
  });
  await waitFor(() => expect(save).toHaveProperty("disabled", false));

  fireEvent.change(screen.getByLabelText("Income year end"), {
    target: { value: "2026-06-30" },
  });
  await waitFor(() => expect(save).toHaveProperty("disabled", true));
  expect(screen.getByText(/income year end date must be on or after its start date/i)).toBeTruthy();
  fireEvent.change(screen.getByLabelText("Income year end"), {
    target: { value: "2027-06-30" },
  });
  await waitFor(() => expect(save).toHaveProperty("disabled", false));

  await user.click(save);

  await waitFor(() =>
    expect(fixture.procedures.basPreparation.setBasObligationsProfile).toHaveBeenCalledTimes(1),
  );
  const sentFrame = fixture.procedures.basPreparation.setBasObligationsProfile.mock.calls[0]?.[0];
  if (!(sentFrame instanceof Uint8Array)) throw new Error("profile request frame missing");
  const submitted = codecs.setProfile.decodeRequest(sentFrame).profile;
  expect(submitted?.effectiveInterval?.startDate).toMatchObject({ year: 2026, month: 7, day: 1 });
  expect(submitted?.gst?.registrationInterval?.startDate).toMatchObject({
    year: 2025,
    month: 7,
    day: 1,
  });
  expect(submitted?.gst?.evidence[0]).toMatchObject({ type: "document", id: evidenceId });
  expect(submitted?.paygw?.applicability?.applicability).toBe(BasApplicability.ACTIVE);
  expect(submitted?.paygw?.applicability?.reason).toBe("Quarterly payroll confirmed.");
  expect(submitted?.paygw?.applicability?.evidence[0]?.id).toBe(evidenceId);
  expect(submitted?.paygi?.applicability?.applicability).toBe(BasApplicability.UNKNOWN);
  expect(submitted?.paygi?.applicability?.reason).toBe("");
  expect(submitted?.paygi?.applicability?.evidence).toHaveLength(0);
  expect(submitted?.paygi?.confirmedIncomeYear).toMatchObject({
    incomeYearId: "018f0000-0000-7000-8000-000000000087",
    dates: {
      startDate: { year: 2026, month: 7, day: 1 },
      endDate: { year: 2027, month: 6, day: 30 },
    },
  });
  expect(submitted?.paygi?.confirmedIncomeYear?.confirmation).toMatchObject({
    type: "document",
    id: evidenceId,
    revision: 1n,
  });
  expect(
    Array.from(submitted?.paygi?.confirmedIncomeYear?.confirmation?.contentHash ?? []),
  ).toEqual(Array(32).fill(0x85));
  expect(submitted?.otherObligations?.fringeBenefitsTax?.applicability).toBe(
    BasApplicability.NOT_APPLICABLE,
  );
  expect(submitted?.otherObligations?.fringeBenefitsTax?.reason).toBe(
    "Reviewed all listed obligations.",
  );
  expect(submitted?.otherObligations?.fringeBenefitsTax?.evidence[0]?.id).toBe(evidenceId);
  expect(submitted?.otherObligations?.wineEqualisationTax?.applicability).toBe(
    BasApplicability.ACTIVE,
  );
  expect(submitted?.otherObligations?.wineEqualisationTax?.reason).toBe(
    "Reviewed all listed obligations.",
  );
  expect(submitted?.otherObligations?.wineEqualisationTax?.evidence[0]?.id).toBe(evidenceId);
  expect(submitted?.otherObligations?.luxuryCarTax?.applicability).toBe(BasApplicability.UNKNOWN);
  expect(submitted?.otherObligations?.luxuryCarTax?.reason).toBe("");
  expect(submitted?.otherObligations?.luxuryCarTax?.evidence).toHaveLength(0);
});

it("exports the selected frozen handoff by its exact immutable identity", async () => {
  const fixture = createConveyorFixture();
  const workspace = {
    workspaceId: "018f0000-0000-7000-8000-000000000011",
    userId: "018f0000-0000-7000-8000-000000000012",
    sessionId: "018f0000-0000-7000-8000-000000000013",
    organisationId: "018f0000-0000-7000-8000-000000000014",
  };
  const frozenId = "018f0000-0000-7000-8000-000000000015";
  const frozenHash = new Uint8Array(32).fill(0xab);
  const frozenRef = create(SourceRefSchema, {
    type: "frozen_bas",
    id: frozenId,
    revision: 1n,
    contentHash: frozenHash,
  });
  fixture.procedures.organisation.getCurrentUser.mockResolvedValue(
    codecs.user.encodeResponse(
      create(GetCurrentUserResponseSchema, {
        user: create(UserSchema, {
          id: workspace.userId,
          version: 2n,
          username: "preparer",
          displayName: "Preparer",
          state: UserState.ACTIVE,
          roles: [Role.BUSINESS_PREPARER],
        }),
      }),
    ),
  );
  fixture.procedures.basPreparation.listBasObligationsProfiles.mockResolvedValue(
    codecs.profiles.encodeResponse(create(ListBasObligationsProfilesResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasPeriods.mockResolvedValue(
    codecs.periods.encodeResponse(create(ListBasPeriodsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasPreparations.mockResolvedValue(
    codecs.preparations.encodeResponse(create(ListBasPreparationsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listFrozenBas.mockResolvedValue(
    codecs.frozen.encodeResponse(
      create(ListFrozenBasResponseSchema, {
        frozenBas: [create(BasFrozenBasSummarySchema, { frozenBas: frozenRef })],
      }),
    ),
  );
  fixture.procedures.basPreparation.listBasExternalDisputeResolutions.mockResolvedValue(
    codecs.disputes.encodeResponse(create(ListBasExternalDisputeResolutionsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.getBasCalendar.mockResolvedValue(
    codecs.calendar.encodeResponse(create(GetBasCalendarResponseSchema, {})),
  );
  fixture.procedures.basPreparation.getFrozenBas.mockResolvedValue(
    codecs.frozenDetail.encodeResponse(create(GetFrozenBasResponseSchema, {})),
  );
  const eventId = "018f0000-0000-7000-8000-000000000016";
  const eventRef = create(SourceRefSchema, {
    type: "bas_export_event",
    id: eventId,
    revision: 1n,
    contentHash: new Uint8Array(32).fill(0x9a),
  });
  const exportRequests: ReturnType<typeof codecs.exportEvents.decodeRequest>[] = [];
  let historyPage = 0;
  fixture.procedures.basPreparation.listBasExportEvents.mockImplementation(async (frame) => {
    exportRequests.push(codecs.exportEvents.decodeRequest(frame));
    historyPage += 1;
    return codecs.exportEvents.encodeResponse(
      historyPage === 1
        ? create(ListBasExportEventsResponseSchema, {
            events: [],
            page: create(PageInfoSchema, { returnedCount: 0, nextCursor: "next" }),
          })
        : create(ListBasExportEventsResponseSchema, {
            events: [
              create(BasExportEventSummarySchema, {
                exportEvent: eventRef,
                target: create(BasExportTargetSchema, {
                  exactTarget: { case: "frozenBas", value: frozenRef },
                }),
                format: BasExportFormat.PDF,
                phase: BasExportPhase.FINISHED,
                outcome: BasExportOutcome.SUCCEEDED,
                recordedAt: { seconds: 1_798_000_000n, nanos: 0 },
              }),
            ],
            page: create(PageInfoSchema, { returnedCount: 1 }),
          }),
    );
  });
  fixture.procedures.reporting.exportBasPreparation.mockResolvedValue({ saved: true });

  renderWithConveyor(
    <BasPreparationLifecycle
      api={fixture.conveyor.basPreparation}
      identityApi={fixture.conveyor.organisation}
      reportApi={fixture.conveyor.reporting}
      workspace={workspace}
    />,
  );

  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Export" }));
  const historyEntry = await screen.findByRole("listitem");
  expect(historyEntry.textContent).toContain(`PDF · Succeeded · event ${eventId}`);
  await waitFor(() => expect(exportRequests).toHaveLength(2));
  expect(exportRequests[0]?.target?.exactTarget.case).toBe("frozenBas");
  if (exportRequests[0]?.target?.exactTarget.case === "frozenBas")
    expect(exportRequests[0].target.exactTarget.value.id).toBe(frozenId);
  expect(exportRequests[1]?.page?.cursor).toBe("next");
  await user.click(screen.getByRole("button", { name: "Export PDF" }));
  await waitFor(() =>
    expect(fixture.procedures.reporting.exportBasPreparation).toHaveBeenCalledWith({
      authentication: create(AuthenticationContextSchema, {
        actorUserId: workspace.userId,
        sessionId: workspace.sessionId,
      }),
      organisationId: workspace.organisationId,
      target: { kind: "frozen_bas", id: frozenId, revision: "1", hash: "ab".repeat(32) },
      format: "pdf",
    }),
  );
});

it("does not treat a different BAS window with identical dates as an existing period", async () => {
  const fixture = createConveyorFixture();
  const workspace = {
    workspaceId: "018f0000-0000-7000-8000-000000000101",
    userId: "018f0000-0000-7000-8000-000000000102",
    sessionId: "018f0000-0000-7000-8000-000000000103",
    organisationId: "018f0000-0000-7000-8000-000000000104",
  };
  const profileRef = create(SourceRefSchema, {
    type: "bas_obligations_profile",
    id: "018f0000-0000-7000-8000-000000000105",
    revision: 1n,
    contentHash: new Uint8Array(32).fill(0x15),
  });
  const periodRef = create(SourceRefSchema, {
    type: "bas_period",
    id: "018f0000-0000-7000-8000-000000000106",
    revision: 4n,
    contentHash: new Uint8Array(32).fill(0x16),
  });
  const dates = create(BasCivilDateRangeSchema, {
    startDate: { year: 2026, month: 7, day: 1 },
    endDate: { year: 2026, month: 9, day: 30 },
  });
  fixture.procedures.organisation.getCurrentUser.mockResolvedValue(
    codecs.user.encodeResponse(
      create(GetCurrentUserResponseSchema, {
        user: create(UserSchema, {
          id: workspace.userId,
          version: 3n,
          username: "preparer",
          displayName: "Preparer",
          state: UserState.ACTIVE,
          roles: [Role.BUSINESS_PREPARER],
        }),
      }),
    ),
  );
  fixture.procedures.basPreparation.listBasObligationsProfiles.mockResolvedValue(
    codecs.profiles.encodeResponse(
      create(ListBasObligationsProfilesResponseSchema, {
        profiles: [create(BasObligationsProfileSummarySchema, { profile: profileRef })],
      }),
    ),
  );
  fixture.procedures.basPreparation.listBasPeriods.mockResolvedValue(
    codecs.periods.encodeResponse(
      create(ListBasPeriodsResponseSchema, {
        periods: [
          create(BasPeriodSummarySchema, {
            period: periodRef,
            dates,
            window: BasPeriodWindow.JUL_SEP,
            profile: profileRef,
          }),
        ],
      }),
    ),
  );
  fixture.procedures.basPreparation.listBasPreparations.mockResolvedValue(
    codecs.preparations.encodeResponse(create(ListBasPreparationsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listFrozenBas.mockResolvedValue(
    codecs.frozen.encodeResponse(create(ListFrozenBasResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasExternalDisputeResolutions.mockResolvedValue(
    codecs.disputes.encodeResponse(create(ListBasExternalDisputeResolutionsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.getBasCalendar.mockResolvedValue(
    codecs.calendar.encodeResponse(
      create(GetBasCalendarResponseSchema, {
        suggestions: [
          create(BasPeriodSuggestionSchema, {
            period: dates,
            window: BasPeriodWindow.OCT_DEC,
            nominalDueDate: { year: 2026, month: 10, day: 28 },
            dueDateRuleVersion: "calendar-v1",
            dueDateRuleHash: new Uint8Array(32).fill(0x17),
            profile: profileRef,
          }),
        ],
      }),
    ),
  );
  let confirmed: ReturnType<typeof codecs.confirmPeriod.decodeRequest> | undefined;
  fixture.procedures.basPreparation.confirmBasPeriod.mockImplementation(async (frame) => {
    confirmed = codecs.confirmPeriod.decodeRequest(frame);
    return codecs.confirmPeriod.encodeResponse(
      create(ConfirmBasPeriodResponseSchema, {
        period: create(BasConfirmedPeriodSchema, {
          periodId: "018f0000-0000-7000-8000-000000000107",
          organisationId: workspace.organisationId,
          version: 1n,
          actorUserId: workspace.userId,
          contentHash: new Uint8Array(32).fill(0x18),
        }),
      }),
    );
  });

  renderWithConveyor(
    <BasPreparationLifecycle
      api={fixture.conveyor.basPreparation}
      identityApi={fixture.conveyor.organisation}
      reportApi={fixture.conveyor.reporting}
      workspace={workspace}
    />,
  );
  const user = userEvent.setup();
  const periodSelect = await screen.findByLabelText("Confirmed period history");
  const dueDateEvidence = within(
    screen.getByRole("group", { name: "Actual statement due-date evidence identity" }),
  );
  await user.type(dueDateEvidence.getByLabelText("Source type"), "ato_statement");
  await user.type(
    dueDateEvidence.getByLabelText("Source UUID"),
    "018f0000-0000-7000-8000-000000000118",
  );
  await user.type(dueDateEvidence.getByLabelText("Revision"), "2");
  await user.type(dueDateEvidence.getByLabelText("SHA-256 hex"), "18".repeat(32));
  await user.selectOptions(
    periodSelect,
    `suggestion:2026-07-01:2026-09-30:${BasPeriodWindow.OCT_DEC}`,
  );
  await user.type(
    screen.getByLabelText("Period confirmation reason"),
    "Confirm the October to December window",
  );
  await user.click(screen.getByRole("button", { name: "Confirm selected period" }));

  await waitFor(() => expect(confirmed).toBeDefined());
  expect(confirmed?.expectedVersion).toBe(0n);
  expect(confirmed?.period?.window).toBe(BasPeriodWindow.OCT_DEC);
  expect(confirmed?.period?.dueDateEvidence).toHaveLength(1);
  expect(confirmed?.period?.dueDateEvidence[0]).toMatchObject({
    type: "ato_statement",
    id: "018f0000-0000-7000-8000-000000000118",
    revision: 2n,
  });
  expect(Array.from(confirmed?.period?.dueDateEvidence[0]?.contentHash ?? [])).toEqual(
    Array(32).fill(0x18),
  );
  expect(confirmed?.period?.registrationEvidence).toHaveLength(0);
});

it("starts a corrective genesis from the exact retained family and explicit absent attempt", async () => {
  const fixture = createConveyorFixture();
  const workspace = {
    workspaceId: "018f0000-0000-7000-8000-000000000111",
    userId: "018f0000-0000-7000-8000-000000000112",
    sessionId: "018f0000-0000-7000-8000-000000000113",
    organisationId: "018f0000-0000-7000-8000-000000000114",
  };
  const profile = create(SourceRefSchema, {
    type: "bas_obligations_profile",
    id: "018f0000-0000-7000-8000-000000000115",
    revision: 2n,
    contentHash: new Uint8Array(32).fill(0x25),
  });
  const period = create(SourceRefSchema, {
    type: "bas_period",
    id: "018f0000-0000-7000-8000-000000000116",
    revision: 3n,
    contentHash: new Uint8Array(32).fill(0x26),
  });
  const family = create(SourceRefSchema, {
    type: "bas_family",
    id: "018f0000-0000-7000-8000-000000000117",
    revision: 1n,
    contentHash: new Uint8Array(32).fill(0x27),
  });
  const branch = create(SourceRefSchema, {
    type: "bas_branch",
    id: "018f0000-0000-7000-8000-000000000118",
    revision: 2n,
    contentHash: new Uint8Array(32).fill(0x28),
  });
  const preparation = create(SourceRefSchema, {
    type: "bas_preparation",
    id: "018f0000-0000-7000-8000-000000000119",
    revision: 3n,
    contentHash: new Uint8Array(32).fill(0x29),
  });
  fixture.procedures.organisation.getCurrentUser.mockResolvedValue(
    codecs.user.encodeResponse(
      create(GetCurrentUserResponseSchema, {
        user: create(UserSchema, {
          id: workspace.userId,
          version: 5n,
          username: "preparer",
          displayName: "Preparer",
          state: UserState.ACTIVE,
          roles: [Role.BUSINESS_PREPARER],
        }),
      }),
    ),
  );
  fixture.procedures.basPreparation.listBasObligationsProfiles.mockResolvedValue(
    codecs.profiles.encodeResponse(
      create(ListBasObligationsProfilesResponseSchema, {
        profiles: [create(BasObligationsProfileSummarySchema, { profile })],
      }),
    ),
  );
  fixture.procedures.basPreparation.listBasPeriods.mockResolvedValue(
    codecs.periods.encodeResponse(
      create(ListBasPeriodsResponseSchema, {
        periods: [
          create(BasPeriodSummarySchema, {
            period,
            dates: create(BasCivilDateRangeSchema, {
              startDate: { year: 2026, month: 7, day: 1 },
              endDate: { year: 2026, month: 9, day: 30 },
            }),
            window: BasPeriodWindow.JUL_SEP,
            profile,
          }),
        ],
      }),
    ),
  );
  fixture.procedures.basPreparation.listBasPreparations.mockResolvedValue(
    codecs.preparations.encodeResponse(
      create(ListBasPreparationsResponseSchema, {
        preparations: [
          create(BasPreparationSummarySchema, {
            preparation,
            family,
            period,
            profile,
            state: BasPreparationState.SUPERSEDED_BY_CONFIRMED_REVISION,
          }),
        ],
      }),
    ),
  );
  fixture.procedures.basPreparation.listFrozenBas.mockResolvedValue(
    codecs.frozen.encodeResponse(create(ListFrozenBasResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasExternalDisputeResolutions.mockResolvedValue(
    codecs.disputes.encodeResponse(create(ListBasExternalDisputeResolutionsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.getBasCalendar.mockResolvedValue(
    codecs.calendar.encodeResponse(create(GetBasCalendarResponseSchema, {})),
  );
  fixture.procedures.basPreparation.getBasPreparation.mockResolvedValue(
    codecs.preparationDetail.encodeResponse(
      create(GetBasPreparationResponseSchema, {
        preparation: create(BasPreparationSchema, {
          preparationId: preparation.id,
          organisationId: workspace.organisationId,
          version: preparation.revision,
          state: BasPreparationState.SUPERSEDED_BY_CONFIRMED_REVISION,
          contentHash: preparation.contentHash,
          inputHash: new Uint8Array(32).fill(0x2a),
          sourceSnapshotHash: new Uint8Array(32).fill(0x2b),
        }),
        currentChain: create(BasChainExpectationSchema, {
          family,
          expectedFamilyVersion: family.revision,
          branch: create(BasExpectedIdentitySchema, {
            expectedState: { case: "retained", value: branch },
            expectedVersion: branch.revision,
          }),
          activeAttempt: create(BasExpectedIdentitySchema, {
            expectedState: { case: "absent", value: true },
          }),
        }),
        currentDisputeOverlay: BasDisputeOverlayState.CORRECTION_REQUIRED,
        currentBranchResolution: create(SourceRefSchema, {
          type: "bas_branch_resolution",
          id: "018f0000-0000-7000-8000-000000000120",
          revision: 1n,
          contentHash: new Uint8Array(32).fill(0x2c),
        }),
      }),
    ),
  );
  let regenerated: ReturnType<typeof codecs.regenerate.decodeRequest> | undefined;
  fixture.procedures.basPreparation.regenerateBasPreparation.mockImplementation(async (frame) => {
    regenerated = codecs.regenerate.decodeRequest(frame);
    return codecs.regenerate.encodeResponse(
      create(RegenerateBasPreparationResponseSchema, {
        preparation: create(BasPreparationSchema, {
          preparationId: "018f0000-0000-7000-8000-000000000121",
          organisationId: workspace.organisationId,
          version: 1n,
          state: BasPreparationState.COLLECTING,
          contentHash: new Uint8Array(32).fill(0x2d),
          inputHash: new Uint8Array(32).fill(0x2e),
          sourceSnapshotHash: new Uint8Array(32).fill(0x2f),
        }),
      }),
    );
  });

  renderWithConveyor(
    <BasPreparationLifecycle
      api={fixture.conveyor.basPreparation}
      identityApi={fixture.conveyor.organisation}
      reportApi={fixture.conveyor.reporting}
      workspace={workspace}
    />,
  );
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Prepare" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Regenerate exact calculation" })).toHaveProperty(
      "disabled",
      false,
    ),
  );
  await user.click(screen.getByRole("button", { name: "Regenerate exact calculation" }));

  await waitFor(() => expect(regenerated).toBeDefined());
  expect(regenerated?.expectedVersion).toBe(0n);
  const familyState = regenerated?.familyExpectation?.expectedState;
  expect(familyState?.case).toBe("retained");
  if (familyState?.case === "retained") expect(familyState.value.id).toBe(family.id);
  expect(regenerated?.familyExpectation?.expectedVersion).toBe(family.revision);
  expect(regenerated?.preparationExpectation?.expectedState.case).toBe("absent");
  expect(regenerated?.period?.id).toBe(period.id);
  expect(regenerated?.profile?.id).toBe(profile.id);
});

it("keeps a retained review-ready state separate from changed live source currentness", async () => {
  const fixture = createConveyorFixture();
  const workspace = {
    workspaceId: "018f0000-0000-7000-8000-000000000021",
    userId: "018f0000-0000-7000-8000-000000000022",
    sessionId: "018f0000-0000-7000-8000-000000000023",
    organisationId: "018f0000-0000-7000-8000-000000000024",
  };
  const preparationId = "018f0000-0000-7000-8000-000000000025";
  const preparationHash = new Uint8Array(32).fill(0x42);
  const preparationRef = create(SourceRefSchema, {
    type: "bas_preparation",
    id: preparationId,
    revision: 1n,
    contentHash: preparationHash,
  });
  fixture.procedures.organisation.getCurrentUser.mockResolvedValue(
    codecs.user.encodeResponse(
      create(GetCurrentUserResponseSchema, {
        user: create(UserSchema, {
          id: workspace.userId,
          version: 2n,
          username: "lodger",
          displayName: "Lodger",
          state: UserState.ACTIVE,
          roles: [Role.BUSINESS_LODGER],
        }),
      }),
    ),
  );
  fixture.procedures.basPreparation.listBasObligationsProfiles.mockResolvedValue(
    codecs.profiles.encodeResponse(create(ListBasObligationsProfilesResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasPeriods.mockResolvedValue(
    codecs.periods.encodeResponse(create(ListBasPeriodsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasPreparations.mockResolvedValue(
    codecs.preparations.encodeResponse(
      create(ListBasPreparationsResponseSchema, {
        preparations: [
          create(BasPreparationSummarySchema, {
            preparation: preparationRef,
            state: BasPreparationState.REVIEW_READY,
          }),
        ],
      }),
    ),
  );
  fixture.procedures.basPreparation.listFrozenBas.mockResolvedValue(
    codecs.frozen.encodeResponse(create(ListFrozenBasResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasExternalDisputeResolutions.mockResolvedValue(
    codecs.disputes.encodeResponse(create(ListBasExternalDisputeResolutionsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.getBasCalendar.mockResolvedValue(
    codecs.calendar.encodeResponse(create(GetBasCalendarResponseSchema, {})),
  );
  fixture.procedures.basPreparation.getBasPreparation.mockResolvedValue(
    codecs.preparationDetail.encodeResponse(
      create(GetBasPreparationResponseSchema, {
        preparation: create(BasPreparationSchema, {
          preparationId,
          organisationId: workspace.organisationId,
          version: 3n,
          state: BasPreparationState.REVIEW_READY,
          contentHash: preparationHash,
          sourceSnapshotHash: new Uint8Array(32).fill(0x33),
          inputHash: new Uint8Array(32).fill(0x22),
        }),
        currentChain: create(BasChainExpectationSchema, { expectedFamilyVersion: 2n }),
        liveCurrentness: BasLiveCurrentness.SOURCE_CHANGED,
      }),
    ),
  );

  renderWithConveyor(
    <BasPreparationLifecycle
      api={fixture.conveyor.basPreparation}
      identityApi={fixture.conveyor.organisation}
      reportApi={fixture.conveyor.reporting}
      workspace={workspace}
    />,
  );

  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Freeze" }));
  expect(await screen.findByText(/Source changed since this snapshot/)).toBeTruthy();
  await user.type(screen.getByLabelText("Approval reason"), "Source review");
  await user.type(screen.getByLabelText("Authenticator code"), "123456");
  expect(screen.getByRole("button", { name: "Approve and freeze exact snapshot" })).toHaveProperty(
    "disabled",
    true,
  );
  expect(fixture.procedures.basPreparation.freezeBasForManualHandoff).not.toHaveBeenCalled();
});

it.each([
  {
    code: "BAS_SOURCE_CAPACITY",
    message:
      "The complete BAS source exceeds the supported capture capacity. Retain the actual statement period and review source records or contact support.",
  },
  {
    code: "BAS_CONFLICT",
    message:
      "Core could not accept this BAS action. Refresh the exact history and review the current findings before retrying.",
  },
  {
    code: "BAS_IDEMPOTENCY_CONFLICT",
    message:
      "This BAS action key was already used for a different request. Refresh the history and start a new action.",
  },
  {
    code: "BAS_PERMISSION_DENIED",
    message:
      "Your current BAS role does not allow this action. Ask a workspace administrator to review your assigned roles.",
  },
  {
    code: "BAS_AUTHENTICATION_REQUIRED",
    message:
      "Your session could not be verified for this BAS action. Sign in again, then refresh the exact history.",
  },
  {
    code: "BAS_REQUEST_TIMEOUT",
    message:
      "The BAS request timed out. Refresh the exact history before retrying to check whether it was saved.",
  },
])(
  "explains unsupported manual tax facts and gives safe guidance for $code",
  async ({ code, message }) => {
    const fixture = createConveyorFixture();
    const workspace = {
      workspaceId: "018f0000-0000-7000-8000-000000000041",
      userId: "018f0000-0000-7000-8000-000000000042",
      sessionId: "018f0000-0000-7000-8000-000000000043",
      organisationId: "018f0000-0000-7000-8000-000000000044",
    };
    const preparationId = "018f0000-0000-7000-8000-000000000045";
    const preparationHash = new Uint8Array(32).fill(0x45);
    const calculationId = "018f0000-0000-7000-8000-000000000046";
    const calculationHash = new Uint8Array(32).fill(0x46);
    const preparationRef = create(SourceRefSchema, {
      type: "bas_preparation",
      id: preparationId,
      revision: 1n,
      contentHash: preparationHash,
    });
    const calculationRef = create(SourceRefSchema, {
      type: "bas_calculation",
      id: calculationId,
      revision: 1n,
      contentHash: calculationHash,
    });
    const family = create(SourceRefSchema, {
      type: "bas_family",
      id: "018f0000-0000-7000-8000-000000000047",
      revision: 1n,
      contentHash: new Uint8Array(32).fill(0x47),
    });
    const period = create(SourceRefSchema, {
      type: "bas_period",
      id: "018f0000-0000-7000-8000-000000000048",
      revision: 1n,
      contentHash: new Uint8Array(32).fill(0x48),
    });
    const profile = create(SourceRefSchema, {
      type: "bas_obligations_profile",
      id: "018f0000-0000-7000-8000-000000000049",
      revision: 1n,
      contentHash: new Uint8Array(32).fill(0x49),
    });
    const validationId = "018f0000-0000-7000-8000-00000000004a";
    const validationHash = new Uint8Array(32).fill(0x4a);
    const sourceSnapshotHash = new Uint8Array(32).fill(0x4b);
    const warningId = "018f0000-0000-7000-8000-00000000004c";
    const manualTaxFactId = "018f0000-0000-7000-8000-00000000004d";

    fixture.procedures.organisation.getCurrentUser.mockResolvedValue(
      codecs.user.encodeResponse(
        create(GetCurrentUserResponseSchema, {
          user: create(UserSchema, {
            id: workspace.userId,
            version: 2n,
            username: "preparer",
            displayName: "Preparer",
            state: UserState.ACTIVE,
            roles: [Role.BUSINESS_PREPARER],
          }),
        }),
      ),
    );
    fixture.procedures.basPreparation.listBasObligationsProfiles.mockResolvedValue(
      codecs.profiles.encodeResponse(create(ListBasObligationsProfilesResponseSchema, {})),
    );
    fixture.procedures.basPreparation.listBasPeriods.mockResolvedValue(
      codecs.periods.encodeResponse(create(ListBasPeriodsResponseSchema, {})),
    );
    fixture.procedures.basPreparation.listBasPreparations.mockResolvedValue(
      codecs.preparations.encodeResponse(
        create(ListBasPreparationsResponseSchema, {
          preparations: [
            create(BasPreparationSummarySchema, {
              preparation: preparationRef,
              family,
              period,
              profile,
              state: BasPreparationState.REVIEW_READY,
              currentCalculation: calculationRef,
              inputRevision: 1n,
              inputHash: new Uint8Array(32).fill(0x4c),
              updatedAt: create(TimestampSchema, { seconds: 1_792_656_000n }),
            }),
          ],
        }),
      ),
    );
    fixture.procedures.basPreparation.listFrozenBas.mockResolvedValue(
      codecs.frozen.encodeResponse(create(ListFrozenBasResponseSchema, {})),
    );
    fixture.procedures.basPreparation.listBasExternalDisputeResolutions.mockResolvedValue(
      codecs.disputes.encodeResponse(create(ListBasExternalDisputeResolutionsResponseSchema, {})),
    );
    fixture.procedures.basPreparation.getBasPreparation.mockResolvedValue(
      codecs.preparationDetail.encodeResponse(
        create(GetBasPreparationResponseSchema, {
          preparation: create(BasPreparationSchema, {
            preparationId,
            organisationId: workspace.organisationId,
            version: 1n,
            state: BasPreparationState.REVIEW_READY,
            contentHash: preparationHash,
            inputHash: new Uint8Array(32).fill(0x4c),
            sourceSnapshotHash,
          }),
          currentCalculation: create(BasCalculationSchema, {
            calculationId,
            organisationId: workspace.organisationId,
            version: 1n,
            contentHash: calculationHash,
            preparation: preparationRef,
            family,
            period,
            profile,
          }),
          currentValidation: create(BasValidationSnapshotSchema, {
            validationId,
            revision: 1n,
            contentHash: validationHash,
            preparationId,
            preparationHash,
            calculationId,
            calculationHash,
            sourceSnapshotHash,
            createdAt: create(TimestampSchema, { seconds: 1_792_656_000n }),
            inputRevision: 1n,
            inputHash: new Uint8Array(32).fill(0x4c),
            findings: [
              create(BasValidationFindingSchema, {
                findingId: warningId,
                severity: BasFindingSeverity.WARNING,
                stableCode: "SOURCE_REVIEW_NOTICE",
                safeMessage: "Review the exact source records before continuing.",
              }),
              create(BasValidationFindingSchema, {
                findingId: manualTaxFactId,
                severity: BasFindingSeverity.UNSUPPORTED,
                stableCode: "MANUAL_TAX_FACT_UNSUPPORTED",
                safeMessage: "A manual accounting tax fact requires review.",
              }),
            ],
          }),
        }),
      ),
    );
    fixture.procedures.basPreparation.acknowledgeBasWarnings.mockImplementation(async () => {
      throw { code };
    });

    renderWithConveyor(
      <BasPreparationLifecycle
        api={fixture.conveyor.basPreparation}
        identityApi={fixture.conveyor.organisation}
        reportApi={fixture.conveyor.reporting}
        workspace={workspace}
      />,
    );

    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Review" }));
    const findings = await screen.findByRole("heading", { name: "Review findings" });
    const review = within(findings.parentElement as HTMLElement);
    expect(
      review.getByText(
        "External review required: this manual accounting tax treatment is unsupported and must be reviewed externally before inclusion. Tammy will not infer, carry forward, or reclassify it.",
      ),
    ).toBeTruthy();
    await user.click(review.getByLabelText(/Warning · SOURCE_REVIEW_NOTICE/));
    await user.click(screen.getByRole("button", { name: "Acknowledge selected exact warnings" }));
    expect(await screen.findByText(message)).toBeTruthy();
  },
);

it("resolves only the server-derived disputed subject set for a Business Lodger", async () => {
  const fixture = createConveyorFixture();
  const workspace = {
    workspaceId: "018f0000-0000-7000-8000-000000000031",
    userId: "018f0000-0000-7000-8000-000000000032",
    sessionId: "018f0000-0000-7000-8000-000000000033",
    organisationId: "018f0000-0000-7000-8000-000000000034",
  };
  const preparationId = "018f0000-0000-7000-8000-000000000035";
  const preparationHash = new Uint8Array(32).fill(0x51);
  const family = create(SourceRefSchema, {
    type: "bas_family",
    id: "018f0000-0000-7000-8000-000000000036",
    revision: 1n,
    contentHash: new Uint8Array(32).fill(0x61),
  });
  const preparationRef = create(SourceRefSchema, {
    type: "bas_preparation",
    id: preparationId,
    revision: 1n,
    contentHash: preparationHash,
  });
  const disputed = [
    create(SourceRefSchema, {
      type: "bas_external_observation",
      id: "018f0000-0000-7000-8000-000000000037",
      revision: 1n,
      contentHash: new Uint8Array(32).fill(0x71),
    }),
    create(SourceRefSchema, {
      type: "bas_external_observation",
      id: "018f0000-0000-7000-8000-000000000038",
      revision: 1n,
      contentHash: new Uint8Array(32).fill(0x72),
    }),
  ];
  const affected = [
    create(SourceRefSchema, {
      type: "bas_preparation",
      id: "018f0000-0000-7000-8000-000000000039",
      revision: 4n,
      contentHash: new Uint8Array(32).fill(0x73),
    }),
  ];
  const manifestHash = new Uint8Array(32).fill(0x74);
  const assertionId = "018f0000-0000-7000-8000-00000000003a";
  const factor = create(FreshFactorContextSchema, {
    assertionId,
    purpose: "bas_resolve_external_dispute",
    assertedAt: create(TimestampSchema, { seconds: 1_792_656_000n, nanos: 0 }),
  });
  let assertedPurpose = "";
  let resolvedRequest: ReturnType<typeof codecs.resolveDispute.decodeRequest> | undefined;

  fixture.procedures.organisation.getCurrentUser.mockResolvedValue(
    codecs.user.encodeResponse(
      create(GetCurrentUserResponseSchema, {
        user: create(UserSchema, {
          id: workspace.userId,
          version: 2n,
          username: "lodger",
          displayName: "Lodger",
          state: UserState.ACTIVE,
          roles: [Role.BUSINESS_LODGER],
        }),
      }),
    ),
  );
  fixture.procedures.organisation.assertTotp.mockImplementation((frame) => {
    assertedPurpose = codecs.assertTotp.decodeRequest(frame).purpose;
    return Promise.resolve(
      codecs.assertTotp.encodeResponse(create(AssertTOTPResponseSchema, { freshFactor: factor })),
    );
  });
  fixture.procedures.basPreparation.listBasObligationsProfiles.mockResolvedValue(
    codecs.profiles.encodeResponse(create(ListBasObligationsProfilesResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasPeriods.mockResolvedValue(
    codecs.periods.encodeResponse(create(ListBasPeriodsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasPreparations.mockResolvedValue(
    codecs.preparations.encodeResponse(
      create(ListBasPreparationsResponseSchema, {
        preparations: [
          create(BasPreparationSummarySchema, {
            preparation: preparationRef,
            state: BasPreparationState.FROZEN_FOR_MANUAL_HANDOFF,
          }),
        ],
      }),
    ),
  );
  fixture.procedures.basPreparation.listFrozenBas.mockResolvedValue(
    codecs.frozen.encodeResponse(create(ListFrozenBasResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasExternalDisputeResolutions.mockResolvedValue(
    codecs.disputes.encodeResponse(create(ListBasExternalDisputeResolutionsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.getBasCalendar.mockResolvedValue(
    codecs.calendar.encodeResponse(create(GetBasCalendarResponseSchema, {})),
  );
  fixture.procedures.basPreparation.getBasPreparation.mockResolvedValue(
    codecs.preparationDetail.encodeResponse(
      create(GetBasPreparationResponseSchema, {
        preparation: create(BasPreparationSchema, {
          preparationId,
          organisationId: workspace.organisationId,
          version: 1n,
          family,
          state: BasPreparationState.FROZEN_FOR_MANUAL_HANDOFF,
          contentHash: preparationHash,
          sourceSnapshotHash: new Uint8Array(32).fill(0x44),
          inputHash: new Uint8Array(32).fill(0x45),
        }),
        currentChain: create(BasChainExpectationSchema, { family, expectedFamilyVersion: 8n }),
        currentDisputeOverlay: BasDisputeOverlayState.DISPUTED,
        currentDispute: create(BasExternalDisputeSummarySchema, {
          disputedObservations: disputed,
          affectedConfirmedChain: affected,
          dependentChainManifestHash: manifestHash,
        }),
      }),
    ),
  );
  fixture.procedures.basPreparation.resolveExternalBasDispute.mockImplementation((frame) => {
    resolvedRequest = codecs.resolveDispute.decodeRequest(frame);
    return Promise.resolve(
      codecs.resolveDispute.encodeResponse(create(ResolveExternalBasDisputeResponseSchema, {})),
    );
  });

  renderWithConveyor(
    <BasPreparationLifecycle
      api={fixture.conveyor.basPreparation}
      identityApi={fixture.conveyor.organisation}
      reportApi={fixture.conveyor.reporting}
      workspace={workspace}
    />,
  );

  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "External record" }));
  expect(await screen.findByText(/The server derived these exact dispute subjects/)).toBeTruthy();
  await user.type(
    screen.getByLabelText("Dispute resolution reason"),
    "Reviewed retained event history",
  );
  await user.type(
    screen.getByLabelText("Fresh authenticator code for dispute resolution"),
    "123456",
  );
  await user.click(
    screen.getByRole("button", { name: "Resolve exact contested history (fresh TOTP)" }),
  );

  expect(fixture.procedures.organisation.assertTotp).toHaveBeenCalledOnce();
  await waitFor(() =>
    expect(fixture.procedures.basPreparation.resolveExternalBasDispute).toHaveBeenCalledOnce(),
  );
  expect(fixture.procedures.organisation.assertTotp).toHaveBeenCalledOnce();
  expect(assertedPurpose).toBe("bas_resolve_external_dispute");
  expect(resolvedRequest?.expectedVersion).toBe(8n);
  expect(resolvedRequest?.resolution?.disputedObservations.map((ref) => ref.id)).toEqual(
    disputed.map((ref) => ref.id),
  );
  expect(resolvedRequest?.resolution?.affectedConfirmedChain.map((ref) => ref.id)).toEqual(
    affected.map((ref) => ref.id),
  );
  expect(resolvedRequest?.resolution?.dependentChainManifestHash).toEqual(manifestHash);
  expect(resolvedRequest?.commandContext?.freshFactor?.assertionId).toBe(assertionId);
});

it("resolves an exact linked preparation across empty pages and never substitutes a newer revision", async () => {
  const fixture = createConveyorFixture();
  const workspace = {
    workspaceId: "018f0000-0000-7000-8000-000000000081",
    userId: "018f0000-0000-7000-8000-000000000082",
    sessionId: "018f0000-0000-7000-8000-000000000083",
    organisationId: "018f0000-0000-7000-8000-000000000084",
  };
  const profile = create(SourceRefSchema, {
    type: "bas_obligations_profile",
    id: "018f0000-0000-7000-8000-000000000085",
    revision: 2n,
    contentHash: new Uint8Array(32).fill(0x85),
  });
  const period = create(SourceRefSchema, {
    type: "bas_period",
    id: "018f0000-0000-7000-8000-000000000086",
    revision: 4n,
    contentHash: new Uint8Array(32).fill(0x86),
  });
  const preparation = create(SourceRefSchema, {
    type: "bas_preparation",
    id: "018f0000-0000-7000-8000-000000000087",
    revision: 3n,
    contentHash: new Uint8Array(32).fill(0x87),
  });
  fixture.procedures.organisation.getCurrentUser.mockResolvedValue(
    codecs.user.encodeResponse(
      create(GetCurrentUserResponseSchema, {
        user: create(UserSchema, {
          id: workspace.userId,
          version: 3n,
          username: "preparer",
          displayName: "Preparer",
          state: UserState.ACTIVE,
          roles: [Role.BUSINESS_PREPARER],
        }),
      }),
    ),
  );
  fixture.procedures.basPreparation.listBasObligationsProfiles.mockResolvedValue(
    codecs.profiles.encodeResponse(
      create(ListBasObligationsProfilesResponseSchema, {
        profiles: [create(BasObligationsProfileSummarySchema, { profile })],
      }),
    ),
  );
  fixture.procedures.basPreparation.listBasPeriods.mockResolvedValue(
    codecs.periods.encodeResponse(
      create(ListBasPeriodsResponseSchema, {
        periods: [
          create(BasPeriodSummarySchema, {
            period,
            dates: create(BasCivilDateRangeSchema, {
              startDate: { year: 2026, month: 7, day: 1 },
              endDate: { year: 2026, month: 9, day: 30 },
            }),
            window: BasPeriodWindow.JUL_SEP,
            profile,
          }),
        ],
      }),
    ),
  );
  let preparationPage = 0;
  const preparationCursors: string[] = [];
  fixture.procedures.basPreparation.listBasPreparations.mockImplementation(async (frame) => {
    const request = codecs.preparations.decodeRequest(frame);
    preparationPage += 1;
    preparationCursors.push(request.page?.cursor ?? "");
    return codecs.preparations.encodeResponse(
      preparationPage === 1
        ? create(ListBasPreparationsResponseSchema, {
            page: create(PageInfoSchema, { returnedCount: 0, nextCursor: "after-empty" }),
          })
        : create(ListBasPreparationsResponseSchema, {
            preparations: [
              create(BasPreparationSummarySchema, {
                preparation,
                profile,
                period,
                state: BasPreparationState.REVIEW_READY,
              }),
            ],
            page: create(PageInfoSchema, { returnedCount: 1 }),
          }),
    );
  });
  fixture.procedures.basPreparation.listFrozenBas.mockResolvedValue(
    codecs.frozen.encodeResponse(create(ListFrozenBasResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasExternalDisputeResolutions.mockResolvedValue(
    codecs.disputes.encodeResponse(create(ListBasExternalDisputeResolutionsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.getBasCalendar.mockResolvedValue(
    codecs.calendar.encodeResponse(create(GetBasCalendarResponseSchema, {})),
  );
  const detailRequests: ReturnType<typeof codecs.preparationDetail.decodeRequest>[] = [];
  fixture.procedures.basPreparation.getBasPreparation.mockImplementation(async (frame) => {
    detailRequests.push(codecs.preparationDetail.decodeRequest(frame));
    return codecs.preparationDetail.encodeResponse(
      create(GetBasPreparationResponseSchema, {
        preparation: create(BasPreparationSchema, {
          preparationId: preparation.id,
          organisationId: workspace.organisationId,
          version: preparation.revision,
          state: BasPreparationState.REVIEW_READY,
          contentHash: preparation.contentHash,
          inputHash: new Uint8Array(32).fill(0x88),
          sourceSnapshotHash: new Uint8Array(32).fill(0x89),
        }),
      }),
    );
  });

  const exactView = renderWithConveyor(
    <BasPreparationLifecycle
      api={fixture.conveyor.basPreparation}
      identityApi={fixture.conveyor.organisation}
      reportApi={fixture.conveyor.reporting}
      workspace={workspace}
      initialPreparationTarget={{
        id: preparation.id,
        revision: preparation.revision,
        hash: "87".repeat(32),
      }}
    />,
  );

  await waitFor(() => expect(preparationPage).toBe(2));
  await waitFor(() => expect(detailRequests).toHaveLength(1));
  expect(await screen.findAllByText(new RegExp(`${preparation.id} · v3`))).toHaveLength(2);
  expect(detailRequests[0]).toMatchObject({
    preparationId: preparation.id,
    preparationHash: preparation.contentHash,
  });
  expect(preparationCursors).toEqual(["", "after-empty"]);
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Prepare" }));
  await waitFor(() =>
    expect((screen.getByLabelText("Selected immutable profile") as HTMLSelectElement).value).toBe(
      `bas_obligations_profile:${profile.id}:2:${"85".repeat(32)}`,
    ),
  );
  expect((screen.getByLabelText("Confirmed period history") as HTMLSelectElement).value).toBe(
    `bas_period:${period.id}:4:${"86".repeat(32)}`,
  );
  exactView.unmount();

  const staleFixture = createConveyorFixture();
  const newerPreparation = create(SourceRefSchema, {
    type: "bas_preparation",
    id: preparation.id,
    revision: 4n,
    contentHash: new Uint8Array(32).fill(0x90),
  });
  staleFixture.procedures.organisation.getCurrentUser.mockResolvedValue(
    codecs.user.encodeResponse(
      create(GetCurrentUserResponseSchema, {
        user: create(UserSchema, {
          id: workspace.userId,
          version: 3n,
          username: "preparer",
          displayName: "Preparer",
          state: UserState.ACTIVE,
          roles: [Role.BUSINESS_PREPARER],
        }),
      }),
    ),
  );
  staleFixture.procedures.basPreparation.listBasObligationsProfiles.mockResolvedValue(
    codecs.profiles.encodeResponse(
      create(ListBasObligationsProfilesResponseSchema, {
        profiles: [create(BasObligationsProfileSummarySchema, { profile })],
      }),
    ),
  );
  staleFixture.procedures.basPreparation.listBasPeriods.mockResolvedValue(
    codecs.periods.encodeResponse(create(ListBasPeriodsResponseSchema, {})),
  );
  staleFixture.procedures.basPreparation.listBasPreparations.mockResolvedValue(
    codecs.preparations.encodeResponse(
      create(ListBasPreparationsResponseSchema, {
        preparations: [
          create(BasPreparationSummarySchema, {
            preparation: newerPreparation,
            profile,
            period,
            state: BasPreparationState.REVIEW_READY,
          }),
        ],
      }),
    ),
  );
  staleFixture.procedures.basPreparation.listFrozenBas.mockResolvedValue(
    codecs.frozen.encodeResponse(create(ListFrozenBasResponseSchema, {})),
  );
  staleFixture.procedures.basPreparation.listBasExternalDisputeResolutions.mockResolvedValue(
    codecs.disputes.encodeResponse(create(ListBasExternalDisputeResolutionsResponseSchema, {})),
  );
  staleFixture.procedures.basPreparation.getBasCalendar.mockResolvedValue(
    codecs.calendar.encodeResponse(create(GetBasCalendarResponseSchema, {})),
  );
  staleFixture.procedures.basPreparation.getBasPreparation.mockResolvedValue(
    codecs.preparationDetail.encodeResponse(create(GetBasPreparationResponseSchema, {})),
  );

  renderWithConveyor(
    <BasPreparationLifecycle
      api={staleFixture.conveyor.basPreparation}
      identityApi={staleFixture.conveyor.organisation}
      reportApi={staleFixture.conveyor.reporting}
      workspace={workspace}
      initialPreparationTarget={{
        id: preparation.id,
        revision: preparation.revision,
        hash: "87".repeat(32),
      }}
    />,
  );
  expect((await screen.findByRole("alert")).textContent).toContain(
    "This exact BAS preparation link is stale or unavailable.",
  );
  expect(staleFixture.procedures.basPreparation.getBasPreparation).not.toHaveBeenCalled();
  expect(
    screen.getByRole("button", { name: "Run validation on this exact calculation" }),
  ).toHaveProperty("disabled", true);
});

it("gates external observation authority by event kind for a sole Business Lodger", async () => {
  const fixture = createConveyorFixture();
  const workspace = {
    workspaceId: "018f0000-0000-7000-8000-000000000091",
    userId: "018f0000-0000-7000-8000-000000000092",
    sessionId: "018f0000-0000-7000-8000-000000000093",
    organisationId: "018f0000-0000-7000-8000-000000000094",
  };
  const frozenRef = create(SourceRefSchema, {
    type: "frozen_bas",
    id: "018f0000-0000-7000-8000-000000000095",
    revision: 1n,
    contentHash: new Uint8Array(32).fill(0x95),
  });
  fixture.procedures.organisation.getCurrentUser.mockResolvedValue(
    codecs.user.encodeResponse(
      create(GetCurrentUserResponseSchema, {
        user: create(UserSchema, {
          id: workspace.userId,
          version: 2n,
          username: "lodger",
          displayName: "Lodger",
          state: UserState.ACTIVE,
          roles: [Role.BUSINESS_LODGER],
        }),
      }),
    ),
  );
  fixture.procedures.basPreparation.listBasObligationsProfiles.mockResolvedValue(
    codecs.profiles.encodeResponse(create(ListBasObligationsProfilesResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasPeriods.mockResolvedValue(
    codecs.periods.encodeResponse(create(ListBasPeriodsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasPreparations.mockResolvedValue(
    codecs.preparations.encodeResponse(create(ListBasPreparationsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listFrozenBas.mockResolvedValue(
    codecs.frozen.encodeResponse(
      create(ListFrozenBasResponseSchema, {
        frozenBas: [create(BasFrozenBasSummarySchema, { frozenBas: frozenRef })],
      }),
    ),
  );
  fixture.procedures.basPreparation.listBasExternalDisputeResolutions.mockResolvedValue(
    codecs.disputes.encodeResponse(create(ListBasExternalDisputeResolutionsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.listBasExternalObservations.mockResolvedValue(
    codecs.observations.encodeResponse(create(ListBasExternalObservationsResponseSchema, {})),
  );
  fixture.procedures.basPreparation.getBasCalendar.mockResolvedValue(
    codecs.calendar.encodeResponse(create(GetBasCalendarResponseSchema, {})),
  );

  renderWithConveyor(
    <BasPreparationLifecycle
      api={fixture.conveyor.basPreparation}
      identityApi={fixture.conveyor.organisation}
      reportApi={fixture.conveyor.reporting}
      workspace={workspace}
    />,
  );
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "External record" }));
  await user.type(screen.getByLabelText("User-recorded reason"), "User said the return is pending");
  const record = screen.getByRole("button", { name: "Record user-reported event as unverified" });
  expect(record).toHaveProperty("disabled", true);

  const eventSelect = screen.getByLabelText("User-recorded event");
  for (const kind of [
    BasExternalEventKind.PENDING,
    BasExternalEventKind.REJECTION,
    BasExternalEventKind.REVIEWER_HANDOFF,
    BasExternalEventKind.PAYMENT,
  ]) {
    await user.selectOptions(eventSelect, String(kind));
    expect(record).toHaveProperty("disabled", true);
  }

  await user.selectOptions(eventSelect, String(BasExternalEventKind.COMPLETED_LODGEMENT));
  const factor = screen.getByLabelText("Fresh authenticator code for external confirmation");
  await user.type(factor, "123456");
  expect(record).toHaveProperty("disabled", false);
  await user.selectOptions(eventSelect, String(BasExternalEventKind.ACCEPTANCE));
  expect(record).toHaveProperty("disabled", false);
  expect(fixture.procedures.basPreparation.recordExternalBasObservation).not.toHaveBeenCalled();
  expect(fixture.procedures.organisation.assertTotp).not.toHaveBeenCalled();
});
