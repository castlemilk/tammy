import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { type SourceRef, SourceRefSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  AssertTOTPRequestSchema,
  AssertTOTPResponseSchema,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  GetOrganisationRequestSchema,
  GetOrganisationResponseSchema,
  GstBasis,
  OrganisationSchema,
  UpdateOrganisationRequestSchema,
  UpdateOrganisationResponseSchema,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import { act, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import {
  BUNDLED_TAX_RULE_SOURCE,
  createBundledTaxRuleSourceRef,
} from "../../../shared/bundled-tax-rule";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { createConveyorFixture, renderWithConveyor } from "../../test/conveyor-fixtures";
import { LegacyRuleReferenceRepair, nextMelbourneCivilDate } from "./legacy-rule-reference-repair";

const getCodec = createProtoMethodCodec({
  input: GetOrganisationRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetOrganisationResponseSchema,
});
const updateCodec = createProtoMethodCodec({
  input: UpdateOrganisationRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 32_768,
  output: UpdateOrganisationResponseSchema,
});
const assertCodec = createProtoMethodCodec({
  input: AssertTOTPRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 8_192,
  output: AssertTOTPResponseSchema,
});

const workspace = {
  organisationId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073994",
  sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073993",
  userId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
  workspaceId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073991",
} as const;
const oldHash = "e2f9cde094db43c30260dc54f089a1ab835912e75e8c29add5c7a240e90497e4";
const oldReference: SourceRef = create(SourceRefSchema, {
  type: BUNDLED_TAX_RULE_SOURCE.type,
  id: BUNDLED_TAX_RULE_SOURCE.id,
  revision: BUNDLED_TAX_RULE_SOURCE.revision,
  contentHash: Uint8Array.from(oldHash.match(/.{2}/gu) ?? [], (pair) => Number.parseInt(pair, 16)),
});
const factor = {
  assertionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073995",
  purpose: "update_organisation_high_risk",
  assertedAt: create(TimestampSchema, { seconds: BigInt(Math.floor(Date.now() / 1000)) }),
};

function organisation(reference: SourceRef = oldReference, version = 4n) {
  return create(OrganisationSchema, {
    id: workspace.organisationId,
    version,
    abn: "51824753556",
    legalName: "Tammy Business Pty Ltd",
    displayName: "Tammy Business",
    entityType: "AU_PRIVATE_COMPANY",
    gstBasis: GstBasis.NON_CASH,
    activeTaxRuleBundle: reference,
  });
}

function componentApi(reference = oldReference, expectedEffectiveDate = nextMelbourneCivilDate()) {
  const fixture = createConveyorFixture();
  let updateRequest: ReturnType<typeof updateCodec.decodeRequest> | undefined;
  const getOrganisation = fixture.procedures.organisation.getOrganisation;
  getOrganisation.mockImplementation(async (frame: Uint8Array) => {
    const request = getCodec.decodeRequest(frame);
    expect(request.organisationId).toBe(workspace.organisationId);
    expect(request.authentication).toMatchObject({
      actorUserId: workspace.userId,
      sessionId: workspace.sessionId,
    });
    return getCodec.encodeResponse(
      create(GetOrganisationResponseSchema, { organisation: organisation(reference) }),
    );
  });
  const assertTotp = fixture.procedures.organisation.assertTotp;
  assertTotp.mockImplementation(async (frame: Uint8Array) => {
    const request = assertCodec.decodeRequest(frame);
    expect(request.code?.value).toBe("123456");
    expect(request.purpose).toBe("update_organisation_high_risk");
    expect(request.authentication).toMatchObject({
      actorUserId: workspace.userId,
      sessionId: workspace.sessionId,
    });
    return assertCodec.encodeResponse(create(AssertTOTPResponseSchema, { freshFactor: factor }));
  });
  const updateOrganisation = fixture.procedures.organisation.updateOrganisation;
  updateOrganisation.mockImplementation(async (frame: Uint8Array) => {
    updateRequest = updateCodec.decodeRequest(frame);
    return updateCodec.encodeResponse(
      create(UpdateOrganisationResponseSchema, {
        organisation: organisation(createBundledTaxRuleSourceRef(), 5n),
      }),
    );
  });
  return {
    api: fixture.conveyor.organisation,
    assertTotp,
    getOrganisation,
    get updateRequest() {
      return updateRequest;
    },
    updateOrganisation,
    expectedEffectiveDate,
  };
}

async function openAndCheck(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByText("Tax-rule reference"));
  await user.click(screen.getByRole("button", { name: "Check stored tax-rule reference" }));
}

it("repairs only the exact known legacy reference with the high-risk fresh-factor request", async () => {
  const user = userEvent.setup();
  const effectiveDate = nextMelbourneCivilDate();
  const api = componentApi(oldReference, effectiveDate);
  const onUpdated = vi.fn();
  renderWithConveyor(
    <LegacyRuleReferenceRepair
      api={api.api}
      canManage
      onUpdated={onUpdated}
      workspace={workspace}
    />,
  );

  expect(api.getOrganisation).not.toHaveBeenCalled();
  await openAndCheck(user);
  expect(await screen.findByText(/known legacy tax-rule reference/u)).toBeTruthy();
  expect(screen.getByLabelText("Tax-rule reference")).toBeTruthy();
  expect(screen.getByText(/Settings → SBR readiness/u)).toBeTruthy();
  expect(screen.getByText(/SBR machine credential is not required/u)).toBeTruthy();
  await user.type(
    screen.getByLabelText("Reason for rule-reference update"),
    "Correct the retained bundled rule reference.",
  );
  const date = screen.getByLabelText("Effective from");
  await user.clear(date);
  await user.type(date, effectiveDate);
  await user.type(screen.getByLabelText("Fresh TOTP code"), "123456");
  await user.click(screen.getByRole("button", { name: "Confirm tax-rule reference update" }));

  expect(await screen.findByText(/Frozen BAS history is unchanged/u)).toBeTruthy();
  expect(api.assertTotp).toHaveBeenCalledOnce();
  expect(api.updateOrganisation).toHaveBeenCalledOnce();
  expect(onUpdated).toHaveBeenCalledOnce();
  expect(api.updateRequest?.organisationId).toBe(workspace.organisationId);
  expect(api.updateRequest?.expectedVersion).toBe(4n);
  expect(api.updateRequest?.updateMask?.paths).toEqual(["active_tax_rule_bundle"]);
  expect(api.updateRequest?.patch?.activeTaxRuleBundle).toMatchObject({
    type: BUNDLED_TAX_RULE_SOURCE.type,
    id: BUNDLED_TAX_RULE_SOURCE.id,
    revision: BUNDLED_TAX_RULE_SOURCE.revision,
    contentHash: createBundledTaxRuleSourceRef().contentHash,
  });
  expect(api.updateRequest?.reason).toBe("Correct the retained bundled rule reference.");
  const [year = "0", month = "0", day = "0"] = api.expectedEffectiveDate.split("-");
  expect(api.updateRequest?.effectiveDate).toMatchObject({
    year: Number(year),
    month: Number(month),
    day: Number(day),
  });
  expect(api.updateRequest?.commandContext?.freshFactor).toMatchObject(factor);
});

it("offers no repair for the current canonical source", async () => {
  const currentUser = userEvent.setup();
  const currentApi = componentApi(createBundledTaxRuleSourceRef());
  renderWithConveyor(
    <LegacyRuleReferenceRepair api={currentApi.api} canManage workspace={workspace} />,
  );
  await openAndCheck(currentUser);
  expect(await screen.findByText(/already uses the supported tax-rule reference/u)).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Confirm tax-rule reference update" })).toBeNull();
  expect(currentApi.updateOrganisation).not.toHaveBeenCalled();
});

it("offers no repair for an unrecognized reference", async () => {
  const user = userEvent.setup();
  const wrongHash = { ...oldReference, contentHash: new Uint8Array(32).fill(9) };
  const unknownApi = componentApi(wrongHash);
  renderWithConveyor(
    <LegacyRuleReferenceRepair api={unknownApi.api} canManage workspace={workspace} />,
  );
  await openAndCheck(user);
  expect(await screen.findByText(/one recognized legacy value/u)).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Confirm tax-rule reference update" })).toBeNull();
  expect(unknownApi.updateOrganisation).not.toHaveBeenCalled();
});

it("shows corrective guidance but no update form to a non-administrator", async () => {
  const user = userEvent.setup();
  const api = componentApi();
  renderWithConveyor(
    <LegacyRuleReferenceRepair api={api.api} canManage={false} workspace={workspace} />,
  );
  await openAndCheck(user);
  expect(await screen.findByText(/Ask a workspace administrator/u)).toBeTruthy();
  expect(screen.queryByLabelText("Fresh TOTP code")).toBeNull();
  expect(screen.queryByRole("button", { name: "Confirm tax-rule reference update" })).toBeNull();
  expect(api.updateOrganisation).not.toHaveBeenCalled();
});

it("requires a fresh stored-reference check after a stale update failure", async () => {
  const user = userEvent.setup();
  const effectiveDate = nextMelbourneCivilDate();
  const api = componentApi(oldReference, effectiveDate);
  api.updateOrganisation.mockRejectedValueOnce(new Error("stale version"));
  renderWithConveyor(<LegacyRuleReferenceRepair api={api.api} canManage workspace={workspace} />);
  await openAndCheck(user);
  await user.type(
    screen.getByLabelText("Reason for rule-reference update"),
    "Correct the retained bundled rule reference.",
  );
  const date = screen.getByLabelText("Effective from");
  await user.clear(date);
  await user.type(date, effectiveDate);
  await user.type(screen.getByLabelText("Fresh TOTP code"), "123456");
  await user.click(screen.getByRole("button", { name: "Confirm tax-rule reference update" }));
  expect(await screen.findByText(/Check the stored reference again before retrying/u)).toBeTruthy();
  expect(screen.queryByLabelText("Fresh TOTP code")).toBeNull();
  expect(api.updateOrganisation).toHaveBeenCalledOnce();
});

it("ignores a stored-reference response that arrives after the workspace scope changes", async () => {
  const user = userEvent.setup();
  const fixture = createConveyorFixture();
  let resolveOldResponse: (() => void) | undefined;
  const oldResponse = new Promise<Uint8Array>((resolve) => {
    resolveOldResponse = () =>
      resolve(
        getCodec.encodeResponse(
          create(GetOrganisationResponseSchema, { organisation: organisation(oldReference) }),
        ),
      );
  });
  fixture.procedures.organisation.getOrganisation.mockReturnValueOnce(oldResponse);
  const view = renderWithConveyor(
    <LegacyRuleReferenceRepair
      api={fixture.conveyor.organisation}
      canManage
      workspace={workspace}
    />,
  );

  await user.click(screen.getByText("Tax-rule reference"));
  await user.click(screen.getByRole("button", { name: "Check stored tax-rule reference" }));
  expect(fixture.procedures.organisation.getOrganisation).toHaveBeenCalledOnce();

  const nextWorkspace = {
    ...workspace,
    organisationId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073996",
    workspaceId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073997",
  };
  view.rerender(
    <LegacyRuleReferenceRepair
      api={fixture.conveyor.organisation}
      canManage
      workspace={nextWorkspace}
    />,
  );
  await act(async () => {
    resolveOldResponse?.();
    await oldResponse;
  });

  expect(screen.queryByText(oldHash)).toBeNull();
  expect(screen.queryByText("This workspace uses the known legacy tax-rule reference.")).toBeNull();
  expect(screen.queryByLabelText("Reason for rule-reference update")).toBeNull();
  expect(screen.queryByRole("button", { name: "Confirm tax-rule reference update" })).toBeNull();
  expect(fixture.procedures.organisation.updateOrganisation).not.toHaveBeenCalled();
});
