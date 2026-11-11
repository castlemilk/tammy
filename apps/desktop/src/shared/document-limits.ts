// Document ingest budgets shared by the renderer, the conveyor and PDF text extraction.
//
// The core transport caps every Connect message at 1 MiB (services/core/internal/transport/server.go).
// An IngestDocument request carries the original bytes and the extracted text together, so each
// budget below is sized to leave room for the candidate and metadata inside that 1 MiB transport cap.

const KIBIBYTE = 1024;

export const MAX_DOCUMENT_ORIGINAL_BYTES = 800 * KIBIBYTE;
export const MAX_DOCUMENT_TEXT_BYTES = 128 * KIBIBYTE;
// Original bytes + extracted text + 64 KiB for metadata stays under the 1 MiB transport cap.
export const MAX_DOCUMENT_INGEST_REQUEST_BYTES =
  MAX_DOCUMENT_ORIGINAL_BYTES + MAX_DOCUMENT_TEXT_BYTES + 64 * KIBIBYTE;

export const DOCUMENT_PAYLOAD_TOO_LARGE = "DOCUMENT_PAYLOAD_TOO_LARGE" as const;

// Thrown by the conveyor before core is called; invokeCore passes it through with its code.
export class DocumentPayloadTooLarge extends Error {
  readonly code = DOCUMENT_PAYLOAD_TOO_LARGE;

  constructor() {
    super(DOCUMENT_PAYLOAD_TOO_LARGE);
    this.name = "DocumentPayloadTooLarge";
  }
}

const utf8 = new TextEncoder();

export function utf8ByteLength(value: string): number {
  return utf8.encode(value).byteLength;
}

/** Returns true when an ingest payload fits the original and text budgets (UTF-8 bytes). */
export function documentIngestPayloadFits(original: Uint8Array, extractedText: string): boolean {
  return (
    original.byteLength <= MAX_DOCUMENT_ORIGINAL_BYTES &&
    utf8ByteLength(extractedText) <= MAX_DOCUMENT_TEXT_BYTES
  );
}
