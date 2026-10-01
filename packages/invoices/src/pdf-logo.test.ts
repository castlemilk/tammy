import assert from "node:assert/strict";
import test from "node:test";
import { renderDocumentWithLayout } from "@formepdf/core";
import { findElements } from "@formepdf/core/layout";
import { Document, Page } from "@formepdf/react";
import { createElement as h } from "react";
import { jpegLogo, pngChunk, pngLogo } from "../test-support/logo-fixtures.ts";
import * as policy from "./pdf-policy.ts";
import { PageHeader } from "./pdfcn/components.ts";

function validate(value: unknown) {
  const validator = (policy as Record<string, unknown>).validateInvoicePdfLogo;
  assert.equal(typeof validator, "function", "logo validation must be available");
  return (validator as (value: unknown) => unknown)(value);
}

test("validates canonical local PNG and JPEG logo bytes with their actual dimensions", () => {
  for (const logo of [pngLogo(), jpegLogo()]) assert.deepEqual(validate(logo), logo);
});

test("rejects remote, malformed, non-canonical, oversized and misleading logos before rendering", () => {
  const png = pngLogo();
  const jpeg = jpegLogo();
  const pngBytes = Buffer.from(png.dataBase64, "base64");
  const jpegBytes = Buffer.from(jpeg.dataBase64, "base64");
  const brokenCrc = Buffer.from(pngBytes);
  brokenCrc[29] = (brokenCrc[29] ?? 0) ^ 1;
  const oversizedHeader = Buffer.from(pngBytes.subarray(16, 29));
  oversizedHeader.writeUInt32BE(4096, 0);
  oversizedHeader.writeUInt32BE(1025, 4);
  const oversizedPixels = Buffer.concat([
    pngBytes.subarray(0, 8),
    pngChunk("IHDR", oversizedHeader),
    pngBytes.subarray(33),
  ]);
  const invalidValues: readonly unknown[] = [
    undefined,
    null,
    "https://example.com/logo.png",
    { ...png, src: "https://example.com/logo.png" },
    { ...png, mimeType: "image/svg+xml" },
    { ...png, mimeType: "image/jpeg" },
    { ...jpeg, mimeType: "image/png" },
    { ...png, dataBase64: "" },
    { ...png, dataBase64: `data:image/png;base64,${png.dataBase64}` },
    { ...png, dataBase64: "https://example.com/logo.png" },
    { ...png, dataBase64: "/absolute/logo.png" },
    { ...png, dataBase64: `${png.dataBase64}\n` },
    { ...jpeg, dataBase64: jpeg.dataBase64.replace(/=+$/, "") },
    { ...png, dataBase64: "iVBORw0KGgo=" },
    { ...png, dataBase64: brokenCrc.toString("base64") },
    { ...png, dataBase64: pngBytes.subarray(0, -8).toString("base64") },
    { ...png, dataBase64: Buffer.concat([pngBytes, Buffer.from("trailing")]).toString("base64") },
    { ...jpeg, dataBase64: jpegBytes.subarray(0, -2).toString("base64") },
    { ...jpeg, dataBase64: Buffer.concat([jpegBytes, Buffer.from("trailing")]).toString("base64") },
    { ...png, dataBase64: Buffer.alloc(1024 * 1024 + 1).toString("base64") },
    { ...png, width: 239 },
    { ...jpeg, height: 95 },
    { ...png, width: 0 },
    { ...png, height: -1 },
    { ...png, width: 1.5 },
    { ...png, height: Number.NaN },
    pngLogo(4097, 1),
    { ...png, dataBase64: oversizedPixels.toString("base64"), width: 4096, height: 1025 },
  ];
  for (const [index, value] of invalidValues.entries())
    assert.throws(() => validate(value), /logo:/, `accepted invalid logo ${index}`);
});

test("PDF readiness validates optional logos before the issue boundary", () => {
  const logo = pngLogo();
  assert.throws(
    () =>
      policy.assertInvoicePdfReady(
        {
          number: "INV-1",
          issueDate: "",
          dueDate: "",
          customerName: "",
          customerAddress: "",
          sellerAddress: "",
          gstRegistered: true,
          notes: "",
          paymentInstructions: "",
          lines: [],
        },
        "Gamma Systems",
        { ...logo, width: logo.width + 1 },
      ),
    /logo:/,
  );
});

test("rejects a JPEG whose scan has no encoded image data", () => {
  const logo = jpegLogo();
  const bytes = Buffer.from(logo.dataBase64, "base64");
  const scan = bytes.indexOf(Buffer.from([0xff, 0xda]));
  assert.ok(scan > 0);
  const headerEnd = scan + 2 + bytes.readUInt16BE(scan + 2);
  const emptyScan = Buffer.concat([bytes.subarray(0, headerEnd), Buffer.from([0xff, 0xd9])]);
  assert.throws(() => validate({ ...logo, dataBase64: emptyScan.toString("base64") }), /logo:/);
});

test("contains wide and tall logos in the header without stretching their aspect ratios", async () => {
  for (const logo of [pngLogo(240, 96), pngLogo(48, 192)]) {
    const result = await renderDocumentWithLayout(
      h(
        Document,
        {},
        h(
          Page,
          { size: "A4", margin: 40 },
          h(PageHeader, {
            title: "Tax Invoice",
            subtitle: "Gamma Systems Pty Ltd",
            rightText: "ABN 51824753556",
            logo,
          }),
        ),
      ),
    );
    const images = findElements(result.layout, (element) => element.nodeType === "Image");
    assert.equal(images.length, 1);
    const image = images[0];
    assert.ok(image);
    assert.ok(image.width <= 120 && image.height <= 56);
    assert.equal(image.width / image.height, logo.width / logo.height);
    assert.ok(image.x >= 40 && image.x + image.width <= 555.28);
  }
});
