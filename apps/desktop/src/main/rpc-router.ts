import type {
  GetAttentionSummaryRequest,
  GetAttentionSummaryResponse,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";
import {
  GetAttentionSummaryRequestSchema,
  GetAttentionSummaryResponseSchema,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";
import type { SignInRequest, SignInResponse } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import { SignInRequestSchema, SignInResponseSchema } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import type {
  ConfirmRecoveryRequest,
  ConfirmRecoveryResponse,
  CreateWorkspaceRequest,
  CreateWorkspaceResponse,
  UnlockWorkspaceRequest,
  UnlockWorkspaceResponse,
} from "@tammy/connect-client/tammy/v1/workspace_pb.js";
import {
  ConfirmRecoveryRequestSchema,
  ConfirmRecoveryResponseSchema,
  CreateWorkspaceRequestSchema,
  CreateWorkspaceResponseSchema,
  UnlockWorkspaceRequestSchema,
  UnlockWorkspaceResponseSchema,
} from "@tammy/connect-client/tammy/v1/workspace_pb.js";

import {
  ATTENTION_SUMMARY_CHANNEL,
  CONFIRM_RECOVERY_CHANNEL,
  CREATE_WORKSPACE_CHANNEL,
  SIGN_IN_CHANNEL,
  UNLOCK_WORKSPACE_CHANNEL,
} from "../shared/desktop-api";
import { createProtoMethodCodec, ProtoIpcError } from "../shared/proto-ipc";

export { ATTENTION_SUMMARY_CHANNEL } from "../shared/desktop-api";

const createWorkspaceCodec = createProtoMethodCodec({
  input: CreateWorkspaceRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 65_536,
  output: CreateWorkspaceResponseSchema,
});
const confirmRecoveryCodec = createProtoMethodCodec({
  input: ConfirmRecoveryRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
  output: ConfirmRecoveryResponseSchema,
});
const signInCodec = createProtoMethodCodec({
  input: SignInRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 32_768,
  output: SignInResponseSchema,
});
const unlockWorkspaceCodec = createProtoMethodCodec({
  input: UnlockWorkspaceRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
  output: UnlockWorkspaceResponseSchema,
});

const attentionCodec = createProtoMethodCodec({
  input: GetAttentionSummaryRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 65_536,
  output: GetAttentionSummaryResponseSchema,
});

export type DesktopRpcRouterErrorCode =
  | "CORE_REQUEST_FAILED"
  | "INVALID_RPC_REQUEST"
  | "UNKNOWN_RPC_CHANNEL";

export class DesktopRpcRouterError extends Error {
  public constructor(public readonly code: DesktopRpcRouterErrorCode) {
    super(code);
    this.name = "DesktopRpcRouterError";
  }
}

export interface DesktopRpcClient {
  readonly createWorkspace: (request: CreateWorkspaceRequest) => Promise<CreateWorkspaceResponse>;
  readonly confirmRecovery: (request: ConfirmRecoveryRequest) => Promise<ConfirmRecoveryResponse>;
  readonly unlockWorkspace: (request: UnlockWorkspaceRequest) => Promise<UnlockWorkspaceResponse>;
  readonly signIn: (request: SignInRequest) => Promise<SignInResponse>;
  readonly getAttentionSummary: (
    request: GetAttentionSummaryRequest,
  ) => Promise<GetAttentionSummaryResponse>;
}

export interface DesktopRpcRouter {
  invoke(channel: string, request: Uint8Array): Promise<Uint8Array>;
}

export function createDesktopRpcRouter(client: DesktopRpcClient): Readonly<DesktopRpcRouter> {
  return Object.freeze({
    invoke: async (channel: string, request: Uint8Array): Promise<Uint8Array> => {
      try {
        switch (channel) {
          case CREATE_WORKSPACE_CHANNEL:
            return createWorkspaceCodec.encodeResponse(
              await client.createWorkspace(createWorkspaceCodec.decodeRequest(request)),
            );
          case CONFIRM_RECOVERY_CHANNEL:
            return confirmRecoveryCodec.encodeResponse(
              await client.confirmRecovery(confirmRecoveryCodec.decodeRequest(request)),
            );
          case UNLOCK_WORKSPACE_CHANNEL:
            return unlockWorkspaceCodec.encodeResponse(
              await client.unlockWorkspace(unlockWorkspaceCodec.decodeRequest(request)),
            );
          case SIGN_IN_CHANNEL:
            return signInCodec.encodeResponse(await client.signIn(signInCodec.decodeRequest(request)));
          case ATTENTION_SUMMARY_CHANNEL:
            return attentionCodec.encodeResponse(
              await client.getAttentionSummary(attentionCodec.decodeRequest(request)),
            );
          default:
            throw new DesktopRpcRouterError("UNKNOWN_RPC_CHANNEL");
        }
      } catch (error) {
        if (error instanceof DesktopRpcRouterError) throw error;
        if (error instanceof ProtoIpcError) throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
        throw new DesktopRpcRouterError("CORE_REQUEST_FAILED");
      }
    },
  });
}
