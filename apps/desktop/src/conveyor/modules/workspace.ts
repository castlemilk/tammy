import {
  SignInRequestSchema,
  SignInResponseSchema,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  ConfirmRecoveryRequestSchema,
  ConfirmRecoveryResponseSchema,
  CreateWorkspaceRequestSchema,
  CreateWorkspaceResponseSchema,
  UnlockWorkspaceRequestSchema,
  UnlockWorkspaceResponseSchema,
} from "@tammy/connect-client/tammy/v1/workspace_pb.js";
import { safeWorkspaceCreateFailure } from "../../main/startup-diagnostics";
import { createProtoMethodCodec } from "../../shared/proto-ipc";
import { command, defineModule } from "../init";
import { conveyorProtoFrame, invokeCore } from "../proto";

const createWorkspace = createProtoMethodCodec({
  input: CreateWorkspaceRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 65_536,
  output: CreateWorkspaceResponseSchema,
});
const confirmRecovery = createProtoMethodCodec({
  input: ConfirmRecoveryRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
  output: ConfirmRecoveryResponseSchema,
});
const unlockWorkspace = createProtoMethodCodec({
  input: UnlockWorkspaceRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
  output: UnlockWorkspaceResponseSchema,
});
const signIn = createProtoMethodCodec({
  input: SignInRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 32_768,
  output: SignInResponseSchema,
});

export const workspace = defineModule({
  createWorkspace: command(conveyorProtoFrame(32_768), ({ input, ctx }) =>
    invokeCore(createWorkspace, input, async (request) => {
      const startedAt = performance.now();
      try {
        return await ctx.client.createWorkspace(request);
      } catch (error) {
        console.error(safeWorkspaceCreateFailure(error, performance.now() - startedAt));
        throw error;
      }
    }),
  ),
  confirmRecovery: command(conveyorProtoFrame(16_384), ({ input, ctx }) =>
    invokeCore(confirmRecovery, input, ctx.client.confirmRecovery),
  ),
  unlockWorkspace: command(conveyorProtoFrame(16_384), ({ input, ctx }) =>
    invokeCore(unlockWorkspace, input, ctx.client.unlockWorkspace),
  ),
  signIn: command(conveyorProtoFrame(16_384), ({ input, ctx }) =>
    invokeCore(signIn, input, ctx.client.signIn),
  ),
});
