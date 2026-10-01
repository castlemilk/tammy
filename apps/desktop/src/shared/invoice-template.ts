import type { InvoiceLogo, InvoiceTemplate } from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { type InvoicePdfLogo, validateInvoicePdfLogo } from "@tammy/invoices/pdf-policy";

const UUID_V7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u;

export interface InvoiceTemplateDefaults {
  readonly sellerAddress: string;
  readonly paymentInstructions: string;
  readonly notes: string;
  readonly paymentTermsDays: number;
  readonly logoAssetId?: string;
}

export interface CompanyInvoiceTemplate {
  readonly version: number;
  readonly defaults: InvoiceTemplateDefaults;
}

export interface InvoiceLogoPreview {
  readonly id: string;
  readonly dataUrl: string;
}

export function invoiceTemplateFromProto(
  value: InvoiceTemplate | undefined,
  organisationId: string,
): CompanyInvoiceTemplate {
  const defaults = value?.defaults;
  if (
    !value ||
    value.organisationId !== organisationId ||
    !defaults ||
    value.version < 0n ||
    value.version > BigInt(Number.MAX_SAFE_INTEGER) ||
    !validText(defaults.sellerAddress, 1000) ||
    !validText(defaults.paymentInstructions, 1000) ||
    !validText(defaults.notes, 2000) ||
    !Number.isInteger(defaults.paymentTermsDays) ||
    defaults.paymentTermsDays < 0 ||
    defaults.paymentTermsDays > 365 ||
    (defaults.logoAssetId !== undefined && !UUID_V7.test(defaults.logoAssetId))
  )
    throw new Error("INVALID_INVOICE_TEMPLATE");
  return {
    version: Number(value.version),
    defaults: {
      sellerAddress: defaults.sellerAddress,
      paymentInstructions: defaults.paymentInstructions,
      notes: defaults.notes,
      paymentTermsDays: defaults.paymentTermsDays,
      ...(defaults.logoAssetId ? { logoAssetId: defaults.logoAssetId } : {}),
    },
  };
}

export function invoiceLogoFromProto(
  logo: InvoiceLogo | undefined,
  content: Uint8Array,
  organisationId: string,
  logoAssetId: string,
): InvoicePdfLogo {
  if (
    !logo ||
    logo.organisationId !== organisationId ||
    logo.id !== logoAssetId ||
    !UUID_V7.test(logo.id) ||
    !(content instanceof Uint8Array) ||
    content.byteLength < 1 ||
    content.byteLength > 1048576 ||
    content.byteLength !== logo.byteLength ||
    logo.sha256.byteLength !== 32
  )
    throw new Error("INVALID_INVOICE_LOGO");
  let binary = "";
  for (let offset = 0; offset < content.length; offset += 32768)
    binary += String.fromCharCode(...content.subarray(offset, offset + 32768));
  return validateInvoicePdfLogo({
    mimeType: logo.mimeType,
    dataBase64: btoa(binary),
    width: logo.width,
    height: logo.height,
  });
}

export function invoiceLogoToPreview(
  logo: InvoiceLogo | undefined,
  content: Uint8Array,
  organisationId: string,
  logoAssetId: string,
): InvoiceLogoPreview {
  const image = invoiceLogoFromProto(logo, content, organisationId, logoAssetId);
  return { id: logoAssetId, dataUrl: `data:${image.mimeType};base64,${image.dataBase64}` };
}

function validText(value: unknown, maximum: number): value is string {
  return typeof value === "string" && value.isWellFormed() && Array.from(value).length <= maximum;
}
