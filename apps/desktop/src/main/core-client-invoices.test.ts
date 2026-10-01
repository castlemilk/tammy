// @vitest-environment node
import { create } from "@bufbuild/protobuf";
import { createRouterTransport } from "@connectrpc/connect";
import {
  GetInvoiceLogoRequestSchema,
  GetInvoiceRequestSchema,
  GetInvoiceTemplateRequestSchema,
  InvoiceLogoSchema,
  InvoiceSchema,
  InvoiceService,
  InvoiceTemplateSchema,
  IssueInvoiceRequestSchema,
  ListInvoicesRequestSchema,
  SaveInvoiceDraftRequestSchema,
  SaveInvoiceTemplateRequestSchema,
  UploadInvoiceLogoRequestSchema,
} from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { describe, expect, it } from "vitest";
import { createCoreClient } from "./core-client";

describe("invoice Core client", () => {
  it("calls all typed invoice RPCs with the private Core capability", async () => {
    const calls: string[] = [];
    const invoice = create(InvoiceSchema, {
      id: "01900000-0000-7000-8000-000000000001",
      version: 1n,
    });
    const template = create(InvoiceTemplateSchema, { version: 2n });
    const logo = create(InvoiceLogoSchema, { id: "01900000-0000-7000-8000-000000000011" });
    const client = createCoreClient(
      { protocol: "tammy-core-ready-v1", port: 43219, caPem: "TEST", capability: "secret" },
      (options) =>
        createRouterTransport(
          (router) =>
            router.service(InvoiceService, {
              saveInvoiceDraft: (_input, context) => {
                calls.push(context.requestHeader.get("X-Tammy-Capability") ?? "");
                return { invoice };
              },
              getInvoice: () => ({ invoice }),
              listInvoices: () => ({ invoices: [invoice] }),
              issueInvoice: () => ({ invoice }),
              getInvoiceTemplate: () => ({ template }),
              saveInvoiceTemplate: () => ({ template }),
              uploadInvoiceLogo: (_input, context) => {
                calls.push(context.requestHeader.get("X-Tammy-Capability") ?? "");
                return { logo };
              },
              getInvoiceLogo: () => ({ logo, content: new Uint8Array([1, 2, 3]) }),
            }),
          { transport: { interceptors: options.interceptors ?? [] } },
        ),
    );
    expect((await client.saveInvoiceDraft(create(SaveInvoiceDraftRequestSchema))).invoice?.id).toBe(
      invoice.id,
    );
    expect((await client.getInvoice(create(GetInvoiceRequestSchema))).invoice?.id).toBe(invoice.id);
    expect((await client.listInvoices(create(ListInvoicesRequestSchema))).invoices).toHaveLength(1);
    expect((await client.issueInvoice(create(IssueInvoiceRequestSchema))).invoice?.id).toBe(
      invoice.id,
    );
    expect(
      (await client.getInvoiceTemplate(create(GetInvoiceTemplateRequestSchema))).template?.version,
    ).toBe(2n);
    expect(
      (await client.saveInvoiceTemplate(create(SaveInvoiceTemplateRequestSchema))).template
        ?.version,
    ).toBe(2n);
    expect((await client.uploadInvoiceLogo(create(UploadInvoiceLogoRequestSchema))).logo?.id).toBe(
      logo.id,
    );
    expect((await client.getInvoiceLogo(create(GetInvoiceLogoRequestSchema))).content).toEqual(
      new Uint8Array([1, 2, 3]),
    );
    expect(calls).toEqual(["secret", "secret"]);
  });
});
