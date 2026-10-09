import { create } from "@bufbuild/protobuf";
import { MoneySchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  DocumentAccountingFilter,
  DocumentCandidateSchema,
  DocumentSchema,
  DocumentStatus,
  ListDocumentsRequestSchema,
  ListDocumentsResponseSchema,
  SaveDocumentReviewRequestSchema,
  SaveDocumentReviewResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";

import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { createConveyorFixture, renderWithConveyor as render } from "../../test/conveyor-fixtures";
import { DocumentsScreen } from "./documents-screen";

const listCodec = createProtoMethodCodec({
  input: ListDocumentsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 4 * 1024 * 1024,
  output: ListDocumentsResponseSchema,
});

it("reaches the next document page and resets signed cursors when filtering", async () => {
  const fixture = createConveyorFixture();
  const organisationId = "018f0000-0000-7000-8000-000000000004";
  const first = create(DocumentSchema, {
    id: "018f0000-0000-7000-8000-000000000010",
    organisationId,
    sourceDisplayName: "First invoice",
    status: DocumentStatus.REVIEWED,
  });
  const later = create(DocumentSchema, {
    id: "018f0000-0000-7000-8000-000000000011",
    organisationId,
    sourceDisplayName: "Invoice 201",
    status: DocumentStatus.REVIEWED,
  });
  const requests: ReturnType<typeof listCodec.decodeRequest>[] = [];
  fixture.procedures.documents.listDocuments.mockImplementation(async (frame: Uint8Array) => {
    const request = listCodec.decodeRequest(frame);
    requests.push(request);
    const firstPage = [
      first,
      ...Array.from({ length: 199 }, (_, index) =>
        create(DocumentSchema, {
          id: `018f0000-0000-7000-8000-${String(index + 1000).padStart(12, "0")}`,
          organisationId,
          sourceDisplayName: `Invoice ${index + 2}`,
          status: DocumentStatus.REVIEWED,
        }),
      ),
    ];
    const documents = request.page?.cursor ? [later] : firstPage;
    return listCodec.encodeResponse(
      create(ListDocumentsResponseSchema, {
        documents,
        page: {
          returnedCount: documents.length,
          nextCursor: request.page?.cursor || request.accounting ? "" : "signed-page-2",
        },
      }),
    );
  });
  const user = userEvent.setup();
  render(
    <DocumentsScreen
      api={fixture.conveyor.documents}
      workspace={{
        workspaceId: "018f0000-0000-7000-8000-000000000001",
        userId: "018f0000-0000-7000-8000-000000000002",
        sessionId: "018f0000-0000-7000-8000-000000000003",
        organisationId,
      }}
    />,
  );
  await screen.findAllByText("First invoice");
  expect(requests[0]?.page?.cursor).toBeUndefined();
  await user.click(screen.getByRole("button", { name: "Next document page" }));
  await screen.findByText("Invoice 201");
  expect(requests.at(-1)?.page?.cursor).toBe("signed-page-2");
  // Moving through the browser does not discard the selected source detail.
  expect(screen.getByRole("heading", { name: "First invoice" })).toBeTruthy();
  await user.selectOptions(
    screen.getByLabelText("Accounting filter"),
    String(DocumentAccountingFilter.NEEDS_GST_REVIEW),
  );
  expect(requests.at(-1)?.page?.cursor).toBeUndefined();
  expect(requests.at(-1)?.accounting).toBe(DocumentAccountingFilter.NEEDS_GST_REVIEW);
});
const reviewCodec = createProtoMethodCodec({
  input: SaveDocumentReviewRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 2 * 1024 * 1024,
  output: SaveDocumentReviewResponseSchema,
});

it("lists retained documents and saves a human-reviewed candidate through named protobuf methods", async () => {
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
  const fixture = createConveyorFixture();
  fixture.procedures.documents.listDocuments.mockImplementation(async (frame: Uint8Array) => {
    const request = listCodec.decodeRequest(frame);
    expect(request.organisationId).toBe(document.organisationId);
    return listCodec.encodeResponse(
      create(ListDocumentsResponseSchema, { documents: [document], page: { returnedCount: 1 } }),
    );
  });
  fixture.procedures.documents.saveDocumentReview.mockImplementation(async (frame: Uint8Array) => {
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
  const user = userEvent.setup();

  render(
    <DocumentsScreen
      api={fixture.conveyor.documents}
      workspace={{
        workspaceId: "018f0000-0000-7000-8000-000000000001",
        userId: "018f0000-0000-7000-8000-000000000002",
        sessionId: "018f0000-0000-7000-8000-000000000003",
        organisationId: document.organisationId,
      }}
    />,
  );

  expect(await screen.findAllByText(/INV-029847/)).toHaveLength(2);
  const supplier = screen.getByLabelText("Supplier");
  await user.clear(supplier);
  await user.type(supplier, "Officeworks Ltd");
  await user.click(screen.getByRole("button", { name: "Save review" }));
  expect(fixture.procedures.documents.saveDocumentReview).toHaveBeenCalledOnce();
});

it("formats invoice totals exactly beyond JavaScript's safe integer range", async () => {
  const fixture = createConveyorFixture();
  const organisationId = "018f0000-0000-7000-8000-000000000004";
  const document = create(DocumentSchema, {
    id: "018f0000-0000-7000-8000-000000000010",
    organisationId,
    status: DocumentStatus.REVIEWED,
    candidate: create(DocumentCandidateSchema, {
      total: create(MoneySchema, { currencyCode: "AUD", minorUnits: 9_000_000_000_000_000_001n }),
    }),
  });
  fixture.procedures.documents.listDocuments.mockResolvedValue(
    listCodec.encodeResponse(
      create(ListDocumentsResponseSchema, { documents: [document], page: { returnedCount: 1 } }),
    ),
  );
  render(
    <DocumentsScreen
      api={fixture.conveyor.documents}
      workspace={{
        workspaceId: "018f0000-0000-7000-8000-000000000001",
        userId: "018f0000-0000-7000-8000-000000000002",
        sessionId: "018f0000-0000-7000-8000-000000000003",
        organisationId,
      }}
    />,
  );
  expect(await screen.findByText("$90,000,000,000,000,000.01")).toBeTruthy();
});
