import { create } from "@bufbuild/protobuf";
import { SessionSchema, SignInRequestSchema, SignInResponseSchema, UserSchema } from "@tammy/connect-client/tammy/v1/identity_pb.js";
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
    getAttentionSummary: vi.fn(),
    getSystemDiagnostics: vi.fn(),
  } satisfies TammyDesktopAPI;
  const onAuthenticated = vi.fn();
  const user = userEvent.setup();

  const organisationId = "01900f3c-7b2e-7cc4-98c4-dc0c0c073994";
  render(
    <UnlockScreen
      api={api}
      onAuthenticated={onAuthenticated}
      organisationId={organisationId}
    />,
  );
  await user.type(screen.getByLabelText("Workspace passphrase"), "workspace-passphrase-long-enough");
  await user.type(screen.getByLabelText("Email or username"), "admin@tammy.local");
  await user.type(screen.getByLabelText("Administrator password"), "administrator-password-long-enough");
  await user.click(screen.getByRole("button", { name: "Unlock workspace" }));

  await vi.waitFor(() =>
    expect(onAuthenticated).toHaveBeenCalledWith({
      sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073993",
      userId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
      workspaceId: workspace.id,
      organisationId,
    }),
  );
  expect(unlockWorkspace).toHaveBeenCalledOnce();
  expect(signIn).toHaveBeenCalledOnce();
});
