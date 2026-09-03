import { AssertTOTPRequestSchema, AssertTOTPResponseSchema, ConfirmTOTPRequestSchema, ConfirmTOTPResponseSchema, EnrolTOTPRequestSchema, EnrolTOTPResponseSchema, GetCurrentUserRequestSchema, GetCurrentUserResponseSchema } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import { CreateOrganisationRequestSchema, CreateOrganisationResponseSchema, GetOrganisationRequestSchema, GetOrganisationResponseSchema, RecordEntityVerificationRequestSchema, RecordEntityVerificationResponseSchema } from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import type { StandardSchemaV1 } from "electron-conveyor/define";
import { createProtoMethodCodec } from "../../shared/proto-ipc";
import { command, defineModule, query } from "../init";
import { boundedProtoFrame, invokeCore } from "../proto";

const conveyorProtoFrame = (maximumBytes: number): StandardSchemaV1<Uint8Array, Uint8Array> => boundedProtoFrame(maximumBytes) as unknown as StandardSchemaV1<Uint8Array, Uint8Array>;

const currentUser = createProtoMethodCodec({ input: GetCurrentUserRequestSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 32_768, output: GetCurrentUserResponseSchema });
const organisation = createProtoMethodCodec({ input: GetOrganisationRequestSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 32_768, output: GetOrganisationResponseSchema });
const createOrganisation = createProtoMethodCodec({ input: CreateOrganisationRequestSchema, maximumRequestBytes: 32_768, maximumResponseBytes: 32_768, output: CreateOrganisationResponseSchema });
const enrolTotp = createProtoMethodCodec({ input: EnrolTOTPRequestSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 32_768, output: EnrolTOTPResponseSchema });
const confirmTotp = createProtoMethodCodec({ input: ConfirmTOTPRequestSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 32_768, output: ConfirmTOTPResponseSchema });
const assertTotp = createProtoMethodCodec({ input: AssertTOTPRequestSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 32_768, output: AssertTOTPResponseSchema });
const recordVerification = createProtoMethodCodec({ input: RecordEntityVerificationRequestSchema, maximumRequestBytes: Math.floor(1.1 * 1024 * 1024), maximumResponseBytes: 32_768, output: RecordEntityVerificationResponseSchema });

export const organisationModule = defineModule({
  getCurrentUser: query(conveyorProtoFrame(8_192), ({ input, ctx }) => invokeCore(currentUser, input, ctx.client.getCurrentUser)),
  getOrganisation: query(conveyorProtoFrame(8_192), ({ input, ctx }) => invokeCore(organisation, input, ctx.client.getOrganisation)),
  createOrganisation: command(conveyorProtoFrame(32_768), ({ input, ctx }) => invokeCore(createOrganisation, input, ctx.client.createOrganisation)),
  enrolTotp: command(conveyorProtoFrame(8_192), ({ input, ctx }) => invokeCore(enrolTotp, input, ctx.client.enrolTotp)),
  confirmTotp: command(conveyorProtoFrame(8_192), ({ input, ctx }) => invokeCore(confirmTotp, input, ctx.client.confirmTotp)),
  assertTotp: command(conveyorProtoFrame(8_192), ({ input, ctx }) => invokeCore(assertTotp, input, ctx.client.assertTotp)),
  recordEntityVerification: command(conveyorProtoFrame(Math.floor(1.1 * 1024 * 1024)), ({ input, ctx }) => invokeCore(recordVerification, input, ctx.client.recordEntityVerification)),
});
