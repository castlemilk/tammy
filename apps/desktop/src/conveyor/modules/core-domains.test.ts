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
  const calls: string[] = [];
  const client = new Proxy({}, {
    get: (_target, method) => async (request: unknown) => {
      calls.push(String(method));
      return method === "createWorkspace" ? create(CreateWorkspaceResponseSchema) : request;
    },
  });
  const router = createTammyRouter();
  const caller = createCaller(router, { ctx: { client: client as never, fileIntake: {} as never } });
  const request = workspaceCodec.encodeRequest(create(CreateWorkspaceRequestSchema));

  await caller.system.getSystemDiagnostics();
  await caller.workspace.createWorkspace(request);

  expect(calls).toEqual(["getDiagnostics", "createWorkspace"]);
  expect(vi.isMockFunction(client)).toBe(false);
  expect(router.manifest).toEqual(manifest);
});
