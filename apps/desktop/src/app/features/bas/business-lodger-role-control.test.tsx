import { create } from "@bufbuild/protobuf";
import {
  AssignRolesRequestSchema,
  AssignRolesResponseSchema,
  GetCurrentUserRequestSchema,
  GetCurrentUserResponseSchema,
  Role,
  type User,
  UserSchema,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";

import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { createConveyorFixture, renderWithConveyor as render } from "../../test/conveyor-fixtures";
import { BASLodgerRoleControl } from "./business-lodger-role-control";

const currentUserCodec = createProtoMethodCodec({
  input: GetCurrentUserRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetCurrentUserResponseSchema,
});
const assignRolesCodec = createProtoMethodCodec({
  input: AssignRolesRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: AssignRolesResponseSchema,
});
const workspace = {
  workspaceId: "018f0000-0000-7000-8000-000000000001",
  userId: "018f0000-0000-7000-8000-000000000002",
  sessionId: "018f0000-0000-7000-8000-000000000003",
  organisationId: "018f0000-0000-7000-8000-000000000004",
  roles: [Role.WORKSPACE_ADMIN],
} as const;

function currentUser(user: Pick<User, "id" | "version" | "username" | "displayName" | "roles">) {
  return currentUserCodec.encodeResponse(
    create(GetCurrentUserResponseSchema, { user: create(UserSchema, user) }),
  );
}

it("requires a live self-admin, confirms the change, and preserves every other role and version", async () => {
  const f = createConveyorFixture();
  let liveUser = create(UserSchema, {
    id: workspace.userId,
    version: 7n,
    username: "admin@tammy.local",
    displayName: "Alex Admin",
    roles: [Role.WORKSPACE_ADMIN, Role.AUDITOR],
  });
  f.procedures.organisation.getCurrentUser.mockImplementation(async (frame: Uint8Array) => {
    const request = currentUserCodec.decodeRequest(frame);
    expect(request.authentication).toMatchObject({
      actorUserId: workspace.userId,
      sessionId: workspace.sessionId,
    });
    return currentUserCodec.encodeResponse(
      create(GetCurrentUserResponseSchema, { user: liveUser }),
    );
  });
  f.procedures.organisation.assignRoles.mockImplementation(async (frame: Uint8Array) => {
    const request = assignRolesCodec.decodeRequest(frame);
    expect(request.commandContext?.authentication).toMatchObject({
      actorUserId: workspace.userId,
      sessionId: workspace.sessionId,
    });
    expect(request.commandContext?.idempotencyKey).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
    expect(request.userId).toBe(workspace.userId);
    expect(request.expectedVersion).toBe(7n);
    expect(request.roles).toEqual([Role.WORKSPACE_ADMIN, Role.BUSINESS_LODGER, Role.AUDITOR]);
    liveUser = create(UserSchema, {
      id: liveUser.id,
      version: 8n,
      username: liveUser.username,
      displayName: liveUser.displayName,
      state: liveUser.state,
      roles: request.roles,
    });
    return assignRolesCodec.encodeResponse(create(AssignRolesResponseSchema, { user: liveUser }));
  });

  render(<BASLodgerRoleControl api={f.conveyor.organisation} workspace={workspace} />);
  const user = userEvent.setup();
  const toggle = await screen.findByRole("checkbox", {
    name: /Grant business lodger approval authority/,
  });
  expect(toggle).toHaveProperty("checked", false);
  expect(f.procedures.organisation.assignRoles).not.toHaveBeenCalled();
  await user.click(toggle);
  await user.click(screen.getByRole("button", { name: "Review role change" }));
  expect(screen.getByText(/Confirm adding the business lodger role for Alex Admin/)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Confirm and save" }));
  expect(await screen.findByText("BAS approval authority updated for Alex Admin.")).toBeTruthy();
  expect(f.procedures.organisation.assignRoles).toHaveBeenCalledOnce();
  expect(f.procedures.organisation.getCurrentUser.mock.calls.length).toBeGreaterThanOrEqual(2);
});

it("hides the role editor when fresh Core roles do not include workspace admin", async () => {
  const f = createConveyorFixture();
  f.procedures.organisation.getCurrentUser.mockResolvedValue(
    currentUser({
      id: workspace.userId,
      version: 4n,
      username: "preparer@tammy.local",
      displayName: "Pat Preparer",
      roles: [Role.BUSINESS_PREPARER],
    }),
  );

  render(<BASLodgerRoleControl api={f.conveyor.organisation} workspace={workspace} />);
  await waitFor(() => expect(f.procedures.organisation.getCurrentUser).toHaveBeenCalledOnce());
  expect(
    screen.queryByRole("checkbox", { name: /Grant business lodger approval authority/ }),
  ).toBeNull();
  expect(f.procedures.organisation.assignRoles).not.toHaveBeenCalled();
});

it("hides role controls if the current-user response belongs to a different account", async () => {
  const f = createConveyorFixture();
  f.procedures.organisation.getCurrentUser.mockResolvedValue(
    currentUser({
      id: "018f0000-0000-7000-8000-000000009999",
      version: 5n,
      username: "other-admin@tammy.local",
      displayName: "Other Admin",
      roles: [Role.WORKSPACE_ADMIN],
    }),
  );

  render(<BASLodgerRoleControl api={f.conveyor.organisation} workspace={workspace} />);
  await waitFor(() => expect(f.procedures.organisation.getCurrentUser).toHaveBeenCalledOnce());
  expect(
    screen.queryByRole("checkbox", { name: /Grant business lodger approval authority/ }),
  ).toBeNull();
  expect(f.procedures.organisation.assignRoles).not.toHaveBeenCalled();
});

it("requires review when the live role version changes during confirmation", async () => {
  const f = createConveyorFixture();
  let reads = 0;
  f.procedures.organisation.getCurrentUser.mockImplementation(async () => {
    reads += 1;
    return currentUser({
      id: workspace.userId,
      version: reads === 1 ? 7n : 8n,
      username: "admin@tammy.local",
      displayName: "Alex Admin",
      roles:
        reads === 1
          ? [Role.WORKSPACE_ADMIN, Role.AUDITOR]
          : [Role.WORKSPACE_ADMIN, Role.BUSINESS_PREPARER, Role.AUDITOR],
    });
  });

  render(<BASLodgerRoleControl api={f.conveyor.organisation} workspace={workspace} />);
  const user = userEvent.setup();
  await user.click(
    await screen.findByRole("checkbox", { name: /Grant business lodger approval authority/ }),
  );
  await user.click(screen.getByRole("button", { name: "Review role change" }));
  await user.click(screen.getByRole("button", { name: "Confirm and save" }));
  expect((await screen.findByRole("alert")).textContent).toMatch(
    /Current roles changed while you reviewed/,
  );
  expect(f.procedures.organisation.assignRoles).not.toHaveBeenCalled();
});

it("retries an uncertain result with the exact protobuf request and idempotency key", async () => {
  const f = createConveyorFixture();
  const userProjection = create(UserSchema, {
    id: workspace.userId,
    version: 7n,
    username: "admin@tammy.local",
    displayName: "Alex Admin",
    roles: [Role.WORKSPACE_ADMIN],
  });
  f.procedures.organisation.getCurrentUser.mockResolvedValue(
    currentUser({
      id: userProjection.id,
      version: userProjection.version,
      username: userProjection.username,
      displayName: userProjection.displayName,
      roles: userProjection.roles,
    }),
  );
  const stored: Uint8Array[] = [];
  f.procedures.organisation.assignRoles
    .mockImplementationOnce(async (frame: Uint8Array) => {
      stored.push(new Uint8Array(frame));
      throw new Error("reply lost after commit");
    })
    .mockImplementationOnce(async (frame: Uint8Array) => {
      stored.push(new Uint8Array(frame));
      return assignRolesCodec.encodeResponse(
        create(AssignRolesResponseSchema, {
          user: create(UserSchema, {
            id: userProjection.id,
            version: 8n,
            username: userProjection.username,
            displayName: userProjection.displayName,
            state: userProjection.state,
            roles: [Role.WORKSPACE_ADMIN, Role.BUSINESS_LODGER],
          }),
        }),
      );
    });

  render(<BASLodgerRoleControl api={f.conveyor.organisation} workspace={workspace} />);
  const user = userEvent.setup();
  await user.click(
    await screen.findByRole("checkbox", { name: /Grant business lodger approval authority/ }),
  );
  await user.click(screen.getByRole("button", { name: "Review role change" }));
  await user.click(screen.getByRole("button", { name: "Confirm and save" }));
  await user.click(await screen.findByRole("button", { name: "Retry exact request" }));
  await waitFor(() => expect(f.procedures.organisation.assignRoles).toHaveBeenCalledTimes(2));
  expect(stored).toHaveLength(2);
  expect(stored[0]).toEqual(stored[1]);
});
