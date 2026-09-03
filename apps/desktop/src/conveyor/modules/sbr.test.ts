import { create, type DescMessage, type Message } from "@bufbuild/protobuf";
import {
  AuthenticationContextSchema,
  CommandContextSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import * as sbrProto from "@tammy/connect-client/tammy/v1/sbr_pb.js";
import { createCaller } from "electron-conveyor/main";
import { expect, test, vi } from "vitest";

import type { ProtoMethodCodec } from "../../shared/proto-ipc";

interface CodecConfig {
  readonly input: DescMessage;
  readonly maximumRequestBytes: number;
  readonly maximumResponseBytes: number;
  readonly output: DescMessage;
}

interface CapturedCodec {
  readonly codec: ProtoMethodCodec<DescMessage, DescMessage>;
  readonly config: CodecConfig;
}

const codecCapture = vi.hoisted(() => ({ entries: [] as CapturedCodec[] }));

vi.mock("electron", () => ({ app: { isPackaged: true } }));
vi.mock("../../shared/proto-ipc", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../shared/proto-ipc")>();
  return {
    ...actual,
    createProtoMethodCodec: (config: CodecConfig) => {
      const codec = actual.createProtoMethodCodec(config);
      codecCapture.entries.push({
        codec: codec as unknown as ProtoMethodCodec<DescMessage, DescMessage>,
        config,
      });
      return codec;
    },
  };
});

import { ProtoIpcError } from "../../shared/proto-ipc";
import { createTammyRouter } from "../router";

const HANDLE = "018fcb44-a5b7-7b8c-8123-123456789abc";
const REPLACEMENT_HANDLE = "018fcb44-a5b7-7b8c-9123-123456789abd";
const ACTOR_ID = "018fcb44-a5b7-7b8c-a123-123456789abe";
const SESSION_ID = "018fcb44-a5b7-7b8c-b123-123456789abf";
const IDEMPOTENCY_KEY = "018fcb44-a5b7-7b8d-8123-123456789abc";
const REQUEST_BYTES = 8_192;
const RESPONSE_BYTES = 32_768;
const COMMAND_CONTEXT = create(CommandContextSchema, {
  authentication: create(AuthenticationContextSchema, {
    actorUserId: ACTOR_ID,
    sessionId: SESSION_ID,
  }),
  idempotencyKey: IDEMPOTENCY_KEY,
});

type SbrInputKind = "credential" | "frame" | "product" | "unlock";

interface SbrCase {
  readonly coreMethod: string;
  readonly input: DescMessage;
  readonly inputKind: SbrInputKind;
  readonly method: string;
  readonly output: DescMessage;
}

const SBR_CASES = [
  {
    method: "getSbrReadiness",
    coreMethod: "getSbrReadiness",
    inputKind: "frame",
    input: sbrProto.GetSbrReadinessRequestSchema,
    output: sbrProto.GetSbrReadinessResponseSchema,
  },
  {
    method: "getMachineCredentialStatus",
    coreMethod: "getMachineCredentialStatus",
    inputKind: "frame",
    input: sbrProto.GetMachineCredentialStatusRequestSchema,
    output: sbrProto.GetMachineCredentialStatusResponseSchema,
  },
  {
    method: "importMachineCredential",
    coreMethod: "importMachineCredential",
    inputKind: "credential",
    input: sbrProto.ImportMachineCredentialRequestSchema,
    output: sbrProto.ImportMachineCredentialResponseSchema,
  },
  {
    method: "replaceMachineCredential",
    coreMethod: "replaceMachineCredential",
    inputKind: "credential",
    input: sbrProto.ReplaceMachineCredentialRequestSchema,
    output: sbrProto.ReplaceMachineCredentialResponseSchema,
  },
  {
    method: "unlockMachineCredential",
    coreMethod: "unlockMachineCredential",
    inputKind: "unlock",
    input: sbrProto.UnlockMachineCredentialRequestSchema,
    output: sbrProto.UnlockMachineCredentialResponseSchema,
  },
  {
    method: "removeMachineCredential",
    coreMethod: "removeMachineCredential",
    inputKind: "frame",
    input: sbrProto.RemoveMachineCredentialRequestSchema,
    output: sbrProto.RemoveMachineCredentialResponseSchema,
  },
  {
    method: "importSbrProductId",
    coreMethod: "importSbrProductId",
    inputKind: "product",
    input: sbrProto.ImportSbrProductIdRequestSchema,
    output: sbrProto.ImportSbrProductIdResponseSchema,
  },
  {
    method: "removeSbrProductId",
    coreMethod: "removeSbrProductId",
    inputKind: "frame",
    input: sbrProto.RemoveSbrProductIdRequestSchema,
    output: sbrProto.RemoveSbrProductIdResponseSchema,
  },
  {
    method: "runSbrReadinessFixture",
    coreMethod: "runSbrReadinessFixture",
    inputKind: "frame",
    input: sbrProto.RunSbrReadinessFixtureRequestSchema,
    output: sbrProto.RunSbrReadinessFixtureResponseSchema,
  },
] as const satisfies readonly SbrCase[];

type Procedure = (...input: unknown[]) => Promise<unknown>;

function procedure(caller: unknown, method: string): Procedure {
  const result = (caller as { sbr: Record<string, Procedure> }).sbr[method];
  if (result === undefined) throw new Error(`Missing SBR procedure: ${method}`);
  return result;
}

function testCaseFor(method: string): SbrCase {
  const result = SBR_CASES.find((candidate) => candidate.method === method);
  if (result === undefined) throw new Error(`Missing SBR test case: ${method}`);
  return result;
}

function capturedCodec(testCase: SbrCase): CapturedCodec {
  const matches = codecCapture.entries.filter(({ config }) => config.input === testCase.input);
  expect(matches, `sbr.${testCase.method} codec capture`).toHaveLength(1);
  const match = matches[0];
  if (match === undefined) throw new Error(`Missing SBR codec: ${testCase.method}`);
  return match;
}

function canonicalRequest(testCase: SbrCase): Message {
  switch (testCase.method) {
    case "getSbrReadiness":
      return create(sbrProto.GetSbrReadinessRequestSchema, {
        authentication: COMMAND_CONTEXT.authentication,
      });
    case "getMachineCredentialStatus":
      return create(sbrProto.GetMachineCredentialStatusRequestSchema, {
        authentication: COMMAND_CONTEXT.authentication,
      });
    case "importMachineCredential":
      return create(sbrProto.ImportMachineCredentialRequestSchema, {
        commandContext: COMMAND_CONTEXT,
      });
    case "replaceMachineCredential":
      return create(sbrProto.ReplaceMachineCredentialRequestSchema, {
        commandContext: COMMAND_CONTEXT,
      });
    case "unlockMachineCredential":
      return create(sbrProto.UnlockMachineCredentialRequestSchema, {
        commandContext: COMMAND_CONTEXT,
      });
    case "removeMachineCredential":
      return create(sbrProto.RemoveMachineCredentialRequestSchema, {
        commandContext: COMMAND_CONTEXT,
      });
    case "importSbrProductId":
      return create(sbrProto.ImportSbrProductIdRequestSchema, {
        commandContext: COMMAND_CONTEXT,
        evteProductIdentifier: "EVTE-PRODUCT",
        evteServiceIdentifier: "EVTE-SERVICE",
      });
    case "removeSbrProductId":
      return create(sbrProto.RemoveSbrProductIdRequestSchema, {
        commandContext: COMMAND_CONTEXT,
        evteProductIdentifier: "EVTE-PRODUCT",
        evteServiceIdentifier: "EVTE-SERVICE",
      });
    case "runSbrReadinessFixture":
      return create(sbrProto.RunSbrReadinessFixtureRequestSchema, {
        commandContext: COMMAND_CONTEXT,
        fixtureId: "SIM-SBR-READINESS-V1",
      });
    default:
      throw new Error(`Unhandled SBR test case: ${testCase.method}`);
  }
}

function validInput(testCase: SbrCase): unknown {
  const command = capturedCodec(testCase).codec.encodeRequest(canonicalRequest(testCase));
  switch (testCase.inputKind) {
    case "frame":
      return command;
    case "credential":
      return {
        command,
        handle: testCase.method === "replaceMachineCredential" ? REPLACEMENT_HANDLE : HANDLE,
        password: `${testCase.method}-password`,
      };
    case "unlock":
      return { command, password: "unlock-password" };
    case "product":
      return { command, productId: "protected-product-id" };
  }
}

function boundaryInput(testCase: SbrCase, length: number): unknown {
  const command = new Uint8Array(length);
  switch (testCase.inputKind) {
    case "frame":
      return command;
    case "credential":
      return { command, handle: HANDLE, password: "password" };
    case "unlock":
      return { command, password: "password" };
    case "product":
      return { command, productId: "product-id" };
  }
}

async function expectPublicError(
  invocation: Promise<unknown>,
  code: string,
  forbidden: RegExp = /private|password=|bookmark|secret/i,
): Promise<void> {
  let caught: unknown;
  try {
    await invocation;
  } catch (error) {
    caught = error;
  }
  expect(caught).toMatchObject({ code });
  if (code !== "INVALID_INPUT") expect(caught).toMatchObject({ message: code });
  expect(JSON.stringify(caught)).not.toMatch(forbidden);
}

function expectProtoError(action: () => unknown, code: string): void {
  let caught: unknown;
  try {
    action();
  } catch (error) {
    caught = error;
  }
  expect(caught).toBeInstanceOf(ProtoIpcError);
  expect(caught).toMatchObject({ code, message: code });
}

function inertIntake() {
  return {
    clear: vi.fn(),
    consumeMachineCredentialFile: vi.fn(async () => ({
      selectedLocalPath: "/private/machine-credential.p12",
    })),
    selectMachineCredentialFile: vi.fn(async () => ({ selected: false as const })),
  };
}

test("all real SBR protobuf codecs use the exact production schemas and limits", () => {
  for (const testCase of SBR_CASES) {
    const captured = capturedCodec(testCase);
    expect(captured.config.input).toBe(testCase.input);
    expect(captured.config.output).toBe(testCase.output);
    expect(captured.config.maximumRequestBytes).toBe(REQUEST_BYTES);
    expect(captured.config.maximumResponseBytes).toBe(RESPONSE_BYTES);
  }

  const inputNames: ReadonlySet<string> = new Set(SBR_CASES.map(({ input }) => input.typeName));
  expect(
    codecCapture.entries.filter(({ config }) => inputNames.has(config.input.typeName)),
  ).toHaveLength(SBR_CASES.length);
});

test("all ten public SBR procedures use file intake or their one exact Core method and decode every framed response", async () => {
  const calls: Array<{ readonly method: string; readonly request: Message }> = [];
  const responses: ReadonlyMap<string, Message> = new Map(
    SBR_CASES.map(({ coreMethod, output }) => [coreMethod, create(output)]),
  );
  const client = new Proxy(
    {},
    {
      get: (_target, property) => async (request: Message) => {
        const method = String(property);
        calls.push({ method, request });
        const response = responses.get(method);
        if (response === undefined) throw new Error("unexpected Core method");
        return response;
      },
    },
  );
  const intake = inertIntake();
  const caller = createCaller(createTammyRouter(), {
    ctx: { client: client as never, fileIntake: intake },
  });

  await expect(caller.sbr.selectMachineCredentialFile()).resolves.toEqual({ selected: false });
  for (const testCase of SBR_CASES) {
    const response = await procedure(caller, testCase.method)(validInput(testCase));
    expect(response).toBeInstanceOf(Uint8Array);
    expect(capturedCodec(testCase).codec.decodeResponse(response as Uint8Array)).toEqual(
      create(testCase.output),
    );
  }

  expect(intake.selectMachineCredentialFile).toHaveBeenCalledExactlyOnceWith();
  expect(intake.consumeMachineCredentialFile.mock.calls).toEqual([[HANDLE], [REPLACEMENT_HANDLE]]);
  expect(calls.map(({ method }) => method)).toEqual(SBR_CASES.map(({ coreMethod }) => coreMethod));
  expect(calls).toHaveLength(SBR_CASES.length);
  for (const [index, call] of calls.entries()) {
    const testCase = SBR_CASES[index];
    if (testCase === undefined) throw new Error("Missing SBR call case");
    expect(call.request.$typeName).toBe(testCase.input.typeName);
    if (testCase.inputKind === "frame") {
      expect(call.request).toEqual(canonicalRequest(testCase));
    }
  }
});

test("every SBR codec enforces its exact request and response byte boundary before Core or intake", async () => {
  const coreCalls: string[] = [];
  const client = new Proxy(
    {},
    {
      get: (_target, property) => async () => {
        coreCalls.push(String(property));
        throw new Error("invalid frames must not reach Core");
      },
    },
  );
  const intake = inertIntake();
  const caller = createCaller(createTammyRouter(), {
    ctx: { client: client as never, fileIntake: intake },
  });

  for (const testCase of SBR_CASES) {
    await expectPublicError(
      procedure(caller, testCase.method)(boundaryInput(testCase, REQUEST_BYTES)),
      "INVALID_RPC_REQUEST",
    );
    await expectPublicError(
      procedure(caller, testCase.method)(boundaryInput(testCase, REQUEST_BYTES + 1)),
      "INVALID_INPUT",
    );
    const { codec } = capturedCodec(testCase);
    expectProtoError(
      () => codec.decodeResponse(new Uint8Array(RESPONSE_BYTES)),
      "PROTO_FRAME_INVALID",
    );
    expectProtoError(
      () => codec.decodeResponse(new Uint8Array(RESPONSE_BYTES + 1)),
      "PROTO_FRAME_TOO_LARGE",
    );
  }

  expect(coreCalls).toEqual([]);
  expect(intake.consumeMachineCredentialFile).not.toHaveBeenCalled();
  expect(intake.selectMachineCredentialFile).not.toHaveBeenCalled();
});

test("credential selection accepts only the two exact renderer-safe shapes and has true zero-input arity", async () => {
  const selectMachineCredentialFile = vi
    .fn()
    .mockResolvedValueOnce({ selected: false })
    .mockResolvedValueOnce({ selected: true, handle: HANDLE });
  const caller = createCaller(createTammyRouter(), {
    ctx: {
      client: new Proxy(
        {},
        {
          get: () => {
            throw new Error("selection must not access Core");
          },
        },
      ) as never,
      fileIntake: {
        clear: vi.fn(),
        consumeMachineCredentialFile: vi.fn(),
        selectMachineCredentialFile,
      },
    },
  });

  const canceled = await caller.sbr.selectMachineCredentialFile();
  const selected = await caller.sbr.selectMachineCredentialFile();
  expect(canceled).toEqual({ selected: false });
  expect(selected).toEqual({ selected: true, handle: HANDLE });
  expect(Object.isFrozen(canceled)).toBe(true);
  expect(Object.isFrozen(selected)).toBe(true);
  await expectPublicError(
    procedure(caller, "selectMachineCredentialFile")("unexpected"),
    "INVALID_INPUT",
  );
  expect(selectMachineCredentialFile).toHaveBeenCalledTimes(2);
});

test("credential selection rejects invalid UUIDs and every path, bookmark, or extra field", async () => {
  const invalidSelections: readonly unknown[] = [
    { selected: true, handle: "not-a-uuid" },
    { selected: true, handle: HANDLE, extra: true },
    { selected: true, path: "/private/credential.p12" },
    { selected: true, handle: HANDLE, path: "/private/credential.p12" },
    { selected: true, handle: HANDLE, bookmark: "secret-bookmark" },
    { selected: false, path: "/private/credential.p12" },
  ];

  for (const invalidSelection of invalidSelections) {
    const selectMachineCredentialFile = vi.fn(async () => invalidSelection);
    const caller = createCaller(createTammyRouter(), {
      ctx: {
        client: new Proxy(
          {},
          {
            get: () => {
              throw new Error("selection must not access Core");
            },
          },
        ) as never,
        fileIntake: {
          clear: vi.fn(),
          consumeMachineCredentialFile: vi.fn(),
          selectMachineCredentialFile,
        } as never,
      },
    });

    await expectPublicError(caller.sbr.selectMachineCredentialFile(), "CORE_REQUEST_FAILED");
    expect(selectMachineCredentialFile).toHaveBeenCalledExactlyOnceWith();
  }
});

test("import and replace preserve command context, consume each handle once per invocation, and reject reuse", async () => {
  const usedHandles = new Set<string>();
  const bookmarks = new Map([
    [HANDLE, Uint8Array.of(1, 2, 3)],
    [REPLACEMENT_HANDLE, Uint8Array.of(4, 5, 6)],
  ]);
  const consumeMachineCredentialFile = vi.fn(async (handle: string) => {
    if (usedHandles.has(handle)) throw new Error(`/private/reused/${handle} password=secret`);
    usedHandles.add(handle);
    return {
      selectedLocalPath: `/private/${handle}.p12`,
      securityScopedBookmark: bookmarks.get(handle),
    };
  });
  const received: Array<{
    readonly bookmark: Uint8Array | undefined;
    readonly commandContext: unknown;
    readonly method: string;
    readonly passwordAtCall: Uint8Array;
    readonly passwordReference: Uint8Array;
    readonly selectedLocalPath: string;
  }> = [];
  const client = {
    importMachineCredential: async (request: sbrProto.ImportMachineCredentialRequest) => {
      received.push({
        bookmark: request.securityScopedBookmark,
        commandContext: request.commandContext,
        method: "importMachineCredential",
        passwordAtCall: new Uint8Array(request.password),
        passwordReference: request.password,
        selectedLocalPath: request.selectedLocalPath,
      });
      return create(sbrProto.ImportMachineCredentialResponseSchema);
    },
    replaceMachineCredential: async (request: sbrProto.ReplaceMachineCredentialRequest) => {
      received.push({
        bookmark: request.securityScopedBookmark,
        commandContext: request.commandContext,
        method: "replaceMachineCredential",
        passwordAtCall: new Uint8Array(request.password),
        passwordReference: request.password,
        selectedLocalPath: request.selectedLocalPath,
      });
      return create(sbrProto.ReplaceMachineCredentialResponseSchema);
    },
  };
  const intake = {
    clear: vi.fn(),
    consumeMachineCredentialFile,
    selectMachineCredentialFile: vi.fn(),
  };
  const caller = createCaller(createTammyRouter(), {
    ctx: { client: client as never, fileIntake: intake as never },
  });

  for (const method of ["importMachineCredential", "replaceMachineCredential"] as const) {
    const testCase = testCaseFor(method);
    const input = validInput(testCase);
    const response = await procedure(caller, method)(input);
    expect(capturedCodec(testCase).codec.decodeResponse(response as Uint8Array)).toEqual(
      create(testCase.output),
    );
    await expectPublicError(procedure(caller, method)(input), "CORE_REQUEST_FAILED");
  }

  expect(consumeMachineCredentialFile.mock.calls).toEqual([
    [HANDLE],
    [HANDLE],
    [REPLACEMENT_HANDLE],
    [REPLACEMENT_HANDLE],
  ]);
  expect(received.map(({ method }) => method)).toEqual([
    "importMachineCredential",
    "replaceMachineCredential",
  ]);
  for (const record of received) {
    const expectedHandle =
      record.method === "importMachineCredential" ? HANDLE : REPLACEMENT_HANDLE;
    expect(record.commandContext).toEqual(COMMAND_CONTEXT);
    expect(record.selectedLocalPath).toBe(`/private/${expectedHandle}.p12`);
    expect(Array.from(record.bookmark ?? [])).toEqual(
      Array.from(bookmarks.get(expectedHandle) ?? []),
    );
    expect(Array.from(record.passwordAtCall)).toEqual(
      Array.from(new TextEncoder().encode(`${record.method}-password`)),
    );
    expect(Array.from(record.passwordReference)).toEqual(
      Array.from(new Uint8Array(record.passwordAtCall.byteLength)),
    );
  }
});

test("renderer-populated credential fields are rejected before file intake and Core", async () => {
  const coreCalls: string[] = [];
  const client = new Proxy(
    {},
    {
      get: (_target, property) => async () => {
        coreCalls.push(String(property));
        throw new Error("hostile renderer fields reached Core");
      },
    },
  );
  const intake = inertIntake();
  const caller = createCaller(createTammyRouter(), {
    ctx: { client: client as never, fileIntake: intake },
  });

  for (const method of ["importMachineCredential", "replaceMachineCredential"] as const) {
    const testCase = testCaseFor(method);
    const { codec } = capturedCodec(testCase);
    for (const hostile of [
      { selectedLocalPath: "/private/renderer-controlled.p12" },
      { securityScopedBookmark: Uint8Array.of(9, 8, 7) },
      { password: new TextEncoder().encode("renderer-password") },
    ]) {
      const request =
        method === "importMachineCredential"
          ? create(sbrProto.ImportMachineCredentialRequestSchema, {
              commandContext: COMMAND_CONTEXT,
              ...hostile,
            })
          : create(sbrProto.ReplaceMachineCredentialRequestSchema, {
              commandContext: COMMAND_CONTEXT,
              ...hostile,
            });
      await expectPublicError(
        procedure(
          caller,
          method,
        )({
          command: codec.encodeRequest(request),
          handle: HANDLE,
          password: "trusted-main-password",
        }),
        "INVALID_RPC_REQUEST",
      );
    }
  }

  const unlockCase = testCaseFor("unlockMachineCredential");
  await expectPublicError(
    procedure(
      caller,
      unlockCase.method,
    )({
      command: capturedCodec(unlockCase).codec.encodeRequest(
        create(sbrProto.UnlockMachineCredentialRequestSchema, {
          commandContext: COMMAND_CONTEXT,
          password: new TextEncoder().encode("renderer-password"),
        }),
      ),
      password: "trusted-main-password",
    }),
    "INVALID_RPC_REQUEST",
  );

  expect(intake.consumeMachineCredentialFile).not.toHaveBeenCalled();
  expect(coreCalls).toEqual([]);
});

test("every mediated SBR input is exact and rejects extra object keys before intake or Core", async () => {
  const coreCalls: string[] = [];
  const client = new Proxy(
    {},
    {
      get: (_target, property) => async () => {
        coreCalls.push(String(property));
        throw new Error("extra input reached Core");
      },
    },
  );
  const intake = inertIntake();
  const caller = createCaller(createTammyRouter(), {
    ctx: { client: client as never, fileIntake: intake },
  });

  for (const method of [
    "importMachineCredential",
    "replaceMachineCredential",
    "unlockMachineCredential",
    "importSbrProductId",
  ]) {
    const input = validInput(testCaseFor(method)) as Record<string, unknown>;
    await expectPublicError(
      procedure(
        caller,
        method,
      )({
        ...input,
        rendererSelectedLocalPath: "/private/hostile.p12",
      }),
      "INVALID_INPUT",
    );
  }

  expect(intake.consumeMachineCredentialFile).not.toHaveBeenCalled();
  expect(coreCalls).toEqual([]);
});

test("passwords and Product IDs accept exact 1024-byte UTF-8 and reject byte overflow or invalid surrogates", async () => {
  const exactUtf8 = "🙂".repeat(256);
  const passwordSnapshots: Uint8Array[] = [];
  const productValues: string[] = [];
  const client = {
    unlockMachineCredential: async (request: sbrProto.UnlockMachineCredentialRequest) => {
      passwordSnapshots.push(new Uint8Array(request.password));
      return create(sbrProto.UnlockMachineCredentialResponseSchema);
    },
    importSbrProductId: async (request: sbrProto.ImportSbrProductIdRequest) => {
      productValues.push(request.productIdValue);
      return create(sbrProto.ImportSbrProductIdResponseSchema);
    },
  };
  const intake = inertIntake();
  const caller = createCaller(createTammyRouter(), {
    ctx: { client: client as never, fileIntake: intake },
  });
  const unlockCase = testCaseFor("unlockMachineCredential");
  const productCase = testCaseFor("importSbrProductId");
  const unlockCommand = capturedCodec(unlockCase).codec.encodeRequest(canonicalRequest(unlockCase));
  const productCommand = capturedCodec(productCase).codec.encodeRequest(
    canonicalRequest(productCase),
  );

  const unlockResponse = await procedure(
    caller,
    unlockCase.method,
  )({
    command: unlockCommand,
    password: exactUtf8,
  });
  const productResponse = await procedure(
    caller,
    productCase.method,
  )({
    command: productCommand,
    productId: exactUtf8,
  });
  expect(capturedCodec(unlockCase).codec.decodeResponse(unlockResponse as Uint8Array)).toEqual(
    create(unlockCase.output),
  );
  expect(capturedCodec(productCase).codec.decodeResponse(productResponse as Uint8Array)).toEqual(
    create(productCase.output),
  );
  expect(passwordSnapshots.map((value) => Array.from(value))).toEqual([
    Array.from(new TextEncoder().encode(exactUtf8)),
  ]);
  expect(productValues).toEqual([exactUtf8]);

  for (const invalid of [`${exactUtf8}a`, "\ud800", "\udc00"]) {
    await expectPublicError(
      procedure(caller, unlockCase.method)({ command: unlockCommand, password: invalid }),
      "INVALID_INPUT",
      /private|bookmark|secret/i,
    );
    await expectPublicError(
      procedure(caller, productCase.method)({ command: productCommand, productId: invalid }),
      "INVALID_INPUT",
      /private|bookmark|secret/i,
    );
  }
  expect(passwordSnapshots).toHaveLength(1);
  expect(productValues).toHaveLength(1);
  expect(intake.consumeMachineCredentialFile).not.toHaveBeenCalled();
});

test.each([
  ["importMachineCredential", "success"],
  ["importMachineCredential", "sync throw"],
  ["importMachineCredential", "async reject"],
  ["replaceMachineCredential", "success"],
  ["replaceMachineCredential", "sync throw"],
  ["replaceMachineCredential", "async reject"],
  ["unlockMachineCredential", "success"],
  ["unlockMachineCredential", "sync throw"],
  ["unlockMachineCredential", "async reject"],
] as const)("%s wipes its transient password after %s", async (method, outcome) => {
  const testCase = testCaseFor(method);
  const password = `sëcret-${method}`;
  let passwordAtCall: Uint8Array | undefined;
  let passwordReference: Uint8Array | undefined;
  const coreCall = (request: { readonly password: Uint8Array }) => {
    passwordAtCall = new Uint8Array(request.password);
    passwordReference = request.password;
    const failure = new Error(`/private/${method}.p12 password=${password} bookmark=secret`);
    if (outcome === "sync throw") throw failure;
    if (outcome === "async reject") return Promise.reject(failure);
    return Promise.resolve(create(testCase.output));
  };
  const intake = inertIntake();
  const caller = createCaller(createTammyRouter(), {
    ctx: {
      client: { [testCase.coreMethod]: coreCall } as never,
      fileIntake: intake,
    },
  });
  const command = capturedCodec(testCase).codec.encodeRequest(canonicalRequest(testCase));
  const input =
    testCase.inputKind === "credential"
      ? { command, handle: HANDLE, password }
      : { command, password };

  if (outcome === "success") {
    const response = await procedure(caller, method)(input);
    expect(capturedCodec(testCase).codec.decodeResponse(response as Uint8Array)).toEqual(
      create(testCase.output),
    );
  } else {
    await expectPublicError(procedure(caller, method)(input), "CORE_REQUEST_FAILED");
  }

  expect(Array.from(passwordAtCall ?? [])).toEqual(Array.from(new TextEncoder().encode(password)));
  expect(Array.from(passwordReference ?? [])).toEqual(
    Array.from(new Uint8Array(passwordAtCall?.byteLength ?? 0)),
  );
  if (testCase.inputKind === "credential") {
    expect(intake.consumeMachineCredentialFile).toHaveBeenCalledExactlyOnceWith(HANDLE);
  } else {
    expect(intake.consumeMachineCredentialFile).not.toHaveBeenCalled();
  }
});

test("Product ID import requires an empty renderer field and preserves trusted scope during main enrichment", async () => {
  let received: sbrProto.ImportSbrProductIdRequest | undefined;
  const client = {
    importSbrProductId: async (request: sbrProto.ImportSbrProductIdRequest) => {
      received = request;
      return create(sbrProto.ImportSbrProductIdResponseSchema);
    },
  };
  const intake = inertIntake();
  const caller = createCaller(createTammyRouter(), {
    ctx: { client: client as never, fileIntake: intake },
  });
  const testCase = testCaseFor("importSbrProductId");
  const { codec } = capturedCodec(testCase);
  const response = await procedure(
    caller,
    testCase.method,
  )({
    command: codec.encodeRequest(canonicalRequest(testCase)),
    productId: "main-supplied-product-id",
  });

  expect(codec.decodeResponse(response as Uint8Array)).toEqual(create(testCase.output));
  expect(received).toMatchObject({
    commandContext: COMMAND_CONTEXT,
    evteProductIdentifier: "EVTE-PRODUCT",
    evteServiceIdentifier: "EVTE-SERVICE",
    productIdValue: "main-supplied-product-id",
  });

  await expectPublicError(
    procedure(
      caller,
      testCase.method,
    )({
      command: codec.encodeRequest(
        create(sbrProto.ImportSbrProductIdRequestSchema, {
          commandContext: COMMAND_CONTEXT,
          evteProductIdentifier: "EVTE-PRODUCT",
          evteServiceIdentifier: "EVTE-SERVICE",
          productIdValue: "renderer-controlled-secret",
        }),
      ),
      productId: "main-supplied-product-id",
    }),
    "INVALID_RPC_REQUEST",
  );
  expect(received?.productIdValue).toBe("main-supplied-product-id");
  expect(intake.consumeMachineCredentialFile).not.toHaveBeenCalled();
});

test("trusted bookmarks reach Core as exact canonical copied bytes even if the intake value mutates", async () => {
  const bookmark = Uint8Array.of(0, 255, 1, 254);
  let beforeMutation: Uint8Array | undefined;
  let afterMutation: Uint8Array | undefined;
  let receivedReference: Uint8Array | undefined;
  const client = {
    importMachineCredential: async (request: sbrProto.ImportMachineCredentialRequest) => {
      const receivedBookmark = request.securityScopedBookmark;
      if (receivedBookmark === undefined) throw new Error("missing test bookmark");
      receivedReference = receivedBookmark;
      beforeMutation = new Uint8Array(receivedBookmark);
      bookmark.fill(9);
      afterMutation = new Uint8Array(receivedBookmark);
      return create(sbrProto.ImportMachineCredentialResponseSchema);
    },
  };
  const consumeMachineCredentialFile = vi.fn(async () => ({
    securityScopedBookmark: bookmark,
    selectedLocalPath: "/private/canonical.p12",
  }));
  const caller = createCaller(createTammyRouter(), {
    ctx: {
      client: client as never,
      fileIntake: {
        clear: vi.fn(),
        consumeMachineCredentialFile,
        selectMachineCredentialFile: vi.fn(),
      },
    },
  });
  const testCase = testCaseFor("importMachineCredential");
  const response = await procedure(caller, testCase.method)(validInput(testCase));

  expect(capturedCodec(testCase).codec.decodeResponse(response as Uint8Array)).toEqual(
    create(testCase.output),
  );
  expect(Array.from(beforeMutation ?? [])).toEqual([0, 255, 1, 254]);
  expect(Array.from(afterMutation ?? [])).toEqual([0, 255, 1, 254]);
  expect(receivedReference).not.toBe(bookmark);
  expect(receivedReference).toBeInstanceOf(Uint8Array);
  expect(
    receivedReference === undefined ? undefined : Object.getPrototypeOf(receivedReference),
  ).toBe(Uint8Array.prototype);
  expect(consumeMachineCredentialFile).toHaveBeenCalledExactlyOnceWith(HANDLE);
});

test("trusted bookmark validation rejects subclasses, DataViews, detached, empty, and oversized values before Core", async () => {
  class BookmarkSubclass extends Uint8Array {}
  const detached = Uint8Array.of(1);
  structuredClone(detached, { transfer: [detached.buffer] });
  const invalidBookmarks: readonly unknown[] = [
    new BookmarkSubclass([1]),
    new DataView(new ArrayBuffer(1)),
    detached,
    new Uint8Array(),
    new Uint8Array(65_537),
  ];

  for (const invalidBookmark of invalidBookmarks) {
    const importMachineCredential = vi.fn();
    const consumeMachineCredentialFile = vi.fn(async () => ({
      securityScopedBookmark: invalidBookmark,
      selectedLocalPath: "/private/credential.p12",
    }));
    const caller = createCaller(createTammyRouter(), {
      ctx: {
        client: { importMachineCredential } as never,
        fileIntake: {
          clear: vi.fn(),
          consumeMachineCredentialFile,
          selectMachineCredentialFile: vi.fn(),
        } as never,
      },
    });
    const testCase = testCaseFor("importMachineCredential");

    await expectPublicError(
      procedure(caller, testCase.method)(validInput(testCase)),
      "CORE_REQUEST_FAILED",
    );
    expect(consumeMachineCredentialFile).toHaveBeenCalledExactlyOnceWith(HANDLE);
    expect(importMachineCredential).not.toHaveBeenCalled();
  }
});

test.each(["sync throw", "async reject", "response encode"] as const)(
  "SBR %s failures expose only the generic Core code",
  async (outcome) => {
    const secret = "/private/credential.p12 password=hunter2 bookmark=secret subprocess output";
    const testCase = testCaseFor("getSbrReadiness");
    const response = new Proxy(create(sbrProto.GetSbrReadinessResponseSchema), {
      get: (_target, property) => {
        if (property === "then") return undefined;
        throw new Error(secret);
      },
    });
    const coreCall =
      outcome === "sync throw"
        ? () => {
            throw new Error(secret);
          }
        : outcome === "async reject"
          ? async () => {
              throw new Error(secret);
            }
          : () => response;
    const intake = inertIntake();
    const caller = createCaller(createTammyRouter(), {
      ctx: {
        client: { getSbrReadiness: coreCall } as never,
        fileIntake: intake,
      },
    });

    await expectPublicError(
      procedure(caller, testCase.method)(validInput(testCase)),
      "CORE_REQUEST_FAILED",
    );
    expect(intake.consumeMachineCredentialFile).not.toHaveBeenCalled();
    expect(intake.selectMachineCredentialFile).not.toHaveBeenCalled();
  },
);
