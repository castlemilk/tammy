import type {
  GetAttentionSummaryRequest,
  GetAttentionSummaryResponse,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";
import {
  GetAttentionSummaryRequestSchema,
  GetAttentionSummaryResponseSchema,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";

import { ATTENTION_SUMMARY_CHANNEL } from "../shared/desktop-api";
import { createProtoMethodCodec, ProtoIpcError } from "../shared/proto-ipc";

export { ATTENTION_SUMMARY_CHANNEL } from "../shared/desktop-api";

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
      if (channel !== ATTENTION_SUMMARY_CHANNEL) {
        throw new DesktopRpcRouterError("UNKNOWN_RPC_CHANNEL");
      }

      let decoded: GetAttentionSummaryRequest;
      try {
        decoded = attentionCodec.decodeRequest(request);
      } catch (error) {
        if (error instanceof ProtoIpcError) throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
        throw new DesktopRpcRouterError("INVALID_RPC_REQUEST");
      }

      try {
        const response = await client.getAttentionSummary(decoded);
        return attentionCodec.encodeResponse(response);
      } catch {
        throw new DesktopRpcRouterError("CORE_REQUEST_FAILED");
      }
    },
  });
}
