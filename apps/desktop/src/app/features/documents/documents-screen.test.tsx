import { create } from "@bufbuild/protobuf";
import { MoneySchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  DocumentAccountingFilter,
  DocumentCandidateSchema,
  DocumentSchema,
  DocumentStatus,
  IngestDocumentRequestSchema,
  IngestDocumentResponseSchema,
  ListDocumentsRequestSchema,
  ListDocumentsResponseSchema,
  SaveDocumentReviewRequestSchema,
  SaveDocumentReviewResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import { act, screen, waitFor, within } from "@testing-library/react";
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
const reviewCodec = createProtoMethodCodec({
  input: SaveDocumentReviewRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 2 * 1024 * 1024,
  output: SaveDocumentReviewResponseSchema,
});
const ingestCodec = createProtoMethodCodec({
  input: IngestDocumentRequestSchema,
  maximumRequestBytes: 11 * 1024 * 1024,
  maximumResponseBytes: 2 * 1024 * 1024,
  output: IngestDocumentResponseSchema,
});

const workspace = {
  workspaceId: "018f0000-0000-7000-8000-000000000001",
  userId: "018f0000-0000-7000-8000-000000000002",
  sessionId: "018f0000-0000-7000-8000-000000000003",
  organisationId: "018f0000-0000-7000-8000-000000000004",
};

function createReviewFixture() {
  const fixture = createConveyorFixture();
  const candidate = create(DocumentCandidateSchema, {
    supplierName: "Officeworks",
    invoiceNumber: "INV-029847",
    subtotal: create(MoneySchema, { currencyCode: "AUD", minorUnits: 29_000n }),
    gst: create(MoneySchema, { currencyCode: "AUD", minorUnits: 2_900n }),
    total: create(MoneySchema, { currencyCode: "AUD", minorUnits: 31_900n }),
  });
  let retained = create(DocumentSchema, {
    id: "018f0000-0000-7000-8000-000000000010",
    organisationId: workspace.organisationId,
    version: 1n,
    status: DocumentStatus.NEEDS_REVIEW,
    sourceDisplayName: "officeworks.pdf",
    mimeType: "application/pdf",
    byteLength: 4096n,
    sha256: new Uint8Array(32).fill(7),
    extractedText: "Officeworks Invoice INV-029847 Total $319.00",
    candidate,
  });
  fixture.procedures.documents.listDocuments.mockImplementation(async (frame: Uint8Array) => {
    const request = listCodec.decodeRequest(frame);
    const documents = request.status && request.status !== retained.status ? [] : [retained];
    return listCodec.encodeResponse(
      create(ListDocumentsResponseSchema, {
        documents,
        page: { returnedCount: documents.length },
      }),
    );
  });
  fixture.procedures.documents.saveDocumentReview.mockImplementation(async (frame: Uint8Array) => {
    const request = reviewCodec.decodeRequest(frame);
    if (request.documentId !== retained.id || request.expectedVersion !== retained.version)
      throw new Error("stale review request");
    retained = create(DocumentSchema, {
      ...retained,
      version: retained.version + 1n,
      status: DocumentStatus.REVIEWED,
      candidate: request.candidate ?? retained.candidate,
    });
    return reviewCodec.encodeResponse(
      create(SaveDocumentReviewResponseSchema, { document: retained }),
    );
  });
  fixture.procedures.documents.ingestDocument.mockImplementation(async (frame: Uint8Array) => {
    const request = ingestCodec.decodeRequest(frame);
    return ingestCodec.encodeResponse(
      create(IngestDocumentResponseSchema, {
        document: create(DocumentSchema, {
          ...retained,
          id: "018f0000-0000-7000-8000-000000000999",
          status: DocumentStatus.NEEDS_REVIEW,
          sourceDisplayName: request.sourceDisplayName,
          mimeType: request.mimeType,
          byteLength: 3n,
          candidate: request.candidate,
        }),
      }),
    );
  });
  return { fixture, document: () => retained };
}

function renderDocuments(fixture: ReturnType<typeof createConveyorFixture>) {
  return render(<DocumentsScreen api={fixture.conveyor.documents} workspace={workspace} />);
}

function lastReviewRequest(fixture: ReturnType<typeof createConveyorFixture>) {
  const frame = fixture.procedures.documents.saveDocumentReview.mock.calls.at(-1)?.[0];
  if (!frame) throw new Error("review save request missing");
  return reviewCodec.decodeRequest(frame);
}

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
    const firstPage = [first];
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
    screen.getByLabelText("Accounting state"),
    String(DocumentAccountingFilter.NEEDS_GST_REVIEW),
  );
  expect(requests.at(-1)?.page?.cursor).toBeUndefined();
  expect(requests.at(-1)?.accounting).toBe(DocumentAccountingFilter.NEEDS_GST_REVIEW);
});
it("keeps a confirmed review over a stale list response", async () => {
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
    const saved = create(DocumentSchema, {
      ...document,
      version: 2n,
      status: DocumentStatus.REVIEWED,
      candidate: request.candidate ?? candidate,
    });
    return reviewCodec.encodeResponse(
      create(SaveDocumentReviewResponseSchema, { document: saved }),
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
  expect(await screen.findByRole("button", { name: "Review saved" })).toHaveProperty(
    "disabled",
    true,
  );
  expect((screen.getByLabelText("Supplier") as HTMLInputElement).readOnly).toBe(true);
  expect(fixture.procedures.documents.saveDocumentReview).toHaveBeenCalledOnce();
});

it("preserves dirty review fields when a refreshed list returns the same document revision", async () => {
  const { fixture } = createReviewFixture();
  const user = userEvent.setup();
  renderDocuments(fixture);
  await screen.findByRole("table", { name: "Retained documents" });

  const supplier = screen.getByLabelText("Supplier") as HTMLInputElement;
  await user.clear(supplier);
  await user.type(supplier, "Officeworks reviewed name");
  await user.click(screen.getByRole("button", { name: "Refresh documents" }));
  const refreshButton = screen.getByRole("button", { name: "Refresh documents" });
  await waitFor(() => expect(fixture.procedures.documents.listDocuments).toHaveBeenCalledTimes(2));
  await waitFor(() => expect(refreshButton).toHaveProperty("disabled", false));

  expect(supplier.value).toBe("Officeworks reviewed name");
  await user.click(screen.getByRole("button", { name: "Save review" }));
  await screen.findByRole("button", { name: "Review saved" });

  const request = lastReviewRequest(fixture);
  expect(request.documentId).toBe("018f0000-0000-7000-8000-000000000010");
  expect(request.expectedVersion).toBe(1n);
  expect(request.candidate?.supplierName).toBe("Officeworks reviewed name");
});

it("resets dirty review fields when the selected document revision changes", async () => {
  const { fixture, document: readDocument } = createReviewFixture();
  let listed = readDocument();
  fixture.procedures.documents.listDocuments.mockImplementation(async () =>
    listCodec.encodeResponse(
      create(ListDocumentsResponseSchema, {
        documents: [listed],
        page: { returnedCount: 1 },
      }),
    ),
  );
  fixture.procedures.documents.saveDocumentReview.mockImplementation(async (frame: Uint8Array) => {
    const request = reviewCodec.decodeRequest(frame);
    expect(request.documentId).toBe(listed.id);
    expect(request.expectedVersion).toBe(listed.version);
    listed = create(DocumentSchema, {
      ...listed,
      version: listed.version + 1n,
      status: DocumentStatus.REVIEWED,
      candidate: request.candidate ?? listed.candidate,
    });
    return reviewCodec.encodeResponse(
      create(SaveDocumentReviewResponseSchema, { document: listed }),
    );
  });
  const user = userEvent.setup();
  renderDocuments(fixture);
  await screen.findByRole("table", { name: "Retained documents" });

  const supplier = screen.getByLabelText("Supplier") as HTMLInputElement;
  await user.clear(supplier);
  await user.type(supplier, "Unsaved edit from older revision");
  listed = create(DocumentSchema, {
    ...listed,
    version: 2n,
    candidate: create(DocumentCandidateSchema, { supplierName: "Officeworks corrected revision" }),
  });
  await user.click(screen.getByRole("button", { name: "Refresh documents" }));
  const refreshButton = screen.getByRole("button", { name: "Refresh documents" });
  await waitFor(() => expect(fixture.procedures.documents.listDocuments).toHaveBeenCalledTimes(2));
  await screen.findByText("Officeworks corrected revision");
  await waitFor(() => expect(refreshButton).toHaveProperty("disabled", false));

  expect((screen.getByLabelText("Supplier") as HTMLInputElement).value).toBe(
    "Officeworks corrected revision",
  );
  await user.click(screen.getByRole("button", { name: "Save review" }));
  await screen.findByRole("button", { name: "Review saved" });
  const request = lastReviewRequest(fixture);
  expect(request.expectedVersion).toBe(2n);
  expect(request.candidate?.supplierName).toBe("Officeworks corrected revision");
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

it("saves once after human confirmation and shows the retained review as read-only", async () => {
  const { fixture } = createReviewFixture();
  const saveReview = fixture.procedures.documents.saveDocumentReview;
  const confirmedReview = saveReview.getMockImplementation();
  let confirm: ((frame: Uint8Array) => void) | undefined;
  saveReview.mockImplementation(
    () =>
      new Promise<Uint8Array>((resolve) => {
        confirm = resolve;
      }),
  );
  const user = userEvent.setup();
  renderDocuments(fixture);
  await screen.findByRole("table", { name: "Retained documents" });
  const supplier = screen.getByLabelText("Supplier") as HTMLInputElement;
  await user.clear(supplier);
  await user.type(supplier, "Officeworks Ltd");
  await user.dblClick(screen.getByRole("button", { name: "Save review" }));
  expect(saveReview).toHaveBeenCalledOnce();
  expect((screen.getByRole("button", { name: "Saving…" }) as HTMLButtonElement).disabled).toBe(
    true,
  );

  const request = saveReview.mock.calls[0]?.[0];
  if (!request || !confirm || !confirmedReview) throw new Error("review confirmation missing");
  const confirmation = await confirmedReview(request);
  await act(async () => confirm?.(confirmation));

  expect(
    ((await screen.findByRole("button", { name: "Review saved" })) as HTMLButtonElement).disabled,
  ).toBe(true);
  expect(supplier.readOnly).toBe(true);
  expect(supplier.value).toBe("Officeworks Ltd");
  expect(saveReview).toHaveBeenCalledOnce();
  await waitFor(() => expect(fixture.procedures.documents.listDocuments).toHaveBeenCalledTimes(2));
});

it("keeps failed review edits and retries with the same idempotency key", async () => {
  const { fixture } = createReviewFixture();
  fixture.procedures.documents.saveDocumentReview.mockRejectedValueOnce(
    new Error("interrupted response"),
  );
  const user = userEvent.setup();
  renderDocuments(fixture);
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
  expect(await screen.findByRole("button", { name: "Review saved" })).toBeTruthy();
  const requests = fixture.procedures.documents.saveDocumentReview.mock.calls.map(([frame]) =>
    reviewCodec.decodeRequest(frame),
  );
  expect(requests).toHaveLength(2);
  expect(requests[0]?.commandContext?.idempotencyKey).toBe(
    requests[1]?.commandContext?.idempotencyKey,
  );
});

it("restores cancelled edits and returns focus to the selected row when review closes", async () => {
  const { fixture } = createReviewFixture();
  const user = userEvent.setup();
  renderDocuments(fixture);
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
  expect(fixture.procedures.documents.saveDocumentReview).not.toHaveBeenCalled();
  expect(screen.queryByRole("region", { name: "Document review" })).toBeNull();
  expect(document.activeElement).toBe(rowButton);
});

it("shows a recoverable load failure without presenting an empty document collection", async () => {
  const { fixture } = createReviewFixture();
  fixture.procedures.documents.listDocuments.mockRejectedValueOnce(new Error("offline engine"));
  const user = userEvent.setup();
  renderDocuments(fixture);
  expect(await screen.findByText("Documents unavailable")).toBeTruthy();
  expect(screen.queryByText("Your next clear step")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Try again" }));
  expect(await screen.findByRole("table", { name: "Retained documents" })).toBeTruthy();
});

it("keeps the no-match state when a server-side filter returns an empty page", async () => {
  const fixture = createConveyorFixture();
  const reviewed = create(DocumentSchema, {
    id: "018f0000-0000-7000-8000-000000000010",
    organisationId: workspace.organisationId,
    version: 1n,
    status: DocumentStatus.REVIEWED,
    sourceDisplayName: "reviewed-invoice.png",
  });
  const requests: ReturnType<typeof listCodec.decodeRequest>[] = [];
  fixture.procedures.documents.listDocuments.mockImplementation(async (frame: Uint8Array) => {
    const request = listCodec.decodeRequest(frame);
    requests.push(request);
    const documents = request.status && request.status !== reviewed.status ? [] : [reviewed];
    return listCodec.encodeResponse(
      create(ListDocumentsResponseSchema, {
        documents,
        page: { returnedCount: documents.length },
      }),
    );
  });
  const user = userEvent.setup();
  renderDocuments(fixture);

  const initialTable = await screen.findByRole("table", { name: "Retained documents" });
  await within(initialTable).findByText("reviewed-invoice.png");
  await user.click(screen.getByRole("button", { name: /Needs review/ }));

  expect(await screen.findByText("No matching documents", { exact: true })).toBeTruthy();
  expect(screen.queryByText("Your next clear step")).toBeNull();
  expect(screen.getByRole("heading", { name: "reviewed-invoice.png" })).toBeTruthy();
  expect(requests.at(-1)?.status).toBe(DocumentStatus.NEEDS_REVIEW);

  await user.click(screen.getByRole("button", { name: /All documents/ }));
  await within(screen.getByRole("table", { name: "Retained documents" })).findByText(
    "reviewed-invoice.png",
  );
  expect(requests.at(-1)?.status).toBeUndefined();
});

it("keeps the current page bounded while showing a newly retained source for review", async () => {
  const { fixture, document: retained } = createReviewFixture();
  const documents = Array.from({ length: 200 }, (_, index) =>
    create(DocumentSchema, {
      ...retained(),
      id: `018f0000-0000-7000-8000-${(index + 100).toString().padStart(12, "0")}`,
      sourceDisplayName: `retained-${index}.pdf`,
    }),
  );
  fixture.procedures.documents.listDocuments.mockResolvedValue(
    listCodec.encodeResponse(
      create(ListDocumentsResponseSchema, { documents, page: { returnedCount: 200 } }),
    ),
  );
  const user = userEvent.setup();
  const { container } = renderDocuments(fixture);
  await screen.findByRole("table", { name: "Retained documents" });
  const file = new File(["synthetic"], "new-synthetic.png", { type: "image/png" });
  Object.defineProperty(file, "arrayBuffer", {
    value: async () => new Uint8Array([1, 2, 3]).buffer,
  });
  const input = container.querySelector<HTMLInputElement>('input[type="file"]');
  if (!input) throw new Error("file input missing");
  await user.upload(input, file);
  expect(await screen.findByRole("heading", { name: "new-synthetic.png" })).toBeTruthy();
  expect(
    within(screen.getByRole("table", { name: "Retained documents" })).getAllByRole("row"),
  ).toHaveLength(201);
  expect(fixture.procedures.documents.ingestDocument).toHaveBeenCalledOnce();
});

it("refreshes the default document page after a source is retained", async () => {
  const fixture = createConveyorFixture();
  const existing = create(DocumentSchema, {
    id: "018f0000-0000-7000-8000-000000000010",
    organisationId: workspace.organisationId,
    version: 1n,
    status: DocumentStatus.NEEDS_REVIEW,
    sourceDisplayName: "existing-invoice.png",
    mimeType: "image/png",
    byteLength: 3n,
    sha256: new Uint8Array(32).fill(1),
  });
  let retainedDocuments = [existing];
  const listRequests: ReturnType<typeof listCodec.decodeRequest>[] = [];
  fixture.procedures.documents.listDocuments.mockImplementation(async (frame: Uint8Array) => {
    const request = listCodec.decodeRequest(frame);
    listRequests.push(request);
    return listCodec.encodeResponse(
      create(ListDocumentsResponseSchema, {
        documents: retainedDocuments,
        page: { returnedCount: retainedDocuments.length },
      }),
    );
  });
  fixture.procedures.documents.ingestDocument.mockImplementation(async (frame: Uint8Array) => {
    const request = ingestCodec.decodeRequest(frame);
    const retained = create(DocumentSchema, {
      id: "018f0000-0000-7000-8000-000000000999",
      organisationId: workspace.organisationId,
      version: 1n,
      status: DocumentStatus.NEEDS_REVIEW,
      sourceDisplayName: request.sourceDisplayName,
      mimeType: request.mimeType,
      byteLength: 3n,
      sha256: new Uint8Array(32).fill(2),
      candidate: request.candidate,
    });
    retainedDocuments = [retained, ...retainedDocuments];
    return ingestCodec.encodeResponse(create(IngestDocumentResponseSchema, { document: retained }));
  });

  const user = userEvent.setup();
  const { container } = renderDocuments(fixture);
  const table = await screen.findByRole("table", { name: "Retained documents" });
  await within(table).findByText("existing-invoice.png");
  expect(listRequests).toHaveLength(1);

  const file = new File(["synthetic"], "new-invoice.png", { type: "image/png" });
  Object.defineProperty(file, "arrayBuffer", {
    value: async () => new Uint8Array([1, 2, 3]).buffer,
  });
  const input = container.querySelector<HTMLInputElement>('input[type="file"]');
  if (!input) throw new Error("file input missing");
  await user.upload(input, file);

  const refreshedTable = screen.getByRole("table", { name: "Retained documents" });
  await within(refreshedTable).findByText("new-invoice.png");
  await waitFor(() => expect(listRequests).toHaveLength(2));
  expect(listRequests.every((request) => request.page?.cursor === undefined)).toBe(true);
  expect(listRequests.every((request) => request.page?.pageSize === 200)).toBe(true);
  expect(await screen.findByRole("heading", { name: "new-invoice.png" })).toBeTruthy();
  expect(screen.queryByText(/last action was confirmed/)).toBeNull();
});

it("keeps confirmed retention separate from a failed document-list refresh", async () => {
  const { fixture, document: existingDocument } = createReviewFixture();
  let listCalls = 0;
  fixture.procedures.documents.listDocuments.mockImplementation(async () => {
    listCalls += 1;
    if (listCalls > 1) throw new Error("list refresh unavailable");
    return listCodec.encodeResponse(
      create(ListDocumentsResponseSchema, {
        documents: [existingDocument()],
        page: { returnedCount: 1 },
      }),
    );
  });
  const user = userEvent.setup();
  const { container } = renderDocuments(fixture);
  await screen.findByRole("table", { name: "Retained documents" });

  const file = new File(["synthetic"], "confirmed-upload.png", { type: "image/png" });
  Object.defineProperty(file, "arrayBuffer", {
    value: async () => new Uint8Array([1, 2, 3]).buffer,
  });
  const input = container.querySelector<HTMLInputElement>('input[type="file"]');
  if (!input) throw new Error("file input missing");
  await user.upload(input, file);

  expect(await screen.findByRole("heading", { name: "confirmed-upload.png" })).toBeTruthy();
  expect(
    await screen.findByText(
      /The last action was confirmed, but the latest document list could not be loaded/,
    ),
  ).toBeTruthy();
  expect(
    screen.queryByText("The document could not be retained. It has not left this device."),
  ).toBeNull();
  expect(listCalls).toBe(2);
});

it("returns focus to search when a status filter hides the selected source", async () => {
  const { fixture } = createReviewFixture();
  const user = userEvent.setup();
  renderDocuments(fixture);
  await screen.findByRole("table", { name: "Retained documents" });
  await user.click(screen.getByRole("button", { name: /Reviewed/ }));
  expect(screen.queryByRole("table", { name: "Retained documents" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Close review" }));
  expect(document.activeElement).toBe(screen.getByLabelText("Search documents"));
  expect(fixture.procedures.documents.saveDocumentReview).not.toHaveBeenCalled();
});
