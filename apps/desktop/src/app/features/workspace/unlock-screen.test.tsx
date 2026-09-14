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
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { createConveyorFixture, renderWithConveyor as render } from "../../test/conveyor-fixtures";
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
  const fixture = createConveyorFixture();
  fixture.procedures.workspace.unlockWorkspace.mockImplementation(unlockWorkspace);
  fixture.procedures.workspace.signIn.mockImplementation(signIn);
  fixture.procedures.organisation.getCurrentUser.mockImplementation(async (frame: Uint8Array) => {
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
  });
  fixture.procedures.organisation.getOrganisation.mockImplementation(async (frame: Uint8Array) => {
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
  });
  const onAuthenticated = vi.fn();
  const user = userEvent.setup();

  const organisationId = "01900f3c-7b2e-7cc4-98c4-dc0c0c073994";
  render(
    <UnlockScreen
      onAuthenticated={onAuthenticated}
      organisation={fixture.conveyor.organisation}
      organisationId={organisationId}
      workspace={fixture.conveyor.workspace}
    />,
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
