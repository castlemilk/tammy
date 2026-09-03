import { create } from "@bufbuild/protobuf";
import { ConveyorError, type StandardSchemaV1 } from "electron-conveyor/define";
import { z } from "zod";
import { GetMachineCredentialStatusRequestSchema, GetMachineCredentialStatusResponseSchema, GetSbrReadinessRequestSchema, GetSbrReadinessResponseSchema, ImportMachineCredentialRequestSchema, ImportMachineCredentialResponseSchema, ImportSbrProductIdRequestSchema, ImportSbrProductIdResponseSchema, RemoveMachineCredentialRequestSchema, RemoveMachineCredentialResponseSchema, RemoveSbrProductIdRequestSchema, RemoveSbrProductIdResponseSchema, ReplaceMachineCredentialRequestSchema, ReplaceMachineCredentialResponseSchema, RunSbrReadinessFixtureRequestSchema, RunSbrReadinessFixtureResponseSchema, UnlockMachineCredentialRequestSchema, UnlockMachineCredentialResponseSchema } from "@tammy/connect-client/tammy/v1/sbr_pb.js";
import { isBoundedUtf8String, type MachineCredentialFileSelection } from "../../shared/conveyor-contract";
import { createProtoMethodCodec } from "../../shared/proto-ipc";
import { command, defineModule, query } from "../init";
import { boundedProtoFrame, conveyorProtoFrame, invokeCore } from "../proto";
const zeroInput = z.undefined() as unknown as StandardSchemaV1<void, void>;

const UUID_V7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const textEncoder = new TextEncoder();
const readiness = createProtoMethodCodec({ input: GetSbrReadinessRequestSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 32_768, output: GetSbrReadinessResponseSchema });
const credentialStatus = createProtoMethodCodec({ input: GetMachineCredentialStatusRequestSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 32_768, output: GetMachineCredentialStatusResponseSchema });
const importCredential = createProtoMethodCodec({ input: ImportMachineCredentialRequestSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 32_768, output: ImportMachineCredentialResponseSchema });
const replaceCredential = createProtoMethodCodec({ input: ReplaceMachineCredentialRequestSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 32_768, output: ReplaceMachineCredentialResponseSchema });
const unlockCredential = createProtoMethodCodec({ input: UnlockMachineCredentialRequestSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 32_768, output: UnlockMachineCredentialResponseSchema });
const removeCredential = createProtoMethodCodec({ input: RemoveMachineCredentialRequestSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 32_768, output: RemoveMachineCredentialResponseSchema });
const importProductId = createProtoMethodCodec({ input: ImportSbrProductIdRequestSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 32_768, output: ImportSbrProductIdResponseSchema });
const removeProductId = createProtoMethodCodec({ input: RemoveSbrProductIdRequestSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 32_768, output: RemoveSbrProductIdResponseSchema });
const readinessFixture = createProtoMethodCodec({ input: RunSbrReadinessFixtureRequestSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 32_768, output: RunSbrReadinessFixtureResponseSchema });

type CredentialMutationInput = { command: Uint8Array; handle: string; password: string };
type CredentialUnlockInput = { command: Uint8Array; password: string };
type ProductIdInput = { command: Uint8Array; productId: string };
const credentialMutationInput = z.object({
  command: boundedProtoFrame(8_192),
  handle: z.string().regex(UUID_V7, "Invalid credential handle"),
  password: z.string().refine((value) => isBoundedUtf8String(value, 1_024), "Invalid password"),
}).strict() as unknown as StandardSchemaV1<CredentialMutationInput, CredentialMutationInput>;
const credentialUnlockInput = z.object({
  command: boundedProtoFrame(8_192),
  password: z.string().refine((value) => isBoundedUtf8String(value, 1_024), "Invalid password"),
}).strict() as unknown as StandardSchemaV1<CredentialUnlockInput, CredentialUnlockInput>;
const productIdInput = z.object({
  command: boundedProtoFrame(8_192),
  productId: z.string().refine((value) => isBoundedUtf8String(value, 1_024), "Invalid product identifier"),
}).strict() as unknown as StandardSchemaV1<ProductIdInput, ProductIdInput>;

function validateSelection(value: MachineCredentialFileSelection): MachineCredentialFileSelection {
  if (Object.keys(value).length === 1 && value.selected === false) return Object.freeze({ selected: false });
  if (Object.keys(value).length === 2 && value.selected === true && typeof value.handle === "string" && UUID_V7.test(value.handle)) return Object.freeze({ selected: true, handle: value.handle });
  throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
}

function validateTrustedFile(value: Readonly<{ selectedLocalPath: string; securityScopedBookmark?: Uint8Array }>): Readonly<{ selectedLocalPath: string; securityScopedBookmark?: Uint8Array }> {
  try {
    if (typeof value.selectedLocalPath !== "string" || !value.selectedLocalPath.startsWith("/") || textEncoder.encode(value.selectedLocalPath).byteLength > 4_096) throw new Error();
    const bookmark = value.securityScopedBookmark;
    if (bookmark === undefined) return Object.freeze({ selectedLocalPath: value.selectedLocalPath });
    if (Object.getPrototypeOf(bookmark) !== Uint8Array.prototype || bookmark.byteLength === 0 || bookmark.byteLength > 65_536) throw new Error();
    return Object.freeze({ selectedLocalPath: value.selectedLocalPath, securityScopedBookmark: new Uint8Array(bookmark) });
  } catch {
    throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
  }
}

async function enrichCredential<Request, Response>(options: {
  readonly call: (request: Request) => Promise<Response>;
  readonly codec: { decodeRequest(frame: Uint8Array): Request; encodeResponse(response: Response): Uint8Array };
  readonly createRequest: (request: Request, file: Readonly<{ selectedLocalPath: string; securityScopedBookmark?: Uint8Array }>, password: Uint8Array) => Request;
  readonly fileIntake: { consumeMachineCredentialFile(handle: string): Promise<Readonly<{ selectedLocalPath: string; securityScopedBookmark?: Uint8Array }>> };
  readonly input: { command: Uint8Array; handle: string; password: string };
  readonly validateRequest: (request: Request) => boolean;
}): Promise<Uint8Array> {
  let decoded: Request;
  try { decoded = options.codec.decodeRequest(new Uint8Array(options.input.command)); } catch { throw new ConveyorError("INVALID_RPC_REQUEST", "INVALID_RPC_REQUEST"); }
  if (!options.validateRequest(decoded)) throw new ConveyorError("INVALID_RPC_REQUEST", "INVALID_RPC_REQUEST");
  const password = textEncoder.encode(options.input.password);
  try {
    const file = validateTrustedFile(await options.fileIntake.consumeMachineCredentialFile(options.input.handle));
    return new Uint8Array(options.codec.encodeResponse(await options.call(options.createRequest(decoded, file, password))));
  } catch { throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED"); } finally { password.fill(0); }
}

export const sbr = defineModule({
  getSbrReadiness: query(conveyorProtoFrame(8_192), ({ input, ctx }) => invokeCore(readiness, input, ctx.client.getSbrReadiness)),
  getMachineCredentialStatus: query(conveyorProtoFrame(8_192), ({ input, ctx }) => invokeCore(credentialStatus, input, ctx.client.getMachineCredentialStatus)),
  selectMachineCredentialFile: command(zeroInput, async ({ ctx }) => {
    try { return validateSelection(await ctx.fileIntake.selectMachineCredentialFile()); } catch { throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED"); }
  }),
  importMachineCredential: command(credentialMutationInput, ({ input, ctx }) => enrichCredential({
    call: ctx.client.importMachineCredential, codec: importCredential, fileIntake: ctx.fileIntake, input,
    validateRequest: (request) => request.selectedLocalPath === "" && request.securityScopedBookmark === undefined && request.password.byteLength === 0,
    createRequest: (request, file, password) => {
      return create(ImportMachineCredentialRequestSchema, { commandContext: request.commandContext, selectedLocalPath: file.selectedLocalPath, securityScopedBookmark: file.securityScopedBookmark, password });
    },
  })),
  replaceMachineCredential: command(credentialMutationInput, ({ input, ctx }) => enrichCredential({
    call: ctx.client.replaceMachineCredential, codec: replaceCredential, fileIntake: ctx.fileIntake, input,
    validateRequest: (request) => request.selectedLocalPath === "" && request.securityScopedBookmark === undefined && request.password.byteLength === 0,
    createRequest: (request, file, password) => {
      return create(ReplaceMachineCredentialRequestSchema, { commandContext: request.commandContext, selectedLocalPath: file.selectedLocalPath, securityScopedBookmark: file.securityScopedBookmark, password });
    },
  })),
  unlockMachineCredential: command(credentialUnlockInput, async ({ input, ctx }) => {
    let request;
    try { request = unlockCredential.decodeRequest(input.command); } catch { throw new ConveyorError("INVALID_RPC_REQUEST", "INVALID_RPC_REQUEST"); }
    if (request.password.byteLength !== 0) throw new ConveyorError("INVALID_RPC_REQUEST", "INVALID_RPC_REQUEST");
    const password = textEncoder.encode(input.password);
    try {
      return new Uint8Array(unlockCredential.encodeResponse(await ctx.client.unlockMachineCredential(create(UnlockMachineCredentialRequestSchema, { commandContext: request.commandContext, password }))));
    } catch {
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    } finally {
      password.fill(0);
    }
  }),
  removeMachineCredential: command(conveyorProtoFrame(8_192), ({ input, ctx }) => invokeCore(removeCredential, input, ctx.client.removeMachineCredential)),
  importSbrProductId: command(productIdInput, async ({ input, ctx }) => {
    let request;
    try { request = importProductId.decodeRequest(input.command); } catch { throw new ConveyorError("INVALID_RPC_REQUEST", "INVALID_RPC_REQUEST"); }
    if (request.productIdValue !== "") throw new ConveyorError("INVALID_RPC_REQUEST", "INVALID_RPC_REQUEST");
    try {
      return new Uint8Array(importProductId.encodeResponse(await ctx.client.importSbrProductId(create(ImportSbrProductIdRequestSchema, { commandContext: request.commandContext, productIdValue: input.productId, evteProductIdentifier: request.evteProductIdentifier, evteServiceIdentifier: request.evteServiceIdentifier }))));
    } catch {
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    }
  }),
  removeSbrProductId: command(conveyorProtoFrame(8_192), ({ input, ctx }) => invokeCore(removeProductId, input, ctx.client.removeSbrProductId)),
  runSbrReadinessFixture: command(conveyorProtoFrame(8_192), ({ input, ctx }) => invokeCore(readinessFixture, input, ctx.client.runSbrReadinessFixture)),
});
