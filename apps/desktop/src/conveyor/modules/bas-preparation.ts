import type { DescMessage } from "@bufbuild/protobuf";
import {
  AcknowledgeBasWarningsRequestSchema,
  AcknowledgeBasWarningsResponseSchema,
  ConfirmBasPeriodRequestSchema,
  ConfirmBasPeriodResponseSchema,
  CorrectExternalBasObservationRequestSchema,
  CorrectExternalBasObservationResponseSchema,
  CreateBasReplacementRequestSchema,
  CreateBasReplacementResponseSchema,
  CreateBasRevisionRequestSchema,
  CreateBasRevisionResponseSchema,
  FreezeBasForManualHandoffRequestSchema,
  FreezeBasForManualHandoffResponseSchema,
  GetBasCalculationRequestSchema,
  GetBasCalculationResponseSchema,
  GetBasCalendarRequestSchema,
  GetBasCalendarResponseSchema,
  GetBasExternalDisputeResolutionRequestSchema,
  GetBasExternalDisputeResolutionResponseSchema,
  GetBasExternalObservationRequestSchema,
  GetBasExternalObservationResponseSchema,
  GetBasObligationsProfileRequestSchema,
  GetBasObligationsProfileResponseSchema,
  GetBasPreparationRequestSchema,
  GetBasPreparationResponseSchema,
  GetFrozenBasRequestSchema,
  GetFrozenBasResponseSchema,
  ListBasCalculationsRequestSchema,
  ListBasCalculationsResponseSchema,
  ListBasExportEventsRequestSchema,
  ListBasExportEventsResponseSchema,
  ListBasExternalDisputeResolutionsRequestSchema,
  ListBasExternalDisputeResolutionsResponseSchema,
  ListBasExternalObservationsRequestSchema,
  ListBasExternalObservationsResponseSchema,
  ListBasObligationsProfilesRequestSchema,
  ListBasObligationsProfilesResponseSchema,
  ListBasPeriodsRequestSchema,
  ListBasPeriodsResponseSchema,
  ListBasPreparationsRequestSchema,
  ListBasPreparationsResponseSchema,
  ListFrozenBasRequestSchema,
  ListFrozenBasResponseSchema,
  RecordBasExportEventRequestSchema,
  RecordBasExportEventResponseSchema,
  RecordExternalBasObservationRequestSchema,
  RecordExternalBasObservationResponseSchema,
  RegenerateBasPreparationRequestSchema,
  RegenerateBasPreparationResponseSchema,
  ReplaceRejectedBasAttemptRequestSchema,
  ReplaceRejectedBasAttemptResponseSchema,
  ResolveExternalBasDisputeRequestSchema,
  ResolveExternalBasDisputeResponseSchema,
  SaveBasPaygInputsRequestSchema,
  SaveBasPaygInputsResponseSchema,
  SetBasObligationsProfileRequestSchema,
  SetBasObligationsProfileResponseSchema,
  ValidateBasPreparationRequestSchema,
  ValidateBasPreparationResponseSchema,
} from "@tammy/connect-client/tammy/v1/bas_preparation_pb.js";
import { ConveyorError } from "electron-conveyor/define";
import { BAS_PREPARATION_RESPONSE_FRAME_LIMIT } from "../../shared/bas-preparation-ipc";
import { createProtoMethodCodec } from "../../shared/proto-ipc";
import { invokeBasPreparation as invokeCore } from "../bas-preparation-proto";
import { command, defineModule, query } from "../init";
import { conveyorProtoFrame } from "../proto";

const MAX_QUERY = 16_384;
const MAX_MUTATION = Math.floor(1.1 * 1024 * 1024);
const MAX_RESPONSE = BAS_PREPARATION_RESPONSE_FRAME_LIMIT;

function codec<Input extends DescMessage, Output extends DescMessage>(
  input: Input,
  output: Output,
  maximumRequestBytes = MAX_QUERY,
) {
  return createProtoMethodCodec<Input, Output>({
    input,
    output,
    maximumRequestBytes,
    maximumResponseBytes: MAX_RESPONSE,
  });
}

const getProfile = codec(
  GetBasObligationsProfileRequestSchema,
  GetBasObligationsProfileResponseSchema,
);
const listProfiles = codec(
  ListBasObligationsProfilesRequestSchema,
  ListBasObligationsProfilesResponseSchema,
);
const calendar = codec(GetBasCalendarRequestSchema, GetBasCalendarResponseSchema);
const periods = codec(ListBasPeriodsRequestSchema, ListBasPeriodsResponseSchema);
const getPreparation = codec(GetBasPreparationRequestSchema, GetBasPreparationResponseSchema);
const preparations = codec(ListBasPreparationsRequestSchema, ListBasPreparationsResponseSchema);
const getCalculation = codec(GetBasCalculationRequestSchema, GetBasCalculationResponseSchema);
const calculations = codec(ListBasCalculationsRequestSchema, ListBasCalculationsResponseSchema);
const getFrozen = codec(GetFrozenBasRequestSchema, GetFrozenBasResponseSchema);
const frozen = codec(ListFrozenBasRequestSchema, ListFrozenBasResponseSchema);
const getObservation = codec(
  GetBasExternalObservationRequestSchema,
  GetBasExternalObservationResponseSchema,
);
const observations = codec(
  ListBasExternalObservationsRequestSchema,
  ListBasExternalObservationsResponseSchema,
);
const getDispute = codec(
  GetBasExternalDisputeResolutionRequestSchema,
  GetBasExternalDisputeResolutionResponseSchema,
);
const disputes = codec(
  ListBasExternalDisputeResolutionsRequestSchema,
  ListBasExternalDisputeResolutionsResponseSchema,
);
const exports = codec(ListBasExportEventsRequestSchema, ListBasExportEventsResponseSchema);
const setProfile = codec(
  SetBasObligationsProfileRequestSchema,
  SetBasObligationsProfileResponseSchema,
  MAX_MUTATION,
);
const confirmPeriod = codec(
  ConfirmBasPeriodRequestSchema,
  ConfirmBasPeriodResponseSchema,
  MAX_MUTATION,
);
const savePayg = codec(
  SaveBasPaygInputsRequestSchema,
  SaveBasPaygInputsResponseSchema,
  MAX_MUTATION,
);
const regenerate = codec(
  RegenerateBasPreparationRequestSchema,
  RegenerateBasPreparationResponseSchema,
  MAX_MUTATION,
);
const validate = codec(
  ValidateBasPreparationRequestSchema,
  ValidateBasPreparationResponseSchema,
  MAX_MUTATION,
);
const acknowledge = codec(
  AcknowledgeBasWarningsRequestSchema,
  AcknowledgeBasWarningsResponseSchema,
  MAX_MUTATION,
);
const freezeBas = codec(
  FreezeBasForManualHandoffRequestSchema,
  FreezeBasForManualHandoffResponseSchema,
  MAX_MUTATION,
);
const replacement = codec(
  CreateBasReplacementRequestSchema,
  CreateBasReplacementResponseSchema,
  MAX_MUTATION,
);
const revision = codec(
  CreateBasRevisionRequestSchema,
  CreateBasRevisionResponseSchema,
  MAX_MUTATION,
);
const recordObservation = codec(
  RecordExternalBasObservationRequestSchema,
  RecordExternalBasObservationResponseSchema,
  MAX_MUTATION,
);
const correctObservation = codec(
  CorrectExternalBasObservationRequestSchema,
  CorrectExternalBasObservationResponseSchema,
  MAX_MUTATION,
);
const resolveDispute = codec(
  ResolveExternalBasDisputeRequestSchema,
  ResolveExternalBasDisputeResponseSchema,
  MAX_MUTATION,
);
const rejectedReplacement = codec(
  ReplaceRejectedBasAttemptRequestSchema,
  ReplaceRejectedBasAttemptResponseSchema,
  MAX_MUTATION,
);
const recordExport = codec(
  RecordBasExportEventRequestSchema,
  RecordBasExportEventResponseSchema,
  MAX_MUTATION,
);

export const basPreparation = defineModule({
  getBasObligationsProfile: query(conveyorProtoFrame(MAX_QUERY), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(getProfile, input, ctx.client.basPreparation.getBasObligationsProfile);
  }),
  listBasObligationsProfiles: query(conveyorProtoFrame(MAX_QUERY), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(listProfiles, input, ctx.client.basPreparation.listBasObligationsProfiles);
  }),
  getBasCalendar: query(conveyorProtoFrame(MAX_QUERY), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(calendar, input, ctx.client.basPreparation.getBasCalendar);
  }),
  listBasPeriods: query(conveyorProtoFrame(MAX_QUERY), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(periods, input, ctx.client.basPreparation.listBasPeriods);
  }),
  getBasPreparation: query(conveyorProtoFrame(MAX_QUERY), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(getPreparation, input, ctx.client.basPreparation.getBasPreparation);
  }),
  listBasPreparations: query(conveyorProtoFrame(MAX_QUERY), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(preparations, input, ctx.client.basPreparation.listBasPreparations);
  }),
  getBasCalculation: query(conveyorProtoFrame(MAX_QUERY), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(getCalculation, input, ctx.client.basPreparation.getBasCalculation);
  }),
  listBasCalculations: query(conveyorProtoFrame(MAX_QUERY), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(calculations, input, ctx.client.basPreparation.listBasCalculations);
  }),
  getFrozenBas: query(conveyorProtoFrame(MAX_QUERY), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(getFrozen, input, ctx.client.basPreparation.getFrozenBas);
  }),
  listFrozenBas: query(conveyorProtoFrame(MAX_QUERY), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(frozen, input, ctx.client.basPreparation.listFrozenBas);
  }),
  getBasExternalObservation: query(conveyorProtoFrame(MAX_QUERY), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(getObservation, input, ctx.client.basPreparation.getBasExternalObservation);
  }),
  listBasExternalObservations: query(conveyorProtoFrame(MAX_QUERY), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(observations, input, ctx.client.basPreparation.listBasExternalObservations);
  }),
  getBasExternalDisputeResolution: query(conveyorProtoFrame(MAX_QUERY), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(getDispute, input, ctx.client.basPreparation.getBasExternalDisputeResolution);
  }),
  listBasExternalDisputeResolutions: query(conveyorProtoFrame(MAX_QUERY), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(disputes, input, ctx.client.basPreparation.listBasExternalDisputeResolutions);
  }),
  listBasExportEvents: query(conveyorProtoFrame(MAX_QUERY), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(exports, input, ctx.client.basPreparation.listBasExportEvents);
  }),
  setBasObligationsProfile: command(conveyorProtoFrame(MAX_MUTATION), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(setProfile, input, ctx.client.basPreparation.setBasObligationsProfile);
  }),
  confirmBasPeriod: command(conveyorProtoFrame(MAX_MUTATION), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(confirmPeriod, input, ctx.client.basPreparation.confirmBasPeriod);
  }),
  saveBasPaygInputs: command(conveyorProtoFrame(MAX_MUTATION), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(savePayg, input, ctx.client.basPreparation.saveBasPaygInputs);
  }),
  regenerateBasPreparation: command(conveyorProtoFrame(MAX_MUTATION), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(regenerate, input, ctx.client.basPreparation.regenerateBasPreparation);
  }),
  validateBasPreparation: command(conveyorProtoFrame(MAX_MUTATION), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(validate, input, ctx.client.basPreparation.validateBasPreparation);
  }),
  acknowledgeBasWarnings: command(conveyorProtoFrame(MAX_MUTATION), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(acknowledge, input, ctx.client.basPreparation.acknowledgeBasWarnings);
  }),
  freezeBasForManualHandoff: command(conveyorProtoFrame(MAX_MUTATION), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(freezeBas, input, ctx.client.basPreparation.freezeBasForManualHandoff);
  }),
  createBasReplacement: command(conveyorProtoFrame(MAX_MUTATION), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(replacement, input, ctx.client.basPreparation.createBasReplacement);
  }),
  createBasRevision: command(conveyorProtoFrame(MAX_MUTATION), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(revision, input, ctx.client.basPreparation.createBasRevision);
  }),
  recordExternalBasObservation: command(conveyorProtoFrame(MAX_MUTATION), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(
      recordObservation,
      input,
      ctx.client.basPreparation.recordExternalBasObservation,
    );
  }),
  correctExternalBasObservation: command(conveyorProtoFrame(MAX_MUTATION), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(
      correctObservation,
      input,
      ctx.client.basPreparation.correctExternalBasObservation,
    );
  }),
  resolveExternalBasDispute: command(conveyorProtoFrame(MAX_MUTATION), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(resolveDispute, input, ctx.client.basPreparation.resolveExternalBasDispute);
  }),
  replaceRejectedBasAttempt: command(conveyorProtoFrame(MAX_MUTATION), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(
      rejectedReplacement,
      input,
      ctx.client.basPreparation.replaceRejectedBasAttempt,
    );
  }),
  recordBasExportEvent: command(conveyorProtoFrame(MAX_MUTATION), ({ input, ctx }) => {
    if (!ctx.client.basPreparation)
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    return invokeCore(recordExport, input, ctx.client.basPreparation.recordBasExportEvent);
  }),
});
