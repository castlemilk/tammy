// @vitest-environment node
import { create } from "@bufbuild/protobuf";
import { createRouterTransport } from "@connectrpc/connect";
import {
  GetInvoiceRequestSchema,
  InvoiceSchema,
  InvoiceService,
  IssueInvoiceRequestSchema,
  ListInvoicesRequestSchema,
  SaveInvoiceDraftRequestSchema,
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
    expect(calls).toEqual(["secret"]);
  });
});
