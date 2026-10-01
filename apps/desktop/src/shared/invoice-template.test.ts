import { create } from "@bufbuild/protobuf";
import {
  InvoiceLogoSchema,
  InvoiceTemplateSchema,
} from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { describe, expect, it } from "vitest";
import { pngLogo } from "../../../../packages/invoices/test-support/logo-fixtures";
import * as templates from "./invoice-template";

const organisationId = "01900000-0000-7000-8000-000000000001";
const logoAssetId = "01900000-0000-7000-8000-000000000002";

describe("company invoice projections", () => {
  it("projects persisted defaults and rejects stale or malformed organisation data", () => {
    const value = create(InvoiceTemplateSchema, {
      organisationId,
      version: 1n,
      defaults: {
        sellerAddress: "1 Company Street",
        paymentInstructions: "BSB 123-456",
        notes: "Thank you",
        paymentTermsDays: 30,
        logoAssetId,
      },
    });
    if (!value.defaults) throw new Error("Missing fixture defaults");
    expect(templates.invoiceTemplateFromProto(value, organisationId)).toEqual({
      version: 1,
      defaults: {
        sellerAddress: "1 Company Street",
        paymentInstructions: "BSB 123-456",
        notes: "Thank you",
        paymentTermsDays: 30,
        logoAssetId,
      },
    });
    for (const invalid of [
      create(InvoiceTemplateSchema, { ...value, organisationId: logoAssetId }),
      create(InvoiceTemplateSchema, { ...value, version: BigInt(Number.MAX_SAFE_INTEGER) + 1n }),
      create(InvoiceTemplateSchema, { ...value, defaults: undefined }),
      create(InvoiceTemplateSchema, {
        ...value,
        defaults: { ...value.defaults, paymentTermsDays: 366 },
      }),
      create(InvoiceTemplateSchema, {
        ...value,
        defaults: { ...value.defaults, logoAssetId: "https://example.com/logo" },
      }),
    ])
      expect(() => templates.invoiceTemplateFromProto(invalid, organisationId)).toThrow();
  });
  it("builds previews only from validated immutable local logo bytes", () => {
    const image = pngLogo();
    const content = Uint8Array.from(atob(image.dataBase64), (character) => character.charCodeAt(0));
    const logo = create(InvoiceLogoSchema, {
      id: logoAssetId,
      organisationId,
      mimeType: image.mimeType,
      width: image.width,
      height: image.height,
      byteLength: content.byteLength,
      sha256: new Uint8Array(32),
    });
    expect(templates.invoiceLogoFromProto(logo, content, organisationId, logoAssetId)).toEqual(
      image,
    );
    expect(templates.invoiceLogoToPreview(logo, content, organisationId, logoAssetId)).toEqual({
      id: logoAssetId,
      dataUrl: `data:image/png;base64,${image.dataBase64}`,
    });
    expect(() =>
      templates.invoiceLogoFromProto(
        { ...logo, organisationId: logoAssetId },
        content,
        organisationId,
        logoAssetId,
      ),
    ).toThrow();
    expect(() =>
      templates.invoiceLogoFromProto(
        { ...logo, width: image.width + 1 },
        content,
        organisationId,
        logoAssetId,
      ),
    ).toThrow();
    expect(() =>
      templates.invoiceLogoFromProto(
        logo,
        content.subarray(0, content.length - 4),
        organisationId,
        logoAssetId,
      ),
    ).toThrow();
    expect(() =>
      templates.invoiceLogoFromProto(
        { ...logo, sha256: new Uint8Array() },
        content,
        organisationId,
        logoAssetId,
      ),
    ).toThrow();
  });
});
