import { GetDocumentRequestSchema, GetDocumentResponseSchema, IngestDocumentRequestSchema, IngestDocumentResponseSchema, ListDocumentsRequestSchema, ListDocumentsResponseSchema, SaveDocumentReviewRequestSchema, SaveDocumentReviewResponseSchema } from "@tammy/connect-client/tammy/v1/documents_pb.js";
import type { StandardSchemaV1 } from "electron-conveyor/define";
import { createProtoMethodCodec } from "../../shared/proto-ipc";
import { command, defineModule, query } from "../init";
import { boundedProtoFrame, invokeCore } from "../proto";

const conveyorProtoFrame = (maximumBytes: number): StandardSchemaV1<Uint8Array, Uint8Array> => boundedProtoFrame(maximumBytes) as unknown as StandardSchemaV1<Uint8Array, Uint8Array>;

const listDocuments = createProtoMethodCodec({ input: ListDocumentsRequestSchema, maximumRequestBytes: 16_384, maximumResponseBytes: 4 * 1024 * 1024, output: ListDocumentsResponseSchema });
const getDocument = createProtoMethodCodec({ input: GetDocumentRequestSchema, maximumRequestBytes: 8_192, maximumResponseBytes: 2 * 1024 * 1024, output: GetDocumentResponseSchema });
const ingestDocument = createProtoMethodCodec({ input: IngestDocumentRequestSchema, maximumRequestBytes: 11 * 1024 * 1024, maximumResponseBytes: 2 * 1024 * 1024, output: IngestDocumentResponseSchema });
const saveReview = createProtoMethodCodec({ input: SaveDocumentReviewRequestSchema, maximumRequestBytes: 32_768, maximumResponseBytes: 2 * 1024 * 1024, output: SaveDocumentReviewResponseSchema });

export const documents = defineModule({
  listDocuments: query(conveyorProtoFrame(16_384), ({ input, ctx }) => invokeCore(listDocuments, input, ctx.client.listDocuments)),
  getDocument: query(conveyorProtoFrame(8_192), ({ input, ctx }) => invokeCore(getDocument, input, ctx.client.getDocument)),
  ingestDocument: command(conveyorProtoFrame(11 * 1024 * 1024), ({ input, ctx }) => invokeCore(ingestDocument, input, ctx.client.ingestDocument)),
  saveDocumentReview: command(conveyorProtoFrame(32_768), ({ input, ctx }) => invokeCore(saveReview, input, ctx.client.saveDocumentReview)),
});
