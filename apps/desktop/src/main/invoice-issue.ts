import { create } from "@bufbuild/protobuf";
import type { AuthenticationContext } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  GetInvoiceRequestSchema,
  type IssueInvoiceRequest,
} from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import type { InvoicePdfData } from "@tammy/invoices/pdf";
import { invoiceToRecord } from "../shared/invoices";
import type { CoreClient } from "./core-client";
import { loadInvoiceLogo } from "./invoice-logo";

/** Check the real saved document can produce a customer PDF before freezing it.
 * Core still owns authorisation, versions, idempotency and the issue transaction.
 */
export function createInvoiceIssuer(options: {
  client: Pick<CoreClient, "getInvoice" | "issueInvoice" | "getInvoiceLogo">;
  capture: (authentication: AuthenticationContext, organisationId: string) => () => void;
  render: (invoice: InvoicePdfData) => Promise<Uint8Array>;
}) {
  return async (request: IssueInvoiceRequest) => {
    const authentication = request.commandContext?.authentication;
    if (!authentication) throw new Error("LOCAL_SESSION_REQUIRED");
    const guard = options.capture(authentication, request.organisationId);
    const response = await options.client.getInvoice(
      create(GetInvoiceRequestSchema, {
        authentication,
        organisationId: request.organisationId,
        invoiceId: request.invoiceId,
      }),
    );
    guard();
    if (!response.invoice || response.invoice.organisationId !== request.organisationId)
      throw new Error("INVALID_INVOICE");
    const record = invoiceToRecord(response.invoice);
    const logo = await loadInvoiceLogo(
      options.client,
      authentication,
      request.organisationId,
      record.logoAssetId,
      guard,
    );
    await options.render({
      ...record.draft,
      sellerName: record.sellerName,
      sellerAbn: record.sellerAbn,
      state: "issued",
      ...(logo ? { logo } : {}),
    });
    guard();
    return options.client.issueInvoice(request);
  };
}
