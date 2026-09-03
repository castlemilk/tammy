import { create } from "@bufbuild/protobuf";
import { CreateWorkspaceRequestSchema, CreateWorkspaceResponseSchema } from "@tammy/connect-client/tammy/v1/workspace_pb.js";
import { createCaller } from "electron-conveyor/main";
import { expect, test, vi } from "vitest";

vi.mock("electron", () => ({ app: { isPackaged: true } }));

import manifest from "../../shared/conveyor-manifest.json";
import { createProtoMethodCodec } from "../../shared/proto-ipc";
import { createTammyRouter } from "../router";

const workspaceCodec = createProtoMethodCodec({
  input: CreateWorkspaceRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 65_536,
  output: CreateWorkspaceResponseSchema,
});

test("all ordinary domain procedures dispatch only to their matching Core method", async () => {
  const [workspace, identity, organisation, accounting, banking, documents, tax, overview, capability] = await Promise.all([
    import("@tammy/connect-client/tammy/v1/workspace_pb.js"), import("@tammy/connect-client/tammy/v1/identity_pb.js"), import("@tammy/connect-client/tammy/v1/organisation_pb.js"), import("@tammy/connect-client/tammy/v1/accounting_pb.js"), import("@tammy/connect-client/tammy/v1/banking_pb.js"), import("@tammy/connect-client/tammy/v1/documents_pb.js"), import("@tammy/connect-client/tammy/v1/tax_pb.js"), import("@tammy/connect-client/tammy/v1/overview_pb.js"), import("@tammy/connect-client/tammy/v1/reporting_capability_pb.js"),
  ]);
  const cases = [
    ["workspace", "createWorkspace", "createWorkspace", workspace, "CreateWorkspace", 32768], ["workspace", "confirmRecovery", "confirmRecovery", workspace, "ConfirmRecovery", 16384], ["workspace", "unlockWorkspace", "unlockWorkspace", workspace, "UnlockWorkspace", 16384], ["workspace", "signIn", "signIn", identity, "SignIn", 16384],
    ["organisation", "getCurrentUser", "getCurrentUser", identity, "GetCurrentUser", 8192], ["organisation", "getOrganisation", "getOrganisation", organisation, "GetOrganisation", 8192], ["organisation", "createOrganisation", "createOrganisation", organisation, "CreateOrganisation", 32768], ["organisation", "enrolTotp", "enrolTotp", identity, "EnrolTOTP", 8192], ["organisation", "confirmTotp", "confirmTotp", identity, "ConfirmTOTP", 8192], ["organisation", "assertTotp", "assertTotp", identity, "AssertTOTP", 8192], ["organisation", "recordEntityVerification", "recordEntityVerification", organisation, "RecordEntityVerification", Math.floor(1.1 * 1024 * 1024)],
    ["accounting", "listAccounts", "listAccounts", accounting, "ListAccounts", 16384], ["accounting", "listJournals", "listJournals", accounting, "ListJournals", 16384], ["accounting", "getJournal", "getJournal", accounting, "GetJournal", 8192], ["accounting", "getTrialBalance", "getTrialBalance", accounting, "GetTrialBalance", 8192], ["accounting", "createAccount", "createAccount", accounting, "CreateAccount", 32768], ["accounting", "postManualJournal", "postManualJournal", accounting, "PostManualJournal", 131072],
    ["banking", "listBankStatementLines", "listBankStatementLines", banking, "ListBankStatementLines", 16384], ["banking", "getBankingSummary", "getBankingSummary", banking, "GetBankingSummary", 16384], ["banking", "importBankStatement", "importBankStatement", banking, "ImportBankStatement", 262144], ["banking", "matchBankStatementLine", "matchBankStatementLine", banking, "MatchBankStatementLine", 16384], ["banking", "completeBankReconciliation", "completeBankReconciliation", banking, "CompleteBankReconciliation", 16384],
    ["documents", "listDocuments", "listDocuments", documents, "ListDocuments", 16384], ["documents", "getDocument", "getDocument", documents, "GetDocument", 8192], ["documents", "ingestDocument", "ingestDocument", documents, "IngestDocument", 11 * 1024 * 1024], ["documents", "saveDocumentReview", "saveDocumentReview", documents, "SaveDocumentReview", 32768],
    ["reporting", "getCurrentBasDraft", "getCurrentBasDraft", tax, "GetCurrentBasDraft", 16384], ["reporting", "getReportingCapability", "getReportingCapability", capability, "GetReportingCapability", 8192], ["reporting", "getAttentionSummary", "getAttentionSummary", overview, "GetAttentionSummary", 8192], ["reporting", "createBasDraft", "createBasDraft", tax, "CreateBasDraft", 16384],
  ] as const;
  const calls: string[] = [];
  const responses = new Map<string, any>(cases.map(([, , core, pb, name]) => [core, (pb as any)[`${name}ResponseSchema`]]));
  const client = new Proxy({}, {
    get: (_target, method) => async (request: unknown) => {
      calls.push(String(method));
      const response = responses.get(String(method));
      return response ? create(response as never) : request;
    },
  });
  const router = createTammyRouter();
  const caller = createCaller(router, { ctx: { client: client as never, fileIntake: {} as never } });
  await caller.system.getSystemDiagnostics();
  for (const [module, method, core, pb, name, limit] of cases) {
    const codec = createProtoMethodCodec({ input: (pb as any)[`${name}RequestSchema`], output: (pb as any)[`${name}ResponseSchema`], maximumRequestBytes: limit, maximumResponseBytes: 16 * 1024 * 1024 });
    const frame = codec.encodeRequest(create((pb as any)[`${name}RequestSchema`]));
    await (caller as any)[module][method](frame);
    expect(calls.at(-1)).toBe(core);
    await expect((caller as any)[module][method](new Uint8Array(limit + 1))).rejects.toMatchObject({ code: "INVALID_INPUT" });
    expect(calls.at(-1)).toBe(core);
  }
  expect(calls[0]).toBe("getDiagnostics");
  await expect((caller.system as any).getSystemDiagnostics("unexpected")).rejects.toMatchObject({ code: "INVALID_INPUT" });
  expect(vi.isMockFunction(client)).toBe(false);
  expect(router.manifest).toEqual(manifest);
});

test("system diagnostics sanitizes synchronous and asynchronous Core failures", async () => {
  const router = createTammyRouter();
  for (const getDiagnostics of [
    () => { throw new Error("/private/diagnostics password=secret"); },
    async () => { throw new Error("/private/diagnostics password=secret"); },
  ]) {
    const caller = createCaller(router, { ctx: { client: { getDiagnostics } as never, fileIntake: {} as never } });
    await expect(caller.system.getSystemDiagnostics()).rejects.toMatchObject({ code: "CORE_REQUEST_FAILED", message: "CORE_REQUEST_FAILED" });
  }
});
