import { create } from "@bufbuild/protobuf";
import { SourceRefSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { expect, it } from "vitest";

import {
  BUNDLED_TAX_RULE_SOURCE,
  createBundledTaxRuleSourceRef,
  isCurrentBundledTaxRuleSourceRef,
  isKnownLegacyBundledTaxRuleSourceRef,
} from "./bundled-tax-rule";

function bytes(value: string) {
  return Uint8Array.from(value.match(/.{2}/gu) ?? [], (pair) => Number.parseInt(pair, 16));
}

const LEGACY_HASH = "e2f9cde094db43c30260dc54f089a1ab835912e75e8c29add5c7a240e90497e4";

it("builds the canonical bundled rule reference and recognizes only the exact known legacy tuple", () => {
  const current = createBundledTaxRuleSourceRef();
  expect(current).toMatchObject({
    type: BUNDLED_TAX_RULE_SOURCE.type,
    id: BUNDLED_TAX_RULE_SOURCE.id,
    revision: BUNDLED_TAX_RULE_SOURCE.revision,
    contentHash: bytes(BUNDLED_TAX_RULE_SOURCE.contentHashHex),
  });
  expect(isCurrentBundledTaxRuleSourceRef(current)).toBe(true);

  const knownLegacy = create(SourceRefSchema, {
    type: BUNDLED_TAX_RULE_SOURCE.type,
    id: BUNDLED_TAX_RULE_SOURCE.id,
    revision: BUNDLED_TAX_RULE_SOURCE.revision,
    contentHash: bytes(LEGACY_HASH),
  });
  expect(isKnownLegacyBundledTaxRuleSourceRef(knownLegacy)).toBe(true);

  for (const candidate of [
    create(SourceRefSchema, { ...knownLegacy, type: "other_bundle" }),
    create(SourceRefSchema, { ...knownLegacy, id: "018f0000-0000-7000-8000-000000000023" }),
    create(SourceRefSchema, { ...knownLegacy, revision: 2n }),
    create(SourceRefSchema, {
      ...knownLegacy,
      contentHash: bytes(`${LEGACY_HASH.slice(0, 62)}00`),
    }),
    current,
  ]) {
    expect(isKnownLegacyBundledTaxRuleSourceRef(candidate)).toBe(false);
  }
});
