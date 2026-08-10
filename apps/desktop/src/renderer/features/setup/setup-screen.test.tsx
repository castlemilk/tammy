import { create } from "@bufbuild/protobuf";
import { OneTimeSecretOutputSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
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

it("creates, confirms, and signs in to a real local workspace through named protobuf methods", async () => {
  const workspace = create(WorkspaceSchema, {
    id: "01900f3c-7b2e-7cc4-98c4-dc0c0c073991",
    version: 1n,
  });
  const recovery = new TextEncoder().encode("ABCD-EFGH-IJKL-MNOP-QRST-UVWX-YZ23-4567-ABCD-EFGH-IJKL-MNOP-QRST");
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
        user: create(UserSchema, { id: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992", username: request.username }),
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
    getAttentionSummary: vi.fn(),
    getSystemDiagnostics: vi.fn(),
  } satisfies TammyDesktopAPI;
  const onAuthenticated = vi.fn();
  const user = userEvent.setup();

  render(<SetupScreen api={api} onAuthenticated={onAuthenticated} />);
  await user.type(screen.getByLabelText("Your name"), "Tammy Admin");
  await user.type(screen.getByLabelText("Email or username"), "admin@tammy.local");
  await user.type(screen.getByLabelText("Business legal name"), "Tammy Business Pty Ltd");
  await user.type(screen.getByLabelText("Business display name"), "Tammy Business");
  await user.type(screen.getByLabelText("ABN"), "51824753556");
  await user.type(screen.getByLabelText("Workspace passphrase"), "workspace-passphrase-long-enough");
  await user.type(screen.getByLabelText("Administrator password"), "administrator-password-long-enough");
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
    }),
  );
});
