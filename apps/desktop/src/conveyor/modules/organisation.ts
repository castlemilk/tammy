import {
  AssertTOTPRequestSchema,
  AssertTOTPResponseSchema,
  AssignRolesRequestSchema,
  AssignRolesResponseSchema,
  ConfirmTOTPRequestSchema,
  ConfirmTOTPResponseSchema,
  EnrolTOTPRequestSchema,
  EnrolTOTPResponseSchema,
  GetCurrentUserRequestSchema,
  GetCurrentUserResponseSchema,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  CreateOrganisationRequestSchema,
  CreateOrganisationResponseSchema,
  GetOrganisationRequestSchema,
  GetOrganisationResponseSchema,
  RecordEntityVerificationRequestSchema,
  RecordEntityVerificationResponseSchema,
  UpdateOrganisationRequestSchema,
  UpdateOrganisationResponseSchema,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import { createProtoMethodCodec } from "../../shared/proto-ipc";
import { command, defineModule, query } from "../init";
import { conveyorProtoFrame, invokeCore } from "../proto";

const currentUser = createProtoMethodCodec({
  input: GetCurrentUserRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetCurrentUserResponseSchema,
});
const assignRoles = createProtoMethodCodec({
  input: AssignRolesRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: AssignRolesResponseSchema,
});
const organisation = createProtoMethodCodec({
  input: GetOrganisationRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetOrganisationResponseSchema,
});
const updateOrganisation = createProtoMethodCodec({
  input: UpdateOrganisationRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 32_768,
  output: UpdateOrganisationResponseSchema,
});
const createOrganisation = createProtoMethodCodec({
  input: CreateOrganisationRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 32_768,
  output: CreateOrganisationResponseSchema,
});
const enrolTotp = createProtoMethodCodec({
  input: EnrolTOTPRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: EnrolTOTPResponseSchema,
});
const confirmTotp = createProtoMethodCodec({
  input: ConfirmTOTPRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: ConfirmTOTPResponseSchema,
});
const assertTotp = createProtoMethodCodec({
  input: AssertTOTPRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: AssertTOTPResponseSchema,
});
const recordVerification = createProtoMethodCodec({
  input: RecordEntityVerificationRequestSchema,
  maximumRequestBytes: Math.floor(1.1 * 1024 * 1024),
  maximumResponseBytes: 32_768,
  output: RecordEntityVerificationResponseSchema,
});

export const organisationModule = defineModule({
  getCurrentUser: query(conveyorProtoFrame(8_192), ({ input, ctx }) =>
    invokeCore(currentUser, input, ctx.client.getCurrentUser),
  ),
  assignRoles: command(conveyorProtoFrame(8_192), ({ input, ctx }) =>
    invokeCore(assignRoles, input, ctx.client.assignRoles),
  ),
  getOrganisation: query(conveyorProtoFrame(8_192), ({ input, ctx }) =>
    invokeCore(organisation, input, ctx.client.getOrganisation),
  ),
  createOrganisation: command(conveyorProtoFrame(32_768), ({ input, ctx }) =>
    invokeCore(createOrganisation, input, ctx.client.createOrganisation),
  ),
  updateOrganisation: command(conveyorProtoFrame(32_768), ({ input, ctx }) =>
    invokeCore(updateOrganisation, input, ctx.client.updateOrganisation),
  ),
  enrolTotp: command(conveyorProtoFrame(8_192), ({ input, ctx }) =>
    invokeCore(enrolTotp, input, ctx.client.enrolTotp),
  ),
  confirmTotp: command(conveyorProtoFrame(8_192), ({ input, ctx }) =>
    invokeCore(confirmTotp, input, ctx.client.confirmTotp),
  ),
  assertTotp: command(conveyorProtoFrame(8_192), ({ input, ctx }) =>
    invokeCore(assertTotp, input, ctx.client.assertTotp),
  ),
  recordEntityVerification: command(
    conveyorProtoFrame(Math.floor(1.1 * 1024 * 1024)),
    ({ input, ctx }) => invokeCore(recordVerification, input, ctx.client.recordEntityVerification),
  ),
});
