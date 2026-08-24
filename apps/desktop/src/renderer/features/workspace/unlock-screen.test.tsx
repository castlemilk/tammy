import { create } from "@bufbuild/protobuf";
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
  GetOrganisationRequestSchema,
  GetOrganisationResponseSchema,
  OrganisationSchema,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import {
  UnlockWorkspaceRequestSchema,
  UnlockWorkspaceResponseSchema,
  WorkspaceSchema,
} from "@tammy/connect-client/tammy/v1/workspace_pb.js";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { UnlockScreen } from "./unlock-screen";

const unlockCodec = createProtoMethodCodec({
  input: UnlockWorkspaceRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
  output: UnlockWorkspaceResponseSchema,
});
const signInCodec = createProtoMethodCodec({
  input: SignInRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 32_768,
  output: SignInResponseSchema,
});
const currentUserCodec = createProtoMethodCodec({
  input: GetCurrentUserRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetCurrentUserResponseSchema,
});
const organisationCodec = createProtoMethodCodec({
  input: GetOrganisationRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetOrganisationResponseSchema,
});

it("unlocks and signs in to an existing local workspace through named protobuf methods", async () => {
  const workspace = create(WorkspaceSchema, {
    id: "01900f3c-7b2e-7cc4-98c4-dc0c0c073991",
    version: 2n,
  });
  const unlockWorkspace = vi.fn(async (frame: Uint8Array) => {
    const request = unlockCodec.decodeRequest(frame);
    expect(request.workspaceFile?.capabilityId).toBe("local-workspace-file");
    expect(request.proof?.proof.case).toBe("passphrase");
    if (request.proof?.proof.case !== "passphrase") throw new Error("missing passphrase");
    expect(new TextDecoder().decode(request.proof.proof.value.utf8)).toBe(
      "workspace-passphrase-long-enough",
    );
    return unlockCodec.encodeResponse(create(UnlockWorkspaceResponseSchema, { workspace }));
  });
  const signIn = vi.fn(async (frame: Uint8Array) => {
    const request = signInCodec.decodeRequest(frame);
    expect(request.username).toBe("admin@tammy.local");
    expect(new TextDecoder().decode(request.password?.utf8)).toBe(
      "administrator-password-long-enough",
    );
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
  const api = {
    createWorkspace: vi.fn(),
    confirmRecovery: vi.fn(),
    unlockWorkspace,
    signIn,
    createOrganisation: vi.fn(),
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
    getReportingCapability: vi.fn(),
    getAttentionSummary: vi.fn(),
    getCurrentUser: vi.fn(async (frame: Uint8Array) => {
      const request = currentUserCodec.decodeRequest(frame);
      expect(request.authentication?.actorUserId).toBe("01900f3c-7b2e-7cc4-98c4-dc0c0c073992");
      return currentUserCodec.encodeResponse(
        create(GetCurrentUserResponseSchema, {
          user: create(UserSchema, {
            id: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
            username: "admin@tammy.local",
            roles: [Role.WORKSPACE_ADMIN],
          }),
        }),
      );
    }),
    enrolTotp: vi.fn(),
    confirmTotp: vi.fn(),
    assertTotp: vi.fn(),
    getOrganisation: vi.fn(async (frame: Uint8Array) => {
      const request = organisationCodec.decodeRequest(frame);
      expect(request.organisationId).toBe("01900f3c-7b2e-7cc4-98c4-dc0c0c073994");
      return organisationCodec.encodeResponse(
        create(GetOrganisationResponseSchema, {
          organisation: create(OrganisationSchema, {
            id: request.organisationId,
            displayName: "Tammy Business",
            legalName: "Tammy Business Pty Ltd",
            abn: "51824753556",
          }),
        }),
      );
    }),
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

  const organisationId = "01900f3c-7b2e-7cc4-98c4-dc0c0c073994";
  render(
    <UnlockScreen api={api} onAuthenticated={onAuthenticated} organisationId={organisationId} />,
  );
  await user.type(
    screen.getByLabelText("Workspace passphrase"),
    "workspace-passphrase-long-enough",
  );
  await user.type(screen.getByLabelText("Email or username"), "admin@tammy.local");
  await user.type(
    screen.getByLabelText("Administrator password"),
    "administrator-password-long-enough",
  );
  await user.click(screen.getByRole("button", { name: "Unlock workspace" }));

  await vi.waitFor(() =>
    expect(onAuthenticated).toHaveBeenCalledWith({
      sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073993",
      userId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
      workspaceId: workspace.id,
      organisationId,
      organisationDisplayName: "Tammy Business",
      organisationCanonicalAbn: "51824753556",
      roles: [Role.WORKSPACE_ADMIN],
    }),
  );
  expect(unlockWorkspace).toHaveBeenCalledOnce();
  expect(signIn).toHaveBeenCalledOnce();
});
