// @vitest-environment node
import { create } from "@bufbuild/protobuf";
import {
  GetInvoiceLogoResponseSchema,
  GetInvoiceResponseSchema,
  InvoiceLogoSchema,
  InvoiceSchema,
  InvoiceState,
  IssueInvoiceRequestSchema,
  IssueInvoiceResponseSchema,
} from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { renderInvoicePdf } from "@tammy/invoices/pdf";
import { describe, expect, it, vi } from "vitest";
import { pngLogo } from "../../../../packages/invoices/test-support/logo-fixtures";
import { invoiceDraftToProto } from "../shared/invoices";
import { createInvoiceIssuer } from "./invoice-issue";

function fixture(customerName = "Customer") {
  const organisationId = "01900000-0000-7000-8000-000000000001";
  const invoice = create(InvoiceSchema, {
    id: "01900000-0000-7000-8000-000000000002",
    organisationId,
    state: InvoiceState.DRAFT,
    version: 1n,
    sellerName: "Original seller",
    sellerAbn: "51824753556",
    draft: invoiceDraftToProto({
      number: "INV-001",
      issueDate: "2026-09-30",
      dueDate: "2026-10-14",
      customerName,
      customerAddress: "1 Customer Street",
      sellerAddress: "1 Seller Street",
      gstRegistered: false,
      notes: "",
      paymentInstructions: "Bank transfer",
      lines: [
        {
          description: "Services",
          quantityMilli: 1000,
          unitPriceMinor: 10000,
          taxTreatment: "out-of-scope",
        },
      ],
    }),
    subtotalMinor: 10000n,
    totalMinor: 10000n,
  });
  const client = {
    getInvoiceLogo: vi.fn(async () => create(GetInvoiceLogoResponseSchema)),
    getInvoice: vi.fn(async () => create(GetInvoiceResponseSchema, { invoice })),
    issueInvoice: vi.fn(async () =>
      create(IssueInvoiceResponseSchema, {
        invoice: create(InvoiceSchema, { ...invoice, state: InvoiceState.ISSUED, version: 2n }),
      }),
    ),
  };
  const guard = vi.fn();
  const capture = vi.fn(() => guard);
  const render = vi.fn(renderInvoicePdf);
  const issue = createInvoiceIssuer({ client, capture, render });
  const request = create(IssueInvoiceRequestSchema, {
    organisationId,
    invoiceId: invoice.id,
    expectedVersion: 1n,
    commandContext: {
      authentication: {
        actorUserId: "01900000-0000-7000-8000-000000000003",
        sessionId: "01900000-0000-7000-8000-000000000004",
      },
      idempotencyKey: "01900000-0000-7000-8000-000000000005",
    },
  });
  return { invoice, client, capture, guard, render, issue, request };
}

describe("native invoice issue preflight", () => {
  it("renders the immutable saved logo without consulting current company defaults", async () => {
    const { invoice, client, request, render, issue } = fixture();
    const logo = pngLogo();
    invoice.logoAssetId = "01900000-0000-7000-8000-000000000006";
    const content = new Uint8Array(Buffer.from(logo.dataBase64, "base64"));
    client.getInvoiceLogo.mockResolvedValue(
      create(GetInvoiceLogoResponseSchema, {
        logo: create(InvoiceLogoSchema, {
          id: invoice.logoAssetId,
          organisationId: invoice.organisationId,
          mimeType: logo.mimeType,
          width: logo.width,
          height: logo.height,
          byteLength: content.length,
          sha256: new Uint8Array(32),
        }),
        content,
      }),
    );
    await issue(request);
    expect(client.getInvoiceLogo).toHaveBeenCalledWith(
      expect.objectContaining({
        logoAssetId: invoice.logoAssetId,
        organisationId: invoice.organisationId,
        authentication: request.commandContext?.authentication,
      }),
    );
    expect(render).toHaveBeenCalledWith(expect.objectContaining({ logo }));
  });
  it("does not issue when the saved logo is unavailable or the session changes during retrieval", async () => {
    const { invoice, client, request, render, issue, guard } = fixture();
    invoice.logoAssetId = "01900000-0000-7000-8000-000000000006";
    await expect(issue(request)).rejects.toThrow();
    expect(render).not.toHaveBeenCalled();
    client.getInvoiceLogo.mockImplementation(async () => {
      guard.mockImplementation(() => {
        throw new Error("LOCAL_SESSION_REQUIRED");
      });
      return create(GetInvoiceLogoResponseSchema);
    });
    await expect(issue(request)).rejects.toThrow("LOCAL_SESSION_REQUIRED");
    expect(client.issueInvoice).not.toHaveBeenCalled();
  });
  it("renders the persisted draft and reviewed seller before issuing the unchanged command", async () => {
    const { issue, request, client, render } = fixture();
    expect((await issue(request)).invoice?.state).toBe(InvoiceState.ISSUED);
    expect(render).toHaveBeenCalledWith(
      expect.objectContaining({
        sellerName: "Original seller",
        customerName: "Customer",
        state: "issued",
      }),
    );
    expect(client.issueInvoice).toHaveBeenCalledWith(request);
    expect(render.mock.invocationCallOrder[0]).toBeLessThan(
      client.issueInvoice.mock.invocationCallOrder[0] ?? 0,
    );
  });
  it("keeps an unsupported-text draft editable rather than freezing an unexportable invoice", async () => {
    const { issue, request, client } = fixture("王伟");
    await expect(issue(request)).rejects.toThrow("U+738B");
    expect(client.issueInvoice).not.toHaveBeenCalled();
  });
  it("rejects a session transition during rendering before any persistent command", async () => {
    const { issue, request, client, render, guard } = fixture();
    render.mockImplementation(async () => {
      guard.mockImplementation(() => {
        throw new Error("LOCAL_SESSION_REQUIRED");
      });
      return new Uint8Array();
    });
    await expect(issue(request)).rejects.toThrow("LOCAL_SESSION_REQUIRED");
    expect(client.issueInvoice).not.toHaveBeenCalled();
  });
  it("rejects cross-organisation records and absent authentication without issuing", async () => {
    const { issue, request, invoice, client } = fixture();
    await expect(
      issue(create(IssueInvoiceRequestSchema, { ...request, commandContext: undefined })),
    ).rejects.toThrow();
    invoice.organisationId = "01900000-0000-7000-8000-000000000099";
    await expect(issue(request)).rejects.toThrow();
    expect(client.issueInvoice).not.toHaveBeenCalled();
  });
});
