import { create } from "@bufbuild/protobuf";
import type { AuthenticationContext } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { GetInvoiceLogoRequestSchema } from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { invoiceLogoFromProto } from "../shared/invoice-template";
import type { CoreClient } from "./core-client";

/** Resolve only the saved immutable asset, checking authority around the read. */
export async function loadInvoiceLogo(
  client: Pick<CoreClient, "getInvoiceLogo">,
  authentication: AuthenticationContext,
  organisationId: string,
  logoAssetId: string | undefined,
  guard: () => void,
) {
  guard();
  if (!logoAssetId) return undefined;
  const result = await client.getInvoiceLogo(
    create(GetInvoiceLogoRequestSchema, {
      authentication,
      organisationId,
      logoAssetId,
    }),
  );
  guard();
  return invoiceLogoFromProto(result.logo, result.content, organisationId, logoAssetId);
}
