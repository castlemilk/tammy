import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const BUNDLE = "services/core/internal/artefacts/bundles/au_gst_v1.pb.json";

test("Windows checkout preserves the authenticated embedded GST bundle bytes", async (context) => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-gst-checkout-"));
  context.after(() => rm(root, { recursive: true, force: true }));
  const bytes = await readFile(path.join(ROOT, BUNDLE));
  const expected = (
    await readFile(path.join(ROOT, BUNDLE.replace(".pb.json", ".sha256")), "utf8")
  ).trim();
  assert.equal(createHash("sha256").update(bytes).digest("hex"), expected);
  await mkdir(path.dirname(path.join(root, BUNDLE)), { recursive: true });
  await writeFile(path.join(root, BUNDLE), bytes);
  await writeFile(
    path.join(root, ".gitattributes"),
    await readFile(path.join(ROOT, ".gitattributes")),
  );
  const git = (args, input) =>
    execFileSync("git", args, {
      cwd: root,
      input,
      maxBuffer: 64 * 1024,
      windowsHide: true,
    });
  git(["init", "--quiet", "--template="]);
  git(["-c", "core.autocrlf=true", "add", "--", ".gitattributes", BUNDLE]);
  const checkout = path.join(root, "windows-checkout");
  await mkdir(checkout);
  git(
    ["-c", "core.autocrlf=true", "checkout-index", `--prefix=${checkout}/`, "-z", "--stdin"],
    `${BUNDLE}\0`,
  );
  const checkedOut = await readFile(path.join(checkout, BUNDLE));
  assert.deepEqual(checkedOut, bytes);
  assert.equal(createHash("sha256").update(checkedOut).digest("hex"), expected);
});
