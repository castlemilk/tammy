import { create, toBinary } from "@bufbuild/protobuf";
import {
  ListAccountsRequestSchema,
  ListAccountsResponseSchema,
  TaxTreatment,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import { ListDocumentsResponseSchema } from "@tammy/connect-client/tammy/v1/documents_pb.js";
import {
  GetInvoiceAccountingRequestSchema,
  GetInvoiceAccountingResponseSchema,
  GetInvoiceLogoRequestSchema,
  GetInvoiceLogoResponseSchema,
  GetInvoiceRequestSchema,
  GetInvoiceResponseSchema,
  GetInvoiceTemplateRequestSchema,
  GetInvoiceTemplateResponseSchema,
  InvoiceLogoSchema,
  InvoiceSchema,
  InvoiceState,
  InvoiceTemplateInputSchema,
  InvoiceTemplateSchema,
  ListInvoicesRequestSchema,
  ListInvoicesResponseSchema,
  SaveInvoiceDraftRequestSchema,
  SaveInvoiceDraftResponseSchema,
  SaveInvoiceTemplateRequestSchema,
  SaveInvoiceTemplateResponseSchema,
  UploadInvoiceLogoRequestSchema,
  UploadInvoiceLogoResponseSchema,
} from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { GstBasis } from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";
import { pngLogo } from "../../../../../../packages/invoices/test-support/logo-fixtures";
import type { TammyConveyor } from "../../../conveyor/client";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { ConnectedInvoicesScreen } from "./connected-invoices-screen";

const workspace = {
  userId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073901",
  sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073902",
  workspaceId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073903",
  organisationId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073904",
};
const getTemplate = createProtoMethodCodec({
  input: GetInvoiceTemplateRequestSchema,
  output: GetInvoiceTemplateResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 32768,
});
const saveTemplate = createProtoMethodCodec({
  input: SaveInvoiceTemplateRequestSchema,
  output: SaveInvoiceTemplateResponseSchema,
  maximumRequestBytes: 32768,
  maximumResponseBytes: 32768,
});
const uploadLogo = createProtoMethodCodec({
  input: UploadInvoiceLogoRequestSchema,
  output: UploadInvoiceLogoResponseSchema,
  maximumRequestBytes: 1179648,
  maximumResponseBytes: 8192,
});
const getLogo = createProtoMethodCodec({
  input: GetInvoiceLogoRequestSchema,
  output: GetInvoiceLogoResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 1179648,
});
const save = createProtoMethodCodec({
  input: SaveInvoiceDraftRequestSchema,
  output: SaveInvoiceDraftResponseSchema,
  maximumRequestBytes: 131072,
  maximumResponseBytes: 131072,
});

it("opens older invoices across pages and reads the full bounded Unicode payment history", async () => {
  const invoiceId = "01900f3c-7b2e-7cc4-98c4-dc0c0c073906";
  const invoice = create(InvoiceSchema, {
    id: invoiceId,
    organisationId: workspace.organisationId,
    version: 2n,
    state: InvoiceState.ISSUED,
    sellerName: "Gamma Systems Pty Ltd",
    sellerAbn: "51824753556",
    subtotalMinor: 10000n,
    gstMinor: 1000n,
    totalMinor: 11000n,
    draft: {
      number: "OLD-001",
      issueDate: "2026-09-30",
      dueDate: "2026-10-14",
      customerName: "Customer",
      customerAddress: "Melbourne",
      sellerAddress: "Melbourne",
      gstRegistered: true,
      lines: [
        {
          description: "Consulting",
          quantityMilli: 1000,
          unitPriceMinor: 10000n,
          taxTreatment: TaxTreatment.TAXABLE,
        },
      ],
    },
  });
  const accounting = create(GetInvoiceAccountingResponseSchema, {
    accounting: {
      invoiceId,
      organisationId: workspace.organisationId,
      posted: true,
      version: 202n,
      gstBasis: GstBasis.CASH,
      receivedMinor: 201n,
      outstandingMinor: 10799n,
      paymentCount: 201n,
      payments: Array.from({ length: 200 }, (_, index) => ({
        id: `01900f3c-7b2e-7cc4-98c4-${(index + 1).toString(16).padStart(12, "0")}`,
        organisationId: workspace.organisationId,
        invoiceId,
        paymentDate: { year: 2026, month: 10, day: 2 },
        amount: { currencyCode: "AUD", minorUnits: 1n },
        allocatedGst: { currencyCode: "AUD", minorUnits: 0n },
        basSalesGross: { currencyCode: "AUD", minorUnits: 1n },
        bankAccountId: workspace.organisationId,
        journalId: workspace.organisationId,
        reference: "🧾".repeat(200),
      })),
    },
  });
  const accountingBytes = toBinary(GetInvoiceAccountingResponseSchema, accounting);
  expect(accountingBytes.byteLength).toBeGreaterThan(131072);
  const accountingCodec = createProtoMethodCodec({
    input: GetInvoiceAccountingRequestSchema,
    output: GetInvoiceAccountingResponseSchema,
    maximumRequestBytes: 8192,
    maximumResponseBytes: 262144,
  });
  const invoiceCodec = createProtoMethodCodec({
    input: GetInvoiceRequestSchema,
    output: GetInvoiceResponseSchema,
    maximumRequestBytes: 8192,
    maximumResponseBytes: 131072,
  });
  const accountsCodec = createProtoMethodCodec({
    input: ListAccountsRequestSchema,
    output: ListAccountsResponseSchema,
    maximumRequestBytes: 16384,
    maximumResponseBytes: 131072,
  });
  const listRequests = createProtoMethodCodec({
    input: ListInvoicesRequestSchema,
    output: ListInvoicesResponseSchema,
    maximumRequestBytes: 8192,
    maximumResponseBytes: 16777216,
  });
  const requestedCursors: string[] = [];
  const conveyor = {
    accounting: {
      listInvoices: async (frame: Parameters<typeof listRequests.decodeRequest>[0]) => {
        const request = listRequests.decodeRequest(frame);
        requestedCursors.push(request.page?.cursor ?? "");
        expect(request.organisationId).toBe(workspace.organisationId);
        return listRequests.encodeResponse(
          create(
            ListInvoicesResponseSchema,
            request.page?.cursor
              ? { invoices: [invoice] }
              : { invoices: [], page: { nextCursor: "older-invoices" } },
          ),
        );
      },
      getInvoice: async () =>
        invoiceCodec.encodeResponse(create(GetInvoiceResponseSchema, { invoice })),
      getInvoiceAccounting: async () => accountingCodec.encodeResponse(accounting),
      listAccounts: async () => accountsCodec.encodeResponse(create(ListAccountsResponseSchema)),
      getInvoiceTemplate: async () =>
        getTemplate.encodeResponse(create(GetInvoiceTemplateResponseSchema)),
    },
    documents: {
      listDocuments: async () => ({
        bytes: toBinary(ListDocumentsResponseSchema, create(ListDocumentsResponseSchema)),
      }),
    },
  } as unknown as TammyConveyor;
  render(<ConnectedInvoicesScreen conveyor={conveyor} workspace={workspace} />);
  await userEvent.click(await screen.findByRole("button", { name: "Open OLD-001" }));
  expect(await screen.findByTestId("invoice-received")).toHaveProperty(
    "textContent",
    expect.stringContaining("$2.01"),
  );
  expect(screen.getByTestId("invoice-outstanding").textContent).toContain("$107.99");
  expect(screen.getByText(/Showing 200 of 201 receipts/)).toBeTruthy();
  expect(
    within(screen.getByRole("table", { name: "Recorded invoice payments" })).getAllByRole("row"),
  ).toHaveLength(201);
  expect(requestedCursors).toEqual(["", "older-invoices"]);
});

it("loads and saves company defaults with organisation, authentication and optimistic version through protobuf", async () => {
  const calls = { reads: 0, writes: 0 };
  const conveyor = {
    accounting: {
      listInvoices: async () => {
        const { toBinary } = await import("@bufbuild/protobuf");
        return { bytes: toBinary(ListInvoicesResponseSchema, create(ListInvoicesResponseSchema)) };
      },
      getInvoiceTemplate: async (frame: Parameters<typeof getTemplate.decodeRequest>[0]) => {
        const request = getTemplate.decodeRequest(frame);
        expect(request.organisationId).toBe(workspace.organisationId);
        expect(request.authentication).toMatchObject({
          actorUserId: workspace.userId,
          sessionId: workspace.sessionId,
        });
        calls.reads += 1;
        return getTemplate.encodeResponse(
          create(GetInvoiceTemplateResponseSchema, {
            template: create(InvoiceTemplateSchema, {
              organisationId: workspace.organisationId,
              version: 7n,
              defaults: create(InvoiceTemplateInputSchema, {
                paymentInstructions: "Original bank",
                paymentTermsDays: 21,
              }),
            }),
          }),
        );
      },
      saveInvoiceTemplate: async (frame: Parameters<typeof saveTemplate.decodeRequest>[0]) => {
        const request = saveTemplate.decodeRequest(frame);
        expect(request.organisationId).toBe(workspace.organisationId);
        expect(request.commandContext?.authentication).toMatchObject({
          actorUserId: workspace.userId,
          sessionId: workspace.sessionId,
        });
        expect(request.commandContext?.idempotencyKey).toMatch(/^[0-9a-f-]{14}7[0-9a-f-]+$/);
        expect(request.expectedVersion).toBe(7n);
        expect(request.defaults?.paymentInstructions).toBe("Updated bank");
        expect(request.defaults?.paymentTermsDays).toBe(365);
        calls.writes += 1;
        return saveTemplate.encodeResponse(
          create(SaveInvoiceTemplateResponseSchema, {
            template: create(InvoiceTemplateSchema, {
              organisationId: request.organisationId,
              version: 8n,
              defaults: request.defaults,
            }),
          }),
        );
      },
    },
    documents: {
      listDocuments: async () => {
        const { toBinary } = await import("@bufbuild/protobuf");
        return {
          bytes: toBinary(ListDocumentsResponseSchema, create(ListDocumentsResponseSchema)),
        };
      },
    },
  } as unknown as TammyConveyor;
  render(<ConnectedInvoicesScreen conveyor={conveyor} workspace={workspace} />);
  await waitFor(() => expect(calls.reads).toBe(1));
  await userEvent.click(screen.getByRole("button", { name: "Manage invoice template" }));
  const panel = screen.getByRole("region", { name: "Company invoice template" });
  fireEvent.change(within(panel).getByLabelText("Default payment instructions"), {
    target: { value: "Updated bank" },
  });
  fireEvent.change(within(panel).getByLabelText("Payment terms (days)"), {
    target: { value: "365" },
  });
  await userEvent.click(within(panel).getByRole("button", { name: "Save template" }));
  await waitFor(() => expect(calls.writes).toBe(1));
  expect(await within(panel).findByText("Company template saved.")).toBeTruthy();
  expect(screen.getByLabelText("Payment instructions")).toHaveProperty("value", "Original bank");
});

it("uploads local logo bytes, fetches the saved asset and explicitly removes invoice branding", async () => {
  const image = pngLogo();
  const content = Uint8Array.from(atob(image.dataBase64), (character) => character.charCodeAt(0));
  const logoAssetId = "01900f3c-7b2e-7cc4-98c4-dc0c0c073905";
  const logo = create(InvoiceLogoSchema, {
    id: logoAssetId,
    organisationId: workspace.organisationId,
    mimeType: image.mimeType,
    byteLength: content.byteLength,
    width: image.width,
    height: image.height,
    sha256: new Uint8Array(32),
  });
  const savedLogos: (string | undefined)[] = [];
  let uploads = 0;
  let template = create(InvoiceTemplateSchema, {
    organisationId: workspace.organisationId,
    version: 0n,
    defaults: { paymentTermsDays: 14 },
  });
  const conveyor = {
    accounting: {
      listInvoices: async () => ({
        bytes: toBinary(ListInvoicesResponseSchema, create(ListInvoicesResponseSchema)),
      }),
      getInvoiceTemplate: async () =>
        getTemplate.encodeResponse(create(GetInvoiceTemplateResponseSchema, { template })),
      saveInvoiceTemplate: async (frame: Parameters<typeof saveTemplate.decodeRequest>[0]) => {
        const request = saveTemplate.decodeRequest(frame);
        expect(request.defaults?.logoAssetId).toBe(logoAssetId);
        template = create(InvoiceTemplateSchema, {
          organisationId: workspace.organisationId,
          version: 1n,
          defaults: request.defaults,
        });
        return saveTemplate.encodeResponse(create(SaveInvoiceTemplateResponseSchema, { template }));
      },
      uploadInvoiceLogo: async (frame: Parameters<typeof uploadLogo.decodeRequest>[0]) => {
        const request = uploadLogo.decodeRequest(frame);
        expect(request.organisationId).toBe(workspace.organisationId);
        expect(request.commandContext?.authentication).toMatchObject({
          actorUserId: workspace.userId,
          sessionId: workspace.sessionId,
        });
        expect(request.commandContext?.idempotencyKey).toMatch(/^[0-9a-f-]{14}7[0-9a-f-]+$/);
        expect(request.mimeType).toBe("image/png");
        expect(request.content).toEqual(content);
        uploads += 1;
        return uploadLogo.encodeResponse(create(UploadInvoiceLogoResponseSchema, { logo }));
      },
      getInvoiceLogo: async (frame: Parameters<typeof getLogo.decodeRequest>[0]) => {
        const request = getLogo.decodeRequest(frame);
        expect(request.organisationId).toBe(workspace.organisationId);
        expect(request.authentication).toMatchObject({
          actorUserId: workspace.userId,
          sessionId: workspace.sessionId,
        });
        expect(request.logoAssetId).toBe(logoAssetId);
        return getLogo.encodeResponse(create(GetInvoiceLogoResponseSchema, { logo, content }));
      },
      saveInvoiceDraft: async (frame: Parameters<typeof save.decodeRequest>[0]) => {
        const request = save.decodeRequest(frame);
        savedLogos.push(request.logoAssetId);
        expect(request.organisationId).toBe(workspace.organisationId);
        return save.encodeResponse(
          create(SaveInvoiceDraftResponseSchema, {
            invoice: create(InvoiceSchema, {
              id: "01900f3c-7b2e-7cc4-98c4-dc0c0c073906",
              version: request.expectedVersion + 1n,
              state: InvoiceState.DRAFT,
              sellerName: "Gamma Systems Pty Ltd",
              sellerAbn: "51824753556",
              draft: request.draft,
              ...(request.logoAssetId ? { logoAssetId: request.logoAssetId } : {}),
            }),
          }),
        );
      },
    },
    documents: {
      listDocuments: async () => ({
        bytes: toBinary(ListDocumentsResponseSchema, create(ListDocumentsResponseSchema)),
      }),
    },
  } as unknown as TammyConveyor;
  render(<ConnectedInvoicesScreen conveyor={conveyor} workspace={workspace} />);
  await userEvent.click(screen.getByRole("button", { name: "Manage invoice template" }));
  const panel = screen.getByRole("region", { name: "Company invoice template" });
  await waitFor(() =>
    expect(within(panel).getByLabelText("Payment terms (days)")).toHaveProperty("value", "14"),
  );
  fireEvent.change(within(panel).getByLabelText("Company logo"), {
    target: { files: [new File([content], "company.png", { type: "image/png" })] },
  });
  const preview = await within(panel).findByRole("img", { name: "Company logo preview" });
  expect(preview.getAttribute("src")).toBe(`data:image/png;base64,${image.dataBase64}`);
  expect(uploads).toBe(1);
  await userEvent.click(within(panel).getByRole("button", { name: "Save template" }));
  await within(panel).findByText("Company template saved.");
  await userEvent.click(within(panel).getByRole("button", { name: "Apply saved template" }));
  await screen.findByRole("img", { name: "Invoice logo" });
  fireEvent.change(screen.getByLabelText("Description 1"), { target: { value: "Consulting" } });
  await userEvent.click(screen.getByRole("button", { name: "Save draft" }));
  await screen.findByText("Draft saved.");
  expect(savedLogos).toEqual([logoAssetId]);
  await userEvent.click(screen.getByRole("button", { name: "Remove invoice logo" }));
  expect(screen.queryByRole("img", { name: "Invoice logo" })).toBeNull();
  expect(screen.getByRole("button", { name: "Export PDF" })).toHaveProperty("disabled", true);
  await userEvent.click(screen.getByRole("button", { name: "Save draft" }));
  await screen.findByText("Draft saved.");
  expect(savedLogos).toEqual([logoAssetId, ""]);
  expect(screen.getByRole("button", { name: "Export PDF" })).toHaveProperty("disabled", false);
});
