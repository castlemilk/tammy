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
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { SetupScreen } from "./setup-screen";

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

it("creates, confirms, and signs in to a real local workspace through named protobuf methods", async () => {
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
  const api = {
    createWorkspace,
    confirmRecovery,
    unlockWorkspace: vi.fn(),
    signIn,
    createOrganisation,
    createAccount: vi.fn(),
    listAccounts: vi.fn(),
    postManualJournal: vi.fn(),
    listJournals: vi.fn(),
    getJournal: vi.fn(),
    getTrialBalance: vi.fn(),
    importBankStatement: vi.fn(),
    listBankStatementLines: vi.fn(),
    matchBankStatementLine: vi.fn(),
    completeBankReconciliation: vi.fn(),
    getBankingSummary: vi.fn(),
    ingestDocument: vi.fn(),
    listDocuments: vi.fn(),
    getDocument: vi.fn(),
    saveDocumentReview: vi.fn(),
    createBasDraft: vi.fn(),
    getCurrentBasDraft: vi.fn(),
    getReportingCapability: vi.fn(async (frame: Uint8Array) => {
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
            summary: "Tammy supports a local reviewed-document GST workpaper only.",
          }),
        }),
      );
    }),
    getAttentionSummary: vi.fn(),
    getCurrentUser: vi.fn(async (frame: Uint8Array) => {
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
    }),
    enrolTotp: vi.fn(),
    confirmTotp: vi.fn(),
    assertTotp: vi.fn(),
    getOrganisation: vi.fn(),
    recordEntityVerification: vi.fn(),
    getSbrReadiness: vi.fn(),
    getMachineCredentialStatus: vi.fn(),
    removeMachineCredential: vi.fn(),
    removeSbrProductId: vi.fn(),
    runSbrReadinessFixture: vi.fn(),
    selectMachineCredentialFile: vi.fn(async () => ({ selected: false as const })),
    importMachineCredential: vi.fn(),
    replaceMachineCredential: vi.fn(),
    unlockMachineCredential: vi.fn(),
    importSbrProductId: vi.fn(),
    getSystemDiagnostics: vi.fn(),
  } satisfies TammyDesktopAPI;
  const onAuthenticated = vi.fn();
  const user = userEvent.setup();

  render(<SetupScreen api={api} onAuthenticated={onAuthenticated} />);
  expect(
    await screen.findByText("Tammy supports a local reviewed-document GST workpaper only."),
  ).toBeTruthy();
  expect(
    screen.getByText("Tammy does not prepare, declare, or lodge a complete BAS."),
  ).toBeTruthy();
  await user.type(screen.getByLabelText("Your name"), "Tammy Admin");
  await user.type(screen.getByLabelText("Email or username"), "admin@tammy.local");
  await user.type(screen.getByLabelText("Business legal name"), "Tammy Business Pty Ltd");
  await user.type(screen.getByLabelText("Business display name"), "Tammy Business");
  await user.type(screen.getByLabelText("ABN"), "51824753556");
  await user.type(
    screen.getByLabelText("Workspace passphrase"),
    "workspace-passphrase-long-enough",
  );
  await user.type(
    screen.getByLabelText("Administrator password"),
    "administrator-password-long-enough",
  );
  await user.click(screen.getByRole("button", { name: "Create local workspace" }));

  expect(await screen.findByText(new TextDecoder().decode(recovery))).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "I saved my recovery code" }));

  expect(createWorkspace).toHaveBeenCalledOnce();
  expect(confirmRecovery).toHaveBeenCalledOnce();
  expect(signIn).toHaveBeenCalledOnce();
  expect(createOrganisation).toHaveBeenCalledOnce();
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
});
