// @vitest-environment node
import { create } from "@bufbuild/protobuf";
import { createRouterTransport } from "@connectrpc/connect";
import {
  BusinessIncomeSummarySchema,
  GetBusinessIncomeSummaryRequestSchema,
  GetInvoiceAccountingRequestSchema,
  InvoiceAccountingSchema,
  InvoicePaymentSchema,
  InvoiceService,
  PostInvoiceIncomeRequestSchema,
  RecordInvoicePaymentRequestSchema,
} from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { expect, it } from "vitest";
import { createCoreClient } from "./core-client";

it("authenticates typed invoice posting, receipt and business-income RPCs", async () => {
  const capabilities: (string | null)[] = [];
  const accounting = create(InvoiceAccountingSchema, { posted: true, version: 2n });
  const payment = create(InvoicePaymentSchema, { reference: "Customer transfer" });
  const summary = create(BusinessIncomeSummarySchema, { issuedCount: 1n });
  const client = createCoreClient(
    { protocol: "tammy-core-ready-v1", port: 43219, caPem: "TEST", capability: "secret" },
    (options) =>
      createRouterTransport(
        (router) =>
          router.service(InvoiceService, {
            getInvoiceAccounting: (_input, context) => {
              capabilities.push(context.requestHeader.get("X-Tammy-Capability"));
              return { accounting };
            },
            postInvoiceIncome: (_input, context) => {
              capabilities.push(context.requestHeader.get("X-Tammy-Capability"));
              return { accounting };
            },
            recordInvoicePayment: (_input, context) => {
              capabilities.push(context.requestHeader.get("X-Tammy-Capability"));
              return { accounting, payment };
            },
            getBusinessIncomeSummary: (_input, context) => {
              capabilities.push(context.requestHeader.get("X-Tammy-Capability"));
              return { summary };
            },
          }),
        { transport: { interceptors: options.interceptors ?? [] } },
      ),
  );
  expect(
    (await client.getInvoiceAccounting(create(GetInvoiceAccountingRequestSchema))).accounting
      ?.posted,
  ).toBe(true);
  expect(
    (await client.postInvoiceIncome(create(PostInvoiceIncomeRequestSchema))).accounting?.version,
  ).toBe(2n);
  expect(
    (await client.recordInvoicePayment(create(RecordInvoicePaymentRequestSchema))).payment
      ?.reference,
  ).toBe("Customer transfer");
  expect(
    (await client.getBusinessIncomeSummary(create(GetBusinessIncomeSummaryRequestSchema))).summary
      ?.issuedCount,
  ).toBe(1n);
  expect(capabilities).toEqual(["secret", "secret", "secret", "secret"]);
});
