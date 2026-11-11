import {
  GetDocumentRequestSchema,
  GetDocumentResponseSchema,
  IngestDocumentRequestSchema,
  IngestDocumentResponseSchema,
  ListDocumentsRequestSchema,
  ListDocumentsResponseSchema,
  SaveDocumentReviewRequestSchema,
  SaveDocumentReviewResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import {
  DocumentPayloadTooLarge,
  documentIngestPayloadFits,
  MAX_DOCUMENT_INGEST_REQUEST_BYTES,
} from "../../shared/document-limits";
import { createProtoMethodCodec } from "../../shared/proto-ipc";
import { command, defineModule, query } from "../init";
import { conveyorProtoFrame, invokeCore } from "../proto";

const listDocuments = createProtoMethodCodec({
  input: ListDocumentsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 4 * 1024 * 1024,
  output: ListDocumentsResponseSchema,
});
const getDocument = createProtoMethodCodec({
  input: GetDocumentRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 2 * 1024 * 1024,
  output: GetDocumentResponseSchema,
});
const ingestDocument = createProtoMethodCodec({
  input: IngestDocumentRequestSchema,
  maximumRequestBytes: MAX_DOCUMENT_INGEST_REQUEST_BYTES,
  maximumResponseBytes: 2 * 1024 * 1024,
  output: IngestDocumentResponseSchema,
});
const saveReview = createProtoMethodCodec({
  input: SaveDocumentReviewRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 2 * 1024 * 1024,
  output: SaveDocumentReviewResponseSchema,
});

export const documents = defineModule({
  listDocuments: query(conveyorProtoFrame(16_384), ({ input, ctx }) =>
    invokeCore(listDocuments, input, ctx.client.listDocuments),
  ),
  getDocument: query(conveyorProtoFrame(8_192), ({ input, ctx }) =>
    invokeCore(getDocument, input, ctx.client.getDocument),
  ),
  ingestDocument: command(conveyorProtoFrame(MAX_DOCUMENT_INGEST_REQUEST_BYTES), ({ input, ctx }) =>
    invokeCore(ingestDocument, input, async (request) => {
      // Enforce the payload budgets in the main process, not only in the renderer.
      if (!documentIngestPayloadFits(request.original, request.extractedText)) {
        throw new DocumentPayloadTooLarge();
      }
      return ctx.client.ingestDocument(request);
    }),
  ),
  saveDocumentReview: command(conveyorProtoFrame(32_768), ({ input, ctx }) =>
    invokeCore(saveReview, input, ctx.client.saveDocumentReview),
  ),
});
