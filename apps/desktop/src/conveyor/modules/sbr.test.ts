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
