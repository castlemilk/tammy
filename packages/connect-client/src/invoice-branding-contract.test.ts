import { create, fromBinary, toBinary } from "@bufbuild/protobuf";
import {
  InvoiceDraftSchema,
  InvoiceSchema,
  InvoiceService,
  SaveInvoiceDraftRequestSchema,
} from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { describe, expect, test } from "vitest";

describe("invoice branding generated contracts", () => {
  test("preserves absent, removed and referenced logo intent on the wire", () => {
    for (const logoAssetId of [undefined, "", "018f0000-0000-7000-8000-000000000050"]) {
      const request = create(SaveInvoiceDraftRequestSchema, { logoAssetId });
      const decoded = fromBinary(
        SaveInvoiceDraftRequestSchema,
        toBinary(SaveInvoiceDraftRequestSchema, request),
      );
      expect(decoded.logoAssetId).toBe(logoAssetId);
    }
  });

  test("keeps branding on the saved snapshot while preserving the draft proposal schema", () => {
    expect(InvoiceSchema.fields.find((field) => field.name === "logo_asset_id")?.number).toBe(15);
    expect(InvoiceDraftSchema.fields.map((field) => field.name)).not.toContain("logo_asset_id");
    expect(InvoiceSchema.fields.map((field) => field.name)).not.toContain("content");
    expect(InvoiceService.methods.map((method) => method.name)).toEqual([
      "SaveInvoiceDraft",
      "GetInvoice",
      "ListInvoices",
      "IssueInvoice",
      "GetInvoiceTemplate",
      "SaveInvoiceTemplate",
      "UploadInvoiceLogo",
      "GetInvoiceLogo",
    ]);
  });
});
