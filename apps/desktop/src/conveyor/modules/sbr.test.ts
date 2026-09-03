import { create } from "@bufbuild/protobuf";
import { ImportMachineCredentialRequestSchema, ImportMachineCredentialResponseSchema } from "@tammy/connect-client/tammy/v1/sbr_pb.js";
import { createCaller } from "electron-conveyor/main";
import { expect, test, vi } from "vitest";

vi.mock("electron", () => ({ app: { isPackaged: true } }));

import { createProtoMethodCodec } from "../../shared/proto-ipc";
import { createTammyRouter } from "../router";

const codec = createProtoMethodCodec({
  input: ImportMachineCredentialRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: ImportMachineCredentialResponseSchema,
});

test("SBR selection exposes only a handle and main alone enriches a consumed command", async () => {
  const handle = "018fcb44-a5b7-7b8c-8123-123456789abc";
  const intake = {
    clear: vi.fn(),
    consumeMachineCredentialFile: vi.fn(async () => ({ selectedLocalPath: "/private/credential.p12" })),
    selectMachineCredentialFile: vi.fn(async () => ({ selected: true as const, handle })),
  };
  const received: unknown[] = [];
  const client = new Proxy({}, { get: (_target, method) => async (request: unknown) => {
    received.push({ method, request });
    return create(ImportMachineCredentialResponseSchema);
  } });
  const router = createTammyRouter();
  const caller = createCaller(router, { ctx: { client: client as never, fileIntake: intake } });
  const frame = codec.encodeRequest(create(ImportMachineCredentialRequestSchema));

  await expect(caller.sbr.selectMachineCredentialFile()).resolves.toEqual({ selected: true, handle });
  await caller.sbr.importMachineCredential({ command: frame, handle, password: "correct horse" });
  expect(intake.consumeMachineCredentialFile).toHaveBeenCalledWith(handle);
  expect(JSON.stringify(received)).toContain("selectedLocalPath");
  expect(JSON.stringify(await caller.sbr.selectMachineCredentialFile())).not.toContain("private");
});

test("SBR synchronous failures sanitize and wipe encoded unlock passwords", async () => {
  const passwordBytes: Uint8Array[] = [];
  const client = {
    unlockMachineCredential: (request: { password: Uint8Array }) => {
      passwordBytes.push(request.password);
      throw new Error("password=secret /private/key");
    },
  };
  const router = createTammyRouter();
  const caller = createCaller(router, { ctx: { client: client as never, fileIntake: {} as never } });
  const unlock = await import("@tammy/connect-client/tammy/v1/sbr_pb.js");
  const unlockCodec = createProtoMethodCodec({ input: unlock.UnlockMachineCredentialRequestSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 32_768, output: unlock.UnlockMachineCredentialResponseSchema });
  const frame = unlockCodec.encodeRequest(create(unlock.UnlockMachineCredentialRequestSchema));
  await expect(caller.sbr.unlockMachineCredential({ command: frame, password: "secret" })).rejects.toMatchObject({ code: "CORE_REQUEST_FAILED", message: "CORE_REQUEST_FAILED" });
  expect(passwordBytes).toHaveLength(1);
  expect([...passwordBytes[0]!]).toEqual([0, 0, 0, 0, 0, 0]);
});

test("every SBR procedure reaches only its exact Core method", async () => {
  const pb = await import("@tammy/connect-client/tammy/v1/sbr_pb.js");
  const handle = "018fcb44-a5b7-7b8c-8123-123456789abc";
  const calls: string[] = [];
  const methods = [
    ["getSbrReadiness", "getSbrReadiness", "GetSbrReadiness"], ["getMachineCredentialStatus", "getMachineCredentialStatus", "GetMachineCredentialStatus"], ["removeMachineCredential", "removeMachineCredential", "RemoveMachineCredential"], ["removeSbrProductId", "removeSbrProductId", "RemoveSbrProductId"], ["runSbrReadinessFixture", "runSbrReadinessFixture", "RunSbrReadinessFixture"],
  ] as const;
  const client = new Proxy({}, { get: (_target, method) => async () => {
    calls.push(String(method));
    const item = methods.find(([, core]) => core === method);
    return create((pb as any)[`${item?.[2]}ResponseSchema`]);
  } });
  const intake = { clear() {}, selectMachineCredentialFile: async () => ({ selected: false as const }), consumeMachineCredentialFile: async () => ({ selectedLocalPath: "/private/a.p12" }) };
  const caller = createCaller(createTammyRouter(), { ctx: { client: client as never, fileIntake: intake } });
  for (const [method, core, name] of methods) {
    const codec = createProtoMethodCodec({ input: (pb as any)[`${name}RequestSchema`], output: (pb as any)[`${name}ResponseSchema`], maximumRequestBytes: 8192, maximumResponseBytes: 32768 });
    await (caller.sbr as any)[method](codec.encodeRequest(create((pb as any)[`${name}RequestSchema`])));
    expect(calls.at(-1)).toBe(core);
  }
});

test("SBR replace and Product ID import are mediated through main-only inputs", async () => {
  const pb = await import("@tammy/connect-client/tammy/v1/sbr_pb.js");
  const handle = "018fcb44-a5b7-7b8c-8123-123456789abc";
  const received: Array<{ method: string; request: any }> = [];
  const client = {
    replaceMachineCredential: async (request: any) => {
      received.push({ method: "replaceMachineCredential", request });
      return create(pb.ReplaceMachineCredentialResponseSchema);
    },
    importSbrProductId: async (request: any) => {
      received.push({ method: "importSbrProductId", request });
      return create(pb.ImportSbrProductIdResponseSchema);
    },
  };
  const bookmark = new Uint8Array([1, 2, 3]);
  const intake = {
    clear() {}, selectMachineCredentialFile: async () => ({ selected: false as const }),
    consumeMachineCredentialFile: vi.fn(async () => ({ selectedLocalPath: "/private/replace.p12", securityScopedBookmark: bookmark })),
  };
  const caller = createCaller(createTammyRouter(), { ctx: { client: client as never, fileIntake: intake } });
  const replace = createProtoMethodCodec({ input: pb.ReplaceMachineCredentialRequestSchema, output: pb.ReplaceMachineCredentialResponseSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 32_768 });
  const product = createProtoMethodCodec({ input: pb.ImportSbrProductIdRequestSchema, output: pb.ImportSbrProductIdResponseSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 32_768 });
  await caller.sbr.replaceMachineCredential({ command: replace.encodeRequest(create(pb.ReplaceMachineCredentialRequestSchema)), handle, password: "replace-password" });
  await caller.sbr.importSbrProductId({ command: product.encodeRequest(create(pb.ImportSbrProductIdRequestSchema)), productId: "product-id" });
  expect(received.map(({ method }) => method)).toEqual(["replaceMachineCredential", "importSbrProductId"]);
  expect(received[0]?.request).toMatchObject({ selectedLocalPath: "/private/replace.p12" });
  expect(received[0]?.request.securityScopedBookmark).not.toBe(bookmark);
  expect([...received[0]?.request.password ?? []]).toEqual([0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
  expect(received[1]?.request.productIdValue).toBe("product-id");
});
