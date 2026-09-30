// @vitest-environment node
import { create } from "@bufbuild/protobuf";
import {
  type ListAccountsResponse,
  ListAccountsResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import { AuthenticationContextSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  DocumentSchema,
  ListDocumentsResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import {
  GetCurrentUserResponseSchema,
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
import { UnlockWorkspaceRequestSchema } from "@tammy/connect-client/tammy/v1/workspace_pb.js";
import { describe, expect, it, vi } from "vitest";
import { validateLocalApiRequest } from "../../../../packages/local-api/src/request.ts";
import type { CoreClient } from "./core-client";
import { createLocalIntegrationSession } from "./local-integration-session";

const actorUserId = "01900000-0000-7000-8000-000000000001";
const sessionId = "01900000-0000-7000-8000-000000000002";
const organisationId = "01900000-0000-7000-8000-000000000003";
const authentication = create(AuthenticationContextSchema, { actorUserId, sessionId });
const user = create(UserSchema, { id: actorUserId, username: "owner", displayName: "Owner" });
const organisation = create(OrganisationSchema, {
  id: organisationId,
  legalName: "Example Pty Ltd",
});

function read(name: string) {
  const request = validateLocalApiRequest({ version: "v1", operation: "tool.call", name });
  if (!request.ok) throw new Error(request.error.code);
  return request.value;
}

function fixture() {
  const core = {
    signIn: vi.fn(async () =>
      create(SignInResponseSchema, {
        user,
        session: create(SessionSchema, { id: sessionId, userId: actorUserId }),
      }),
    ),
    unlockWorkspace: vi.fn(async () => ({})),
    getCurrentUser: vi.fn(async () => create(GetCurrentUserResponseSchema, { user })),
    getOrganisation: vi.fn(async () => create(GetOrganisationResponseSchema, { organisation })),
    listAccounts: vi.fn(async () => create(ListAccountsResponseSchema, { accounts: [] })),
    listDocuments: vi.fn(async () =>
      create(ListDocumentsResponseSchema, {
        documents: [
          create(DocumentSchema, {
            id: organisationId,
            sourceDisplayName: "invoice.pdf",
            extractedText: "private invoice body",
          }),
        ],
      }),
    ),
  } as unknown as CoreClient;
  const invalidated = vi.fn();
  const session = createLocalIntegrationSession(core, invalidated);
  const signIn = async () => {
    await session.client.signIn(create(SignInRequestSchema));
    await session.client.getOrganisation(
      create(GetOrganisationRequestSchema, { authentication, organisationId }),
    );
  };
  return { core, invalidated, session, signIn };
}

describe("local integration session", () => {
  it("requires a native app sign-in and verified organisation before pairing", async () => {
    const { session } = fixture();
    await expect(session.createReader()).rejects.toThrow("LOCAL_SESSION_REQUIRED");
    await session.client.signIn(create(SignInRequestSchema));
    await expect(session.createReader()).rejects.toThrow("LOCAL_SESSION_REQUIRED");
  });

  it("derives authentication and organisation internally for API reads", async () => {
    const { core, session, signIn } = fixture();
    await signIn();
    const execute = await session.createReader();
    expect(await execute(read("tammy.accounting.list_accounts"))).toEqual({});
    expect(core.listAccounts).toHaveBeenCalledWith(
      expect.objectContaining({
        authentication,
        organisationId,
        page: expect.objectContaining({ pageSize: 200 }),
      }),
    );
    expect(core.getCurrentUser).toHaveBeenCalledWith(expect.objectContaining({ authentication }));
  });

  it("projects document metadata and excludes extracted document bodies", async () => {
    const { session, signIn } = fixture();
    await signIn();
    const execute = await session.createReader();
    const result = await execute(read("tammy.documents.list_metadata"));
    expect(result).toMatchObject({
      documents: [{ id: organisationId, sourceDisplayName: "invoice.pdf" }],
    });
    expect(JSON.stringify(result)).not.toContain("private invoice body");
    expect(JSON.stringify(result)).not.toContain("extractedText");
  });

  it("revokes an existing reader on a new sign-in or workspace unlock", async () => {
    const { session, signIn, invalidated } = fixture();
    await signIn();
    const execute = await session.createReader();
    await session.client.signIn(create(SignInRequestSchema));
    await expect(execute(read("tammy.accounting.list_accounts"))).rejects.toThrow(
      "LOCAL_PAIRING_REVOKED",
    );
    await session.client.unlockWorkspace(create(UnlockWorkspaceRequestSchema));
    await expect(session.createReader()).rejects.toThrow("LOCAL_SESSION_REQUIRED");
    expect(invalidated).toHaveBeenCalled();
  });

  it("does not retain an organisation returned for a different actor", async () => {
    const { session } = fixture();
    await session.client.signIn(create(SignInRequestSchema));
    await session.client.getOrganisation(
      create(GetOrganisationRequestSchema, {
        authentication: { actorUserId: organisationId, sessionId },
        organisationId,
      }),
    );
    await expect(session.createReader()).rejects.toThrow("LOCAL_SESSION_REQUIRED");
  });

  it("rejects a read that finishes after revocation", async () => {
    const { core, session, signIn } = fixture();
    await signIn();
    const execute = await session.createReader();
    let complete!: (value: ListAccountsResponse) => void;
    vi.mocked(core.listAccounts).mockImplementation(
      () =>
        new Promise((resolve) => {
          complete = resolve;
        }),
    );
    const pending = execute(read("tammy.accounting.list_accounts"));
    session.clear();
    complete(create(ListAccountsResponseSchema));
    await expect(pending).rejects.toThrow("LOCAL_PAIRING_REVOKED");
  });
});
