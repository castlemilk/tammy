import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { notoSansBold } from "./fonts/noto-sans-bold.ts";
import { notoSansRegular } from "./fonts/noto-sans-regular.ts";

function tables(bytes: Buffer): Map<string, Buffer> {
  const result = new Map<string, Buffer>();
  for (let index = 0; index < bytes.readUInt16BE(4); index++) {
    const entry = 12 + index * 16,
      tag = bytes.toString("ascii", entry, entry + 4);
    const offset = bytes.readUInt32BE(entry + 8),
      length = bytes.readUInt32BE(entry + 12);
    assert.ok(offset + length <= bytes.length);
    result.set(tag, Buffer.from(bytes.subarray(offset, offset + length)));
  }
  return result;
}

test("report font derivatives preserve every glyph and metric table while disabling substitution", () => {
  for (const [weight, data, digest] of [
    [
      "Regular",
      notoSansRegular,
      "478c558ea716033cd60c03438f628dfa75694dcf6b5f6d505a2f05fd2b4f3823",
    ],
    ["Bold", notoSansBold, "1df075a380fc7cb898acf64c1f7b3b4dd780de3caa860178bf929de35817a913"],
  ] as const) {
    const original = readFileSync(new URL(`../fonts/NotoSans-${weight}.ttf`, import.meta.url));
    assert.equal(createHash("sha256").update(original).digest("hex"), digest);
    assert.ok(data.startsWith("data:font/ttf;base64,"));
    const generated = Buffer.from(data.slice(data.indexOf(",") + 1), "base64");
    const before = tables(original),
      after = tables(generated);
    const names = after.get("name");
    assert.ok(names);
    const readableNames = Buffer.from(names).swap16().toString("utf16le");
    assert.ok(readableNames.includes("Copyright 2022 The Noto Project Authors"));
    assert.ok(readableNames.includes("SIL Open Font License, Version 1.1"));
    assert.ok(before.has("GSUB"));
    assert.equal(after.has("GSUB"), false);
    assert.equal(after.size, before.size - 1);
    for (const [tag, originalTable] of before) {
      if (tag === "GSUB") continue;
      const generatedTable = after.get(tag);
      assert.ok(generatedTable, `missing ${tag}`);
      if (tag === "head") {
        originalTable.fill(0, 8, 12);
        generatedTable.fill(0, 8, 12);
      }
      assert.deepEqual(generatedTable, originalTable, `${weight} ${tag} table must remain exact`);
    }
    let checksum = 0;
    for (let offset = 0; offset < generated.length; offset += 4)
      checksum = (checksum + generated.readUInt32BE(offset)) >>> 0;
    assert.equal(checksum, 0xb1b0afba, "valid TrueType whole-font checksum");
  }
});

test("report font modules regenerate exactly from pinned original files", () => {
  execFileSync(process.execPath, [
    fileURLToPath(new URL("../scripts/generate-fonts.mjs", import.meta.url)),
    "--check",
  ]);
});
