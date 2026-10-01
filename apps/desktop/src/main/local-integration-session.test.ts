// @vitest-environment node
import { create } from "@bufbuild/protobuf";
import {
  type ListAccountsResponse,
  ListAccountsResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import { AuthenticationContextSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  DocumentSchema,
  DocumentStatus,
  type GetDocumentResponse,
  GetDocumentResponseSchema,
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
    getDocument: vi.fn(async () => create(GetDocumentResponseSchema)),
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
  it("returns only reviewed expense costs without retained document bodies", async () => {
    const { core, session, signIn } = fixture();
    vi.mocked(core.listDocuments).mockResolvedValue(
      create(ListDocumentsResponseSchema, {
        documents: [
          {
            id: organisationId,
            organisationId,
            status: DocumentStatus.REVIEWED,
            extractedText: "secret body",
            candidate: {
              supplierName: "Supplier",
              invoiceNumber: "EXP-1",
              subtotal: { minorUnits: 10000n, currencyCode: "AUD" },
              gst: { minorUnits: 1000n, currencyCode: "AUD" },
              total: { minorUnits: 11000n, currencyCode: "AUD" },
            },
          },
          {
            id: actorUserId,
            organisationId,
            extractedText: "unreviewed body",
            candidate: { supplierName: "Unreviewed" },
          },
        ],
      }),
    );
    await signIn();
    const execute = await session.createReader();
    const result = await execute(read("tammy.invoices.list_cost_sources"));
    expect(result).toEqual({
      expenses: [
        {
          id: organisationId,
          description: "Supplier · EXP-1",
          subtotalMinor: 10000,
          gstMinor: 1000,
          totalMinor: 11000,
          currency: "AUD",
        },
      ],
    });
    expect(JSON.stringify(result)).not.toMatch(/secret|body|candidate|Unreviewed/);
  });
  it("cannot restore a pending proposal after local access is revoked", async () => {
    const { core, session, signIn } = fixture();
    await signIn();
    const execute = await session.createReader();
    let finish!: (value: GetDocumentResponse) => void;
    vi.mocked(core.getDocument).mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const input = validateLocalApiRequest({
      version: "v1",
      operation: "tool.call",
      name: "tammy.invoices.propose_draft",
      arguments: {
        number: "INV-001",
        issueDate: "2026-09-30",
        dueDate: "2026-10-14",
        customerName: "",
        customerAddress: "",
        sellerAddress: "",
        gstRegistered: false,
        notes: "",
        paymentInstructions: "",
        lines: [
          {
            description: "Expense",
            quantityMilli: 1000,
            unitPriceMinor: 10000,
            taxTreatment: "out-of-scope",
            sourceDocumentId: organisationId,
          },
        ],
      },
    });
    if (!input.ok) throw new Error(input.error.code);
    const pending = execute(input.value);
    session.clearInvoiceProposal();
    finish(
      create(GetDocumentResponseSchema, {
        document: { id: organisationId, organisationId, status: DocumentStatus.REVIEWED },
      }),
    );
    await expect(pending).rejects.toThrow("LOCAL_PAIRING_REVOKED");
    expect(session.getInvoiceProposal(authentication, organisationId)).toBeUndefined();
  });
  it("stages an MCP draft for native review without saving and clears it on session change", async () => {
    const { session, signIn } = fixture();
    await signIn();
    const execute = await session.createReader();
    const draft = {
      number: "INV-001",
      issueDate: "2026-09-30",
      dueDate: "2026-10-14",
      customerName: "Buyer",
      customerAddress: "Melbourne",
      sellerAddress: "Mooroolbark",
      gstRegistered: true,
      notes: "",
      paymentInstructions: "",
      lines: [
        {
          description: "Services",
          quantityMilli: 1000,
          unitPriceMinor: 10000,
          unitCostMinor: 5000,
          taxTreatment: "taxable",
        },
      ],
    };
    const input = validateLocalApiRequest({
      version: "v1",
      operation: "tool.call",
      name: "tammy.invoices.propose_draft",
      arguments: draft,
    });
    if (!input.ok) throw new Error(input.error.code);
    await expect(execute(input.value)).resolves.toMatchObject({
      staged: true,
      reviewRequired: true,
      totals: { totalMinor: 11000 },
    });
    expect(() =>
      session.getInvoiceProposal({ actorUserId: organisationId, sessionId }, organisationId),
    ).toThrow("LOCAL_SESSION_REQUIRED");
    expect(session.getInvoiceProposal(authentication, organisationId)).toEqual(draft);
    session.clear();
    await signIn();
    expect(session.getInvoiceProposal(authentication, organisationId)).toBeUndefined();
  });
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
