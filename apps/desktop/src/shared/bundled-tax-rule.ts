import { create } from "@bufbuild/protobuf";
import { type SourceRef, SourceRefSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";

export const BUNDLED_TAX_RULE_SOURCE = Object.freeze({
  type: "tax_rule_bundle",
  id: "018f0000-0000-7000-8000-000000000022",
  revision: 1n,
  contentHashHex: "1da4e89d4fa785da7591070e8a6054f91df5ed02bb4ec10a0c894a15141506de",
});

const KNOWN_LEGACY_BUNDLED_TAX_RULE_HASH =
  "e2f9cde094db43c30260dc54f089a1ab835912e75e8c29add5c7a240e90497e4";

export function createBundledTaxRuleSourceRef(): SourceRef {
  return create(SourceRefSchema, {
    type: BUNDLED_TAX_RULE_SOURCE.type,
    id: BUNDLED_TAX_RULE_SOURCE.id,
    revision: BUNDLED_TAX_RULE_SOURCE.revision,
    contentHash: hexBytes(BUNDLED_TAX_RULE_SOURCE.contentHashHex),
  });
}

export function isCurrentBundledTaxRuleSourceRef(value: SourceRef | undefined): boolean {
  return sameSourceRef(value, BUNDLED_TAX_RULE_SOURCE.contentHashHex);
}

export function isKnownLegacyBundledTaxRuleSourceRef(value: SourceRef | undefined): boolean {
  return sameSourceRef(value, KNOWN_LEGACY_BUNDLED_TAX_RULE_HASH);
}

function sameSourceRef(value: SourceRef | undefined, hashHex: string): boolean {
  return Boolean(
    value &&
      value.type === BUNDLED_TAX_RULE_SOURCE.type &&
      value.id === BUNDLED_TAX_RULE_SOURCE.id &&
      value.revision === BUNDLED_TAX_RULE_SOURCE.revision &&
      bytesToHex(value.contentHash) === hashHex,
  );
}

function hexBytes(value: string): Uint8Array {
  return Uint8Array.from(value.match(/.{2}/gu) ?? [], (pair) => Number.parseInt(pair, 16));
}

function bytesToHex(value: Uint8Array | undefined): string {
  return value ? [...value].map((byte) => byte.toString(16).padStart(2, "0")).join("") : "";
}
