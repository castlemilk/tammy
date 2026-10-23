import { createHash } from "node:crypto";
import { create } from "@bufbuild/protobuf";
import { OneTimeSecretOutputSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  GetCurrentUserRequestSchema,
  GetCurrentUserResponseSchema,
  Role,
  SessionSchema,
  SignInRequestSchema,
  SignInResponseSchema,
  UserSchema,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  CreateOrganisationRequestSchema,
  CreateOrganisationResponseSchema,
  GstBasis,
  OrganisationSchema,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import {
  GetReportingCapabilityRequestSchema,
  GetReportingCapabilityResponseSchema,
  ReportingCapabilitySchema,
  ReportingCapabilityStatus,
  ReportingEntityType,
  ReportKind,
} from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import {
  ConfirmRecoveryRequestSchema,
  ConfirmRecoveryResponseSchema,
  CreateWorkspaceRequestSchema,
  CreateWorkspaceResponseSchema,
  WorkspaceSchema,
} from "@tammy/connect-client/tammy/v1/workspace_pb.js";
import { fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import retainedRuleBytes from "../../../../../../services/core/internal/artefacts/bundles/au_gst_v1.pb.json?raw";

import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { createConveyorFixture, renderWithConveyor as render } from "../../test/conveyor-fixtures";
import { SetupScreen } from "./setup-screen";

function cachedQueryMaterial(
  queryClient: ReturnType<typeof createConveyorFixture>["queryClient"],
): unknown[] {
  return queryClient
    .getQueryCache()
    .getAll()
    .flatMap((query) => [query.queryKey, query.state.data]);
}

function containsText(value: unknown, text: string, seen = new WeakSet<object>()): boolean {
  if (typeof value === "string") return value.includes(text);
  if (value instanceof Uint8Array) return new TextDecoder().decode(value).includes(text);
  if (typeof value !== "object" || value === null || seen.has(value)) return false;
  seen.add(value);
  return Object.values(value).some((child) => containsText(child, text, seen));
}

const createCodec = createProtoMethodCodec({
  input: CreateWorkspaceRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 65_536,
  output: CreateWorkspaceResponseSchema,
});
const confirmCodec = createProtoMethodCodec({
  input: ConfirmRecoveryRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
  output: ConfirmRecoveryResponseSchema,
});
const signInCodec = createProtoMethodCodec({
  input: SignInRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 32_768,
  output: SignInResponseSchema,
});
const createOrganisationCodec = createProtoMethodCodec({
  input: CreateOrganisationRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 32_768,
  output: CreateOrganisationResponseSchema,
});
const reportingCodec = createProtoMethodCodec({
  input: GetReportingCapabilityRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetReportingCapabilityResponseSchema,
});
const currentUserCodec = createProtoMethodCodec({
  input: GetCurrentUserRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetCurrentUserResponseSchema,
});

it.each([GstBasis.NON_CASH, GstBasis.CASH])(
  "creates, confirms, and signs in with GST basis %s through named protobuf methods",
  async (gstBasis) => {
    const workspace = create(WorkspaceSchema, {
      id: "01900f3c-7b2e-7cc4-98c4-dc0c0c073991",
      version: 1n,
    });
    const recovery = new TextEncoder().encode(
      "ABCD-EFGH-IJKL-MNOP-QRST-UVWX-YZ23-4567-ABCD-EFGH-IJKL-MNOP-QRST",
    );
    const createWorkspace = vi.fn(async (frame: Uint8Array) => {
      const request = createCodec.decodeRequest(frame);
      expect(request.destination?.capabilityId).toBe("local-workspace-directory");
      expect(request.administratorUsername).toBe("admin@tammy.local");
      return createCodec.encodeResponse(
        create(CreateWorkspaceResponseSchema, {
          workspace,
          recoverySecret: create(OneTimeSecretOutputSchema, { utf8: recovery }),
        }),
      );
    });
    const confirmRecovery = vi.fn(async (frame: Uint8Array) => {
      const request = confirmCodec.decodeRequest(frame);
      expect(request.confirmations.map((group) => new TextDecoder().decode(group.value))).toEqual([
        "ABCD",
        "EFGH",
      ]);
      return confirmCodec.encodeResponse(create(ConfirmRecoveryResponseSchema, { workspace }));
    });
    const signIn = vi.fn(async (frame: Uint8Array) => {
      const request = signInCodec.decodeRequest(frame);
      expect(request.username).toBe("admin@tammy.local");
      return signInCodec.encodeResponse(
        create(SignInResponseSchema, {
          user: create(UserSchema, {
            id: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
            username: request.username,
            roles: [Role.WORKSPACE_ADMIN],
          }),
          session: create(SessionSchema, { id: "01900f3c-7b2e-7cc4-98c4-dc0c0c073993" }),
        }),
      );
    });
    const createOrganisation = vi.fn(async (frame: Uint8Array) => {
      const request = createOrganisationCodec.decodeRequest(frame);
      expect(request.abn).toBe("51824753556");
      expect(request.legalName).toBe("Tammy Business Pty Ltd");
      expect(request.displayName).toBe("Tammy Business");
      expect(request.gstBasis).toBe(gstBasis);
      expect(request.commandContext?.authentication).toMatchObject({
        actorUserId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
        sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073993",
      });
      expect(request.activeTaxRuleBundle).toMatchObject({
        type: "tax_rule_bundle",
        id: "018f0000-0000-7000-8000-000000000022",
        revision: 1n,
      });
      return createOrganisationCodec.encodeResponse(
        create(CreateOrganisationResponseSchema, {
          organisation: create(OrganisationSchema, {
            id: "01900f3c-7b2e-7cc4-98c4-dc0c0c073994",
            displayName: request.displayName,
            legalName: request.legalName,
            abn: request.abn,
            version: 1n,
          }),
        }),
      );
    });
    const fixture = createConveyorFixture();
    fixture.procedures.workspace.createWorkspace.mockImplementation(createWorkspace);
    fixture.procedures.workspace.confirmRecovery.mockImplementation(confirmRecovery);
    fixture.procedures.workspace.signIn.mockImplementation(signIn);
    fixture.procedures.organisation.createOrganisation.mockImplementation(createOrganisation);
    fixture.procedures.reporting.getReportingCapability.mockImplementation(
      async (frame: Uint8Array) => {
        const request = reportingCodec.decodeRequest(frame);
        expect(request).toMatchObject({
          report: ReportKind.GST_WORKPAPER,
          entityType: ReportingEntityType.AU_BUSINESS,
          taxYear: 2024,
        });
        return reportingCodec.encodeResponse(
          create(GetReportingCapabilityResponseSchema, {
            capability: create(ReportingCapabilitySchema, {
              report: request.report,
              taxYear: request.taxYear,
              entityType: request.entityType,
              status: ReportingCapabilityStatus.AVAILABLE,
              appVersion: "test-core",
              summary: "Tammy supports a local invoice and reconciled purchase GST workpaper.",
            }),
          }),
        );
      },
    );
    fixture.procedures.organisation.getCurrentUser.mockImplementation(async (frame: Uint8Array) => {
      const request = currentUserCodec.decodeRequest(frame);
      expect(request.authentication).toMatchObject({
        actorUserId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
        sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073993",
      });
      return currentUserCodec.encodeResponse(
        create(GetCurrentUserResponseSchema, {
          user: create(UserSchema, {
            id: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
            username: "admin@tammy.local",
            displayName: "Tammy Admin",
            roles: [Role.WORKSPACE_ADMIN],
          }),
        }),
      );
    });
    const onAuthenticated = vi.fn();
    const user = userEvent.setup();

    render(
      <SetupScreen
        onAuthenticated={onAuthenticated}
        organisation={fixture.conveyor.organisation}
        reporting={fixture.conveyor.reporting}
        workspace={fixture.conveyor.workspace}
      />,
    );
    expect(
      await screen.findByText(
        "Tammy supports a local invoice and reconciled purchase GST workpaper.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Local BAS preparation requires confirmed supported obligations and source evidence. Tammy does not declare or lodge a BAS with the ATO.",
      ),
    ).toBeTruthy();
    await user.type(screen.getByLabelText("Your name"), "Tammy Admin");
    await user.type(screen.getByLabelText("Email or username"), "admin@tammy.local");
    await user.type(screen.getByLabelText("Business legal name"), "Tammy Business Pty Ltd");
    await user.type(screen.getByLabelText("Business display name"), "Tammy Business");
    await user.type(screen.getByLabelText("ABN"), "51 824 753 556");
    const lookup = screen.getByRole("link", {
      name: "Look up this ABN on the Australian Business Register",
    });
    expect(lookup.getAttribute("href")).toBe(
      "https://abr.business.gov.au/ABN/View?abn=51824753556",
    );
    expect(lookup.getAttribute("target")).toBe("_blank");
    expect(lookup.getAttribute("rel")).toBe("noopener noreferrer");
    expect(screen.getByRole("combobox", { name: "GST accounting basis" })).toHaveProperty(
      "value",
      String(GstBasis.NON_CASH),
    );
    await user.selectOptions(
      screen.getByRole("combobox", { name: "GST accounting basis" }),
      String(gstBasis),
    );
    await user.type(
      screen.getByLabelText("Workspace passphrase"),
      "workspace-passphrase-long-enough",
    );
    await user.type(
      screen.getByLabelText("Confirm workspace passphrase"),
      "workspace-passphrase-long-enough",
    );
    await user.type(
      screen.getByLabelText("Administrator password"),
      "administrator-password-long-enough",
    );
    await user.type(
      screen.getByLabelText("Confirm administrator password"),
      "administrator-password-long-enough",
    );
    await user.click(screen.getByRole("button", { name: "Create local workspace" }));

    expect(await screen.findByText(new TextDecoder().decode(recovery))).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "I saved my recovery code" }));

    expect(createWorkspace).toHaveBeenCalledOnce();
    expect(confirmRecovery).toHaveBeenCalledOnce();
    expect(signIn).toHaveBeenCalledOnce();
    await vi.waitFor(() =>
      expect(onAuthenticated).toHaveBeenCalledWith({
        sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073993",
        userId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
        workspaceId: workspace.id,
        organisationId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073994",
        organisationDisplayName: "Tammy Business",
        organisationCanonicalAbn: "51824753556",
        roles: [Role.WORKSPACE_ADMIN],
      }),
    );
    expect(createOrganisation).toHaveBeenCalledOnce();
    const firstCreateCall = createOrganisation.mock.calls[0];
    if (!firstCreateCall) throw new Error("Organisation was not created");
    const organisationRequest = createOrganisationCodec.decodeRequest(firstCreateCall[0]);
    const retainedRule = JSON.parse(retainedRuleBytes) as {
      bundle_id: string;
      revision: number;
    };
    expect(organisationRequest.activeTaxRuleBundle).toMatchObject({
      type: "tax_rule_bundle",
      id: retainedRule.bundle_id,
      revision: BigInt(retainedRule.revision),
      contentHash: new Uint8Array(createHash("sha256").update(retainedRuleBytes).digest()),
    });
    const cached = cachedQueryMaterial(fixture.queryClient);
    for (const secret of [
      "workspace-passphrase-long-enough",
      "administrator-password-long-enough",
      "ABCD-EFGH-IJKL-MNOP",
    ]) {
      expect(cached.some((value) => containsText(value, secret))).toBe(false);
    }
  },
);

it.each([
  {
    abn: "51824753557",
    passphrase: "workspace-passphrase-long-enough",
    confirmation: "workspace-passphrase-long-enough",
    expected: "This number does not pass the ABN checksum. Check it against the register.",
  },
  {
    abn: "51824753556",
    passphrase: "workspace-passphrase-long-enough",
    confirmation: "workspace-passphrase-does-not-match",
    expected: "Passwords do not match yet.",
  },
  {
    abn: "51824753556",
    passphrase: "a".repeat(14),
    confirmation: "a".repeat(14),
    expected: "Use 15–128 characters.",
  },
])(
  "blocks workspace creation for a failing ABN or credential check",
  async ({ abn, passphrase, confirmation, expected }) => {
    const fixture = createConveyorFixture();
    const user = userEvent.setup();
    render(
      <SetupScreen
        onAuthenticated={vi.fn()}
        organisation={fixture.conveyor.organisation}
        reporting={fixture.conveyor.reporting}
        workspace={fixture.conveyor.workspace}
      />,
    );

    await user.type(screen.getByLabelText("Your name"), "Tammy Admin");
    await user.type(screen.getByLabelText("Email or username"), "admin@tammy.local");
    await user.type(screen.getByLabelText("Business legal name"), "Tammy Business Pty Ltd");
    await user.type(screen.getByLabelText("Business display name"), "Tammy Business");
    await user.type(screen.getByLabelText("ABN"), abn);
    await user.type(screen.getByLabelText("Workspace passphrase"), passphrase);
    await user.type(screen.getByLabelText("Confirm workspace passphrase"), confirmation);
    await user.type(
      screen.getByLabelText("Administrator password"),
      "administrator-password-long-enough",
    );
    await user.type(
      screen.getByLabelText("Confirm administrator password"),
      "administrator-password-long-enough",
    );

    const createButton = screen.getByRole("button", { name: "Create local workspace" });
    expect(createButton.hasAttribute("disabled")).toBe(true);
    expect(fixture.procedures.workspace.createWorkspace).not.toHaveBeenCalled();
    expect(screen.getByText(expected)).toBeTruthy();
    if (abn === "51824753557") {
      expect(
        screen.queryByRole("link", {
          name: "Look up this ABN on the Australian Business Register",
        }),
      ).toBeNull();
    }
  },
);

it.each(["Business legal name", "Business display name"])(
  "blocks workspace creation when %s contains only whitespace",
  async (blankNameLabel) => {
    const fixture = createConveyorFixture();
    const user = userEvent.setup();
    render(
      <SetupScreen
        onAuthenticated={vi.fn()}
        organisation={fixture.conveyor.organisation}
        reporting={fixture.conveyor.reporting}
        workspace={fixture.conveyor.workspace}
      />,
    );

    await user.type(screen.getByLabelText("Your name"), "Tammy Admin");
    await user.type(screen.getByLabelText("Email or username"), "admin@tammy.local");
    await user.type(screen.getByLabelText("Business legal name"), "Tammy Business Pty Ltd");
    await user.type(screen.getByLabelText("Business display name"), "Tammy Business");
    await user.clear(screen.getByLabelText(blankNameLabel));
    await user.type(screen.getByLabelText(blankNameLabel), "   ");
    await user.type(screen.getByLabelText("ABN"), "51824753556");
    await user.type(
      screen.getByLabelText("Workspace passphrase"),
      "workspace-passphrase-long-enough",
    );
    await user.type(
      screen.getByLabelText("Confirm workspace passphrase"),
      "workspace-passphrase-long-enough",
    );
    await user.type(
      screen.getByLabelText("Administrator password"),
      "administrator-password-long-enough",
    );
    await user.type(
      screen.getByLabelText("Confirm administrator password"),
      "administrator-password-long-enough",
    );

    const createButton = screen.getByRole("button", { name: "Create local workspace" });
    expect(createButton.hasAttribute("disabled")).toBe(true);
    expect(screen.getByText("Enter a business name.")).toBeTruthy();
    const form = createButton.closest("form");
    if (!form) throw new Error("Setup form was not rendered");
    fireEvent.submit(form);

    expect(fixture.procedures.workspace.createWorkspace).not.toHaveBeenCalled();
    expect(screen.getByRole("alert").textContent).toBe(
      "Enter a value in each name field before creating the workspace.",
    );
  },
);
