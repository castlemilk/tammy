import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtemp, rename, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";

import { hashStableFile, readStableFileBytes } from "./stable-file.mjs";

test("reads and hashes a regular file through a stable no-follow handle", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-stable-file-"));
  try {
    const file = path.join(root, "evidence");
    await writeFile(file, "evidence");
    assert.equal(
      (
        await readStableFileBytes(file, {
          code: "EVIDENCE_INVALID",
          maxBytes: 1024,
        })
      ).toString(),
      "evidence",
    );
    assert.equal(
      await hashStableFile(file, {
        code: "EVIDENCE_INVALID",
        maxBytes: 1024,
      }),
      createHash("sha256").update("evidence").digest("hex"),
    );
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("rejects a lexical symlink and a same-path replacement after reading", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-stable-swap-"));
  try {
    const file = path.join(root, "evidence");
    const replacement = path.join(root, "replacement");
    const linked = path.join(root, "linked");
    await writeFile(file, "original");
    await symlink(file, linked);
    await assert.rejects(
      readStableFileBytes(linked, {
        code: "EVIDENCE_INVALID",
        maxBytes: 1024,
      }),
      /EVIDENCE_INVALID/,
    );
    await writeFile(replacement, "replaced");
    await assert.rejects(
      readStableFileBytes(file, {
        afterRead: async () => {
          await rename(replacement, file);
        },
        code: "EVIDENCE_CHANGED",
        maxBytes: 1024,
      }),
      /EVIDENCE_CHANGED/,
    );
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("rejects files beyond the explicit read bound", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-stable-bound-"));
  try {
    const file = path.join(root, "evidence");
    await writeFile(file, "too large");
    await assert.rejects(
      readStableFileBytes(file, {
        code: "EVIDENCE_TOO_LARGE",
        maxBytes: 4,
      }),
      /EVIDENCE_TOO_LARGE/,
    );
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});
