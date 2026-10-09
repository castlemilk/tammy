import assert from "node:assert/strict";
import test from "node:test";
import {
  pdfRetainsMetadata,
  pdfRetainsReviewDocument,
} from "../apps/desktop/tests/e2e/support/purchase-pdf-provenance.ts";

function normalizedPdf(value) {
  return value.replace(/\s+/gu, " ");
}

test("retains wrapped canonical UUID and SHA-256 identities exactly", () => {
  const uuid = "01a1229f-b4b8-72c9-bdb3-88a23482a796";
  const sha256 = "0123456789abcdef".repeat(4);
  const newline = "\n";
  const extracted =
    "actor " +
    uuid.slice(0, 9) +
    newline +
    uuid.slice(9) +
    " · source hash " +
    sha256.slice(0, 32) +
    newline +
    sha256.slice(32);
  const normalized = normalizedPdf(extracted);

  assert.equal(
    normalized.includes(uuid),
    false,
    "the prior normalized-only predicate reproduces the wrap failure",
  );
  assert.equal(pdfRetainsMetadata(normalized, uuid), true);
  assert.equal(pdfRetainsMetadata(normalized, sha256), true);
  assert.equal(pdfRetainsMetadata(normalized, uuid.slice(0, -1)), false);
  assert.equal(pdfRetainsMetadata(normalized, sha256.slice(0, -1)), false);
});

test("rejects canonical UUID substrings with adjacent identifier characters", () => {
  const uuid = "01a1229f-b4b8-72c9-bdb3-88a23482a796";

  for (const malformed of [
    `x${uuid}`,
    `0${uuid}`,
    `${uuid}0`,
    `${uuid}Document`,
    `_${uuid}`,
    `${uuid}-`,
  ]) {
    assert.equal(pdfRetainsMetadata(normalizedPdf(`actor ${malformed}`), uuid), false, malformed);
  }
});

test("rejects SHA-256 substrings with adjacent identifier characters", () => {
  const sha256 = "0123456789abcdef".repeat(4);

  for (const malformed of [
    `0${sha256}`,
    `${sha256}a`,
    `${sha256}Allocation`,
    `_${sha256}`,
    `${sha256}-`,
  ]) {
    assert.equal(
      pdfRetainsMetadata(normalizedPdf(`source hash ${malformed}`), sha256),
      false,
      malformed,
    );
  }
});

test("rejects missing identities and same-length canonical substitutions", () => {
  for (const expected of ["01a1229f-b4b8-72c9-bdb3-88a23482a796", "0123456789abcdef".repeat(4)]) {
    const changed = `${expected.slice(0, -1)}0`;
    assert.equal(changed.length, expected.length);
    assert.equal(pdfRetainsMetadata(normalizedPdf(`Identity ${changed}`), expected), false);
    assert.equal(pdfRetainsMetadata("Identity not retained", expected), false);
    assert.equal(pdfRetainsMetadata(normalizedPdf(`Identity ${expected}`), expected), true);
  }
  const uuid = "01a1229f-b4b8-72c9-bdb3-88a23482a796";
  assert.equal(pdfRetainsReviewDocument(`Review document ${uuid.slice(0, -1)}0`, uuid), false);
});

test("pairs the Review document label with exactly one complete identifier", () => {
  const uuid = "01a1229f-b4b8-72c9-bdb3-88a23482a796";
  const wrappedUuid = `${uuid.slice(0, 9)}\n${uuid.slice(9)}`;

  assert.equal(
    pdfRetainsReviewDocument(normalizedPdf(`Review document ${wrappedUuid}`), uuid),
    true,
  );
  assert.equal(pdfRetainsReviewDocument(normalizedPdf(`Other document ${uuid}`), uuid), false);
  assert.equal(pdfRetainsReviewDocument(normalizedPdf(`Review document x${uuid}`), uuid), false);
  assert.equal(
    pdfRetainsReviewDocument(normalizedPdf(`Review document ${uuid}Allocation`), uuid),
    false,
  );
  assert.equal(
    pdfRetainsReviewDocument(normalizedPdf(`Review document ${uuid.slice(0, -1)}`), uuid),
    false,
  );
  assert.equal(
    pdfRetainsReviewDocument(normalizedPdf("Review document document-42"), "document-42"),
    true,
  );
  assert.equal(
    pdfRetainsReviewDocument(normalizedPdf("Review document document-42Allocation"), "document-42"),
    false,
  );
});

test("keeps prose, dates, amounts, enums, versions, and timestamps on normalized text", () => {
  const extracted = [
    "Reason: claim",
    "was unsupported · date 2026-10-10 · amount $1,234.56",
    "· status needs",
    "timing review · version 7 · recorded 2026-10-10T14:35:12Z",
  ].join("\n");
  const normalized = normalizedPdf(extracted);

  for (const expected of [
    "claim was unsupported",
    "2026-10-10",
    "$1,234.56",
    "needs timing review",
    "version 7",
    "2026-10-10T14:35:12Z",
  ]) {
    assert.equal(pdfRetainsMetadata(normalized, expected), true, expected);
  }
  assert.equal(pdfRetainsMetadata(normalized, "claimwasunsupported"), false);
  assert.equal(pdfRetainsMetadata(normalized, "2026-10-11"), false);
  assert.equal(pdfRetainsMetadata(normalized, "$1,234.57"), false);
  assert.equal(pdfRetainsMetadata(normalized, "needstimingreview"), false);
  assert.equal(pdfRetainsMetadata(normalized, "version 8"), false);
  assert.equal(pdfRetainsMetadata(normalized, "2026-10-10T14:35:13Z"), false);
});
