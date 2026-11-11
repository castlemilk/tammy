import { create } from "@bufbuild/protobuf";
import {
  type IngestDocumentRequest,
  IngestDocumentRequestSchema,
  IngestDocumentResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import { createCaller } from "electron-conveyor/main";
import { describe, expect, it, vi } from "vitest";

import {
  MAX_DOCUMENT_INGEST_REQUEST_BYTES,
  MAX_DOCUMENT_ORIGINAL_BYTES,
  MAX_DOCUMENT_TEXT_BYTES,
} from "../../shared/document-limits";
import { createProtoMethodCodec } from "../../shared/proto-ipc";
import { createTammyRouter } from "../router";

vi.mock("electron", () => ({ app: { isPackaged: true } }));

const codec = createProtoMethodCodec({
  input: IngestDocumentRequestSchema,
  maximumRequestBytes: MAX_DOCUMENT_INGEST_REQUEST_BYTES,
  maximumResponseBytes: 2 * 1024 * 1024,
  output: IngestDocumentResponseSchema,
});

function ingestFrame(original: Uint8Array, extractedText: string): Uint8Array {
  return codec.encodeRequest(
    create(IngestDocumentRequestSchema, {
      sourceDisplayName: "boundary.pdf",
      mimeType: "application/pdf",
      original,
      extractedText,
    }),
  );
}

function callerWithIngest() {
  const ingestDocument = vi.fn(async (_request: IngestDocumentRequest) =>
    create(IngestDocumentResponseSchema, {}),
  );
  const caller = createCaller(createTammyRouter(), {
    ctx: {
      client: { ingestDocument } as never,
      fileIntake: {} as never,
    },
  });
  return { caller, ingestDocument };
}

describe("documents.ingestDocument payload budgets", () => {
  it("rejects an original one byte over the limit before core is called", async () => {
    const { caller, ingestDocument } = callerWithIngest();
    const frame = ingestFrame(new Uint8Array(MAX_DOCUMENT_ORIGINAL_BYTES + 1), "");

    await expect(caller.documents.ingestDocument(frame)).rejects.toMatchObject({
      code: "DOCUMENT_PAYLOAD_TOO_LARGE",
    });
    expect(ingestDocument).not.toHaveBeenCalled();
  });

  it("rejects extracted text one UTF-8 byte over the limit before core is called", async () => {
    const { caller, ingestDocument } = callerWithIngest();
    // "a" is one byte, so this is exactly MAX + 1 bytes while staying within the frame cap.
    const frame = ingestFrame(new Uint8Array(1), "a".repeat(MAX_DOCUMENT_TEXT_BYTES + 1));

    await expect(caller.documents.ingestDocument(frame)).rejects.toMatchObject({
      code: "DOCUMENT_PAYLOAD_TOO_LARGE",
    });
    expect(ingestDocument).not.toHaveBeenCalled();
  });

  it("rejects multibyte text whose UTF-8 length exceeds the limit even when its unit count does not", async () => {
    const { caller, ingestDocument } = callerWithIngest();
    // 43,691 euro signs are 131,073 UTF-8 bytes (one over) but only 43,691 UTF-16 units.
    const frame = ingestFrame(new Uint8Array(1), "€".repeat(43_691));

    await expect(caller.documents.ingestDocument(frame)).rejects.toMatchObject({
      code: "DOCUMENT_PAYLOAD_TOO_LARGE",
    });
    expect(ingestDocument).not.toHaveBeenCalled();
  });

  it("passes exactly-at-limit original and text through to core", async () => {
    const { caller, ingestDocument } = callerWithIngest();
    const frame = ingestFrame(
      new Uint8Array(MAX_DOCUMENT_ORIGINAL_BYTES),
      "a".repeat(MAX_DOCUMENT_TEXT_BYTES),
    );

    await expect(caller.documents.ingestDocument(frame)).resolves.toBeInstanceOf(Uint8Array);
    expect(ingestDocument).toHaveBeenCalledOnce();
    const forwarded = ingestDocument.mock.calls[0]?.[0];
    if (!forwarded) throw new Error("core ingest was not called");
    expect(forwarded.original.byteLength).toBe(MAX_DOCUMENT_ORIGINAL_BYTES);
    expect(forwarded.extractedText.length).toBe(MAX_DOCUMENT_TEXT_BYTES);
  });
});
