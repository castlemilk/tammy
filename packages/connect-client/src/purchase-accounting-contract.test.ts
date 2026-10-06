import { create, type DescMessage, fromBinary, toBinary } from "@bufbuild/protobuf";
import {
  file_tammy_v1_purchases,
  PurchaseService,
} from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import { BasWorkpaperSchema } from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { describe, expect, test } from "vitest";

describe("purchase generated contracts", () => {
  test("round-trips saved per-portion future claims and globally bounded legacy support", () => {
    const schema: DescMessage = BasWorkpaperSchema;
    const workpaper = populated(schema);
    expect(fromBinary(schema, toBinary(schema, workpaper))).toEqual(workpaper);
    const review = BasWorkpaperSchema.fields.find((field) => field.name === "purchase_reviews");
    expect(review?.fieldKind).toBe("list");
    if (review?.fieldKind !== "list" || review.listKind !== "message")
      throw new Error("Missing typed reviews");
    const future = review.message.fields.find((field) => field.name === "future_claim_obligation");
    expect(future?.fieldKind).toBe("message");
    if (future?.fieldKind !== "message") throw new Error("Missing saved future claim");
    expect(future.message.fields.some((field) => field.name === "portion")).toBe(true);
    expect(future.message.fields.some((field) => field.fieldKind === "list")).toBe(false);
    expect(
      BasWorkpaperSchema.fields.find((field) => field.name === "legacy_purchase_support")?.number,
    ).toBe(23);
  });

  test("exposes exactly the eight approved unary RPCs", () => {
    expect(PurchaseService.methods.map((method) => method.name)).toEqual([
      "GetPurchaseAccounting",
      "PostReviewedPurchase",
      "RecordPurchasePayment",
      "ListPurchasePayments",
      "RecordPurchaseTaxEvidence",
      "AttributePurchaseGst",
      "DeferPurchaseGstClaim",
      "ResolveLegacyPurchaseReview",
    ]);
  });

  for (const method of PurchaseService.methods) {
    for (const schema of [method.input, method.output]) {
      test(`${schema.typeName} retains exact fields through binary encoding`, () => {
        const message = populated(schema);
        expect(fromBinary(schema, toBinary(schema, message))).toEqual(message);
      });
    }
  }

  test("uses typed, bounded messages without original evidence or snapshot bytes", () => {
    for (const message of file_tammy_v1_purchases.messages) {
      for (const field of message.fields) {
        expect(field.fieldKind).not.toBe("map");
        expect(["original_content", "snapshot_bytes", "document_copy"]).not.toContain(field.name);
        if (field.fieldKind === "message") {
          expect([
            "google.protobuf.Any",
            "google.protobuf.Struct",
            "google.protobuf.Value",
          ]).not.toContain(field.message.typeName);
        }
      }
    }
    const post = PurchaseService.method.postReviewedPurchase.input;
    expect(post.fields.map((field) => field.name)).toContain("expected_reviewed_version");
    const attribution = PurchaseService.method.attributePurchaseGst.input;
    expect(attribution.fields.map((field) => field.name)).not.toContain("amount");
  });
});

// Nonempty fixtures exercise nested messages, oneofs, optional fields, int64 and hashes.
function populated(schema: DescMessage): ReturnType<typeof create> {
  const input: Record<string, unknown> = {};
  const selectedOneofs = new Set<string>();
  for (const field of schema.fields) {
    if (field.oneof) {
      if (selectedOneofs.has(field.oneof.localName)) continue;
      selectedOneofs.add(field.oneof.localName);
    }
    let value: unknown;
    const scalar = (type: number): unknown => {
      if (type === 9)
        return field.name === "currency_code" ? "AUD" : "018f0c4a-7b9d-7abc-8def-0123456789ab";
      if (type === 12) return new Uint8Array(32).fill(42);
      if (type === 8) return true;
      if ([3, 4, 6, 16, 18].includes(type)) return 9007199254740993n;
      return 1;
    };
    if (field.fieldKind === "message") value = populated(field.message);
    else if (field.fieldKind === "enum") value = field.enum.values[1]?.number ?? 1;
    else if (field.fieldKind === "scalar") value = scalar(field.scalar);
    else if (field.fieldKind === "list") {
      value = [
        field.listKind === "message"
          ? populated(field.message)
          : field.listKind === "enum"
            ? (field.enum.values[1]?.number ?? 1)
            : scalar(field.scalar),
      ];
    } else throw new Error(`Unexpected map ${field.name}`);
    if (field.oneof) input[field.oneof.localName] = { case: field.localName, value };
    else input[field.localName] = value;
  }
  return create(schema, input);
}
