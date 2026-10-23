import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
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

const fixtureIds = {
  organisationId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073994",
  sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073993",
  userId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
  workspaceId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073991",
};

function configureSuccessfulUnlock(
  fixture: ReturnType<typeof createConveyorFixture>,
  options: { readonly mismatchedCurrentUser?: boolean } = {},
): void {
  fixture.procedures.workspace.unlockWorkspace.mockResolvedValue(
    unlockCodec.encodeResponse(
      create(UnlockWorkspaceResponseSchema, {
        workspace: create(WorkspaceSchema, { id: fixtureIds.workspaceId, version: 2n }),
      }),
    ),
  );
  fixture.procedures.workspace.signIn.mockResolvedValue(
    signInCodec.encodeResponse(
      create(SignInResponseSchema, {
        user: create(UserSchema, {
          id: fixtureIds.userId,
          username: "admin@tammy.local",
          roles: [Role.WORKSPACE_ADMIN],
        }),
        session: create(SessionSchema, { id: fixtureIds.sessionId }),
      }),
    ),
  );
  fixture.procedures.organisation.getCurrentUser.mockImplementation(async (frame: Uint8Array) => {
    currentUserCodec.decodeRequest(frame);
    return currentUserCodec.encodeResponse(
      create(GetCurrentUserResponseSchema, {
        user: create(UserSchema, {
          id: options.mismatchedCurrentUser
            ? "01900f3c-7b2e-7cc4-98c4-dc0c0c073995"
            : fixtureIds.userId,
          username: "admin@tammy.local",
          roles: [Role.WORKSPACE_ADMIN],
        }),
      }),
    );
  });
  fixture.procedures.organisation.getOrganisation.mockImplementation(async (frame: Uint8Array) => {
    const request = organisationCodec.decodeRequest(frame);
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
}

async function submitUnlock(user: ReturnType<typeof userEvent.setup>): Promise<void> {
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
}

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

it("lets current credentials at the backend's 15-code-point minimum reach unlock", async () => {
  const fixture = createConveyorFixture();
  configureSuccessfulUnlock(fixture);
  const user = userEvent.setup();
  render(
    <UnlockScreen
      onAuthenticated={vi.fn()}
      organisation={fixture.conveyor.organisation}
      organisationId={fixtureIds.organisationId}
      workspace={fixture.conveyor.workspace}
    />,
  );

  await user.type(screen.getByLabelText("Workspace passphrase"), "passphrase12345");
  await user.type(screen.getByLabelText("Email or username"), "admin@tammy.local");
  await user.type(screen.getByLabelText("Administrator password"), "adminpass123456");
  await user.click(screen.getByRole("button", { name: "Unlock workspace" }));

  expect(fixture.procedures.workspace.unlockWorkspace).toHaveBeenCalledOnce();
});

it.each([
  {
    failure: "workspace unlock",
    marker: "LOCAL_ENGINE_UNLOCK_WORKSPACE_UNLOCK_FAILED",
    operation: "unlock" as const,
    connectCode: "LOCAL_ENGINE_CONNECT_PERMISSION_DENIED",
  },
  {
    failure: "sign in",
    marker: "LOCAL_ENGINE_UNLOCK_SIGN_IN_FAILED",
    operation: "sign-in" as const,
    connectCode: "LOCAL_ENGINE_CONNECT_PERMISSION_DENIED",
  },
  {
    failure: "current-user read",
    marker: "LOCAL_ENGINE_UNLOCK_CURRENT_USER_READ_FAILED",
    operation: "current-user" as const,
    connectCode: "LOCAL_ENGINE_CONNECT_PERMISSION_DENIED",
  },
  {
    failure: "organisation read",
    marker: "LOCAL_ENGINE_UNLOCK_ORGANISATION_READ_FAILED",
    operation: "organisation" as const,
    connectCode: "LOCAL_ENGINE_CONNECT_PERMISSION_DENIED",
  },
  {
    failure: "authenticated projection validation",
    marker: "LOCAL_ENGINE_UNLOCK_PROJECTION_VALIDATION_FAILED",
    operation: "projection" as const,
    connectCode: undefined,
  },
])("retains only the fixed $failure failure stage", async ({ marker, operation, connectCode }) => {
  const fixture = createConveyorFixture();
  configureSuccessfulUnlock(fixture, {
    mismatchedCurrentUser: operation === "projection",
  });
  const privateFailure = new ConnectError(
    "private workspace path and password=secret",
    Code.PermissionDenied,
  );
  if (operation === "unlock") {
    fixture.procedures.workspace.unlockWorkspace.mockRejectedValue(privateFailure);
  } else if (operation === "sign-in") {
    fixture.procedures.workspace.signIn.mockRejectedValue(privateFailure);
  } else if (operation === "current-user") {
    fixture.procedures.organisation.getCurrentUser.mockRejectedValue(privateFailure);
  } else if (operation === "organisation") {
    fixture.procedures.organisation.getOrganisation.mockRejectedValue(privateFailure);
  }

  const onAuthenticated = vi.fn();
  const user = userEvent.setup();
  const diagnostic = vi.spyOn(console, "error").mockImplementation(() => undefined);
  try {
    render(
      <UnlockScreen
        onAuthenticated={onAuthenticated}
        organisation={fixture.conveyor.organisation}
        organisationId={fixtureIds.organisationId}
        workspace={fixture.conveyor.workspace}
      />,
    );
    await submitUnlock(user);

    expect((await screen.findByRole("alert")).textContent).toBe(
      "The workspace could not be unlocked. Check your passphrase and sign-in details.",
    );
    expect(onAuthenticated).not.toHaveBeenCalled();
    expect(diagnostic).toHaveBeenCalledWith(connectCode ? `${marker} ${connectCode}` : marker);
    const emitted = diagnostic.mock.calls.flat().join(" ");
    expect(emitted).not.toContain("private workspace path");
    expect(emitted).not.toContain("password=secret");
  } finally {
    diagnostic.mockRestore();
  }
});
