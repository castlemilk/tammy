import { create } from "@bufbuild/protobuf";
import { MoneySchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  DocumentCandidateSchema,
  DocumentSchema,
  DocumentStatus,
  ListDocumentsRequestSchema,
  ListDocumentsResponseSchema,
  SaveDocumentReviewRequestSchema,
  SaveDocumentReviewResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { DocumentsScreen } from "./documents-screen";

const listCodec = createProtoMethodCodec({
  input: ListDocumentsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 4 * 1024 * 1024,
  output: ListDocumentsResponseSchema,
});
const reviewCodec = createProtoMethodCodec({
  input: SaveDocumentReviewRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 2 * 1024 * 1024,
  output: SaveDocumentReviewResponseSchema,
});

function fixture() {
  const candidate = create(DocumentCandidateSchema, {
    supplierName: "Officeworks",
    invoiceNumber: "INV-029847",
    subtotal: create(MoneySchema, { currencyCode: "AUD", minorUnits: 29_000n }),
    gst: create(MoneySchema, { currencyCode: "AUD", minorUnits: 2_900n }),
    total: create(MoneySchema, { currencyCode: "AUD", minorUnits: 31_900n }),
  });
  const document = create(DocumentSchema, {
    id: "018f0000-0000-7000-8000-000000000010",
    organisationId: "018f0000-0000-7000-8000-000000000004",
    version: 1n,
    status: DocumentStatus.NEEDS_REVIEW,
    sourceDisplayName: "officeworks.pdf",
    mimeType: "application/pdf",
    byteLength: 4096n,
    sha256: new Uint8Array(32).fill(7),
    extractedText: "Officeworks Invoice INV-029847 Total $319.00",
    candidate,
  });
  const listDocuments = vi.fn(async (frame: Uint8Array) => {
    const request = listCodec.decodeRequest(frame);
    expect(request.organisationId).toBe(document.organisationId);
    return listCodec.encodeResponse(
      create(ListDocumentsResponseSchema, { documents: [document], page: { returnedCount: 1 } }),
    );
  });
  const saveDocumentReview = vi.fn(async (frame: Uint8Array) => {
    const request = reviewCodec.decodeRequest(frame);
    expect(request.documentId).toBe(document.id);
    expect(request.expectedVersion).toBe(1n);
    expect(request.candidate?.supplierName).toBe("Officeworks Ltd");
    return reviewCodec.encodeResponse(
      create(SaveDocumentReviewResponseSchema, {
        document: create(DocumentSchema, {
          ...document,
          version: 2n,
          status: DocumentStatus.REVIEWED,
          candidate: request.candidate ?? candidate,
        }),
      }),
    );
  });
  const api = {
    ingestDocument: vi.fn(),
    listDocuments,
    saveDocumentReview,
  } satisfies Pick<TammyDesktopAPI, "ingestDocument" | "listDocuments" | "saveDocumentReview">;
  return { api, document };
}
function renderDocuments(api: Parameters<typeof DocumentsScreen>[0]["api"]) {
  return render(
    <DocumentsScreen
      api={api}
      workspace={{
        workspaceId: "018f0000-0000-7000-8000-000000000001",
        userId: "018f0000-0000-7000-8000-000000000002",
        sessionId: "018f0000-0000-7000-8000-000000000003",
        organisationId: "018f0000-0000-7000-8000-000000000004",
      }}
    />,
  );
}
it("saves a review once and displays the confirmed details as read-only", async () => {
  const { api } = fixture();
  const user = userEvent.setup();
  renderDocuments(api);
  await screen.findByRole("table", { name: "Retained documents" });
  const supplier = screen.getByLabelText("Supplier");
  await user.clear(supplier);
  await user.type(supplier, "Officeworks Ltd");
  await user.dblClick(screen.getByRole("button", { name: "Save review" }));
  await waitFor(() =>
    expect(
      (screen.getByRole("button", { name: "Review saved" }) as HTMLButtonElement).disabled,
    ).toBe(true),
  );
  expect((supplier as HTMLInputElement).readOnly).toBe(true);
  expect((supplier as HTMLInputElement).value).toBe("Officeworks Ltd");
  expect(api.saveDocumentReview).toHaveBeenCalledOnce();
  expect(api.listDocuments).toHaveBeenCalledOnce();
});
it("keeps failed review edits and retries the same command without duplicating approval", async () => {
  const { api } = fixture();
  api.saveDocumentReview.mockRejectedValueOnce(new Error("interrupted response"));
  const user = userEvent.setup();
  renderDocuments(api);
  await screen.findByRole("table", { name: "Retained documents" });
  await user.clear(screen.getByLabelText("Supplier"));
  await user.type(screen.getByLabelText("Supplier"), "Officeworks Ltd");
  await user.click(screen.getByRole("button", { name: "Save review" }));
  expect(await screen.findByRole("alert")).toHaveProperty(
    "textContent",
    expect.stringContaining("Your changes are still here"),
  );
  expect((screen.getByLabelText("Supplier") as HTMLInputElement).value).toBe("Officeworks Ltd");
  await user.click(screen.getByRole("button", { name: "Save review" }));
  await waitFor(() => expect(api.saveDocumentReview).toHaveBeenCalledTimes(2));
  const requests = api.saveDocumentReview.mock.calls.map(([frame]) =>
    reviewCodec.decodeRequest(frame),
  );
  expect(requests[0]?.commandContext?.idempotencyKey).toBe(
    requests[1]?.commandContext?.idempotencyKey,
  );
});
it("filters loaded records, restores cancelled edits and returns focus when closing review", async () => {
  const { api } = fixture();
  const user = userEvent.setup();
  renderDocuments(api);
  await screen.findByRole("table", { name: "Retained documents" });
  await user.type(screen.getByLabelText("Search documents"), "missing");
  expect(screen.queryByRole("table", { name: "Retained documents" })).toBeNull();
  expect(screen.getByText("No matching documents")).toBeTruthy();
  await user.clear(screen.getByLabelText("Search documents"));
  const rowButton = within(screen.getByRole("table", { name: "Retained documents" })).getByRole(
    "button",
  );
  await user.clear(screen.getByLabelText("Supplier"));
  await user.type(screen.getByLabelText("Supplier"), "Discard me");
  await user.click(screen.getByRole("button", { name: "Cancel changes" }));
  expect((screen.getByLabelText("Supplier") as HTMLInputElement).value).toBe("Officeworks");
  await user.click(screen.getByRole("button", { name: "Close review" }));
  expect(api.saveDocumentReview).not.toHaveBeenCalled();
  expect(screen.queryByRole("region", { name: "Document review" })).toBeNull();
  expect(document.activeElement).toBe(rowButton);
});
it("shows a recoverable load failure without presenting an empty collection", async () => {
  const { api } = fixture();
  api.listDocuments.mockRejectedValueOnce(new Error("offline engine"));
  const user = userEvent.setup();
  renderDocuments(api);
  expect(await screen.findByText("Documents unavailable")).toBeTruthy();
  expect(screen.queryByText("Your next clear step")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Try again" }));
  expect(await screen.findByRole("table", { name: "Retained documents" })).toBeTruthy();
});
