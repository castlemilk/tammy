import assert from "node:assert/strict";
import test from "node:test";
import { validateInvoiceDraft, validateInvoiceForIssue } from "./model.ts";
import { assertInvoicePdfReady, InvoicePdfUnsupportedTextError } from "./pdf-policy.ts";
import { draftFixture } from "./test-fixtures.ts";

test("allows an issued document with supported Latin, Greek and Cyrillic text", () => {
  const draft = validateInvoiceForIssue(
    validateInvoiceDraft({
      ...draftFixture(),
      customerName: "Émilie Müller — Εταιρεία — Компания",
    }),
    { name: "Zoë Núñez", abn: "51824753556" },
  );
  assert.doesNotThrow(() => assertInvoicePdfReady(draft, "Zoë Núñez"));
});

test("blocks structurally valid but unexportable CJK identities before issue without modifying the draft", () => {
  const draft = validateInvoiceForIssue(
    validateInvoiceDraft({ ...draftFixture(), customerName: "公司服务" }),
    { name: "Seller Pty Ltd", abn: "51824753556" },
  );
  assert.throws(
    () => assertInvoicePdfReady(draft, "Seller Pty Ltd"),
    (error: unknown) =>
      error instanceof InvoicePdfUnsupportedTextError &&
      error.field === "customerName" &&
      error.codePoint === 0x516c,
  );
  assert.equal(draft.customerName, "公司服务");
});

test("preflights every customer-visible field but never private source references or costs", () => {
  const fields = [
    "number",
    "customerName",
    "customerAddress",
    "sellerAddress",
    "notes",
    "paymentInstructions",
  ] as const;
  for (const field of fields)
    assert.throws(
      () => assertInvoicePdfReady({ ...draftFixture(), [field]: "中" }, "Seller Pty Ltd"),
      new RegExp(`${field}.*U\\+4E2D`),
    );
  assert.throws(() => assertInvoicePdfReady(draftFixture(), "Seller 😀"), /sellerName.*U\+1F600/);
  const firstLine = draftFixture().lines[0];
  assert.ok(firstLine);
  assert.throws(
    () =>
      assertInvoicePdfReady(
        { ...draftFixture(), lines: [{ ...firstLine, description: "中" }] },
        "Seller Pty Ltd",
      ),
    /lines\[0\]\.description.*U\+4E2D/,
  );
  assert.doesNotThrow(() =>
    assertInvoicePdfReady(
      {
        ...draftFixture(),
        lines: [
          {
            ...firstLine,
            unitCostMinor: 4000,
            sourceDocumentId: "019fdab5-4704-7d72-8d37-0645f45b2ad3",
          },
        ],
      },
      "Seller Pty Ltd",
    ),
  );
});
