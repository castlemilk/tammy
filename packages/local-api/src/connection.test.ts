import assert from "node:assert/strict";
import { randomBytes } from "node:crypto";
import { chmod, mkdir, mkdtemp, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import * as api from "./index.ts";

const connection = {
  version: "v1",
  origin: "http://127.0.0.1:54321",
  capability: randomBytes(32).toString("base64url"),
};

test("connection validation permits only exact loopback authority and a canonical 32-byte capability", () => {
  assert.ok("validateLocalApiConnection" in api, "connection validation is exported");
  const validate = (api as typeof import("./connection.ts")).validateLocalApiConnection;
  assert.deepEqual(validate(connection), connection);
  for (const origin of [
    "https://127.0.0.1:54321",
    "http://localhost:54321",
    "http://[::1]:54321",
    "http://127.1:54321",
    "http://127.0.0.1:80",
    "http://127.0.0.1:65536",
    "http://127.0.0.1:054321",
    `${connection.origin}/`,
    `${connection.origin}?x=1`,
    `${connection.origin}#x`,
    "http://user@127.0.0.1:54321",
  ])
    assert.throws(() => validate({ ...connection, origin }), /^Error: INVALID_CONNECTION$/);
  for (const value of [
    null,
    [],
    { ...connection, extra: "secret" },
    { ...connection, version: "v2" },
    { ...connection, capability: `${connection.capability}=` },
    { ...connection, capability: "a".repeat(42) },
  ])
    assert.throws(() => validate(value), /^Error: INVALID_CONNECTION$/);
});

test("connection file reads require an absolute mode-0600 regular non-symlink file bounded at 4KiB", async (t) => {
  assert.ok("readLocalApiConnectionFile" in api, "private file reader is exported");
  const read = (api as typeof import("./connection.ts")).readLocalApiConnectionFile;
  const directory = await mkdtemp(join(tmpdir(), "tammy-local-api-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const file = join(directory, "connection.json");
  await writeFile(file, JSON.stringify(connection), { mode: 0o600 });
  assert.deepEqual(await read(file), connection);
  await assert.rejects(read("relative.json"), /^Error: INVALID_CONNECTION_FILE$/);
  await assert.rejects(read(join(directory, "missing.json")), /^Error: INVALID_CONNECTION_FILE$/);
  await chmod(file, 0o644);
  await assert.rejects(read(file), /^Error: INVALID_CONNECTION_FILE$/);
  await chmod(file, 0o600);
  const link = join(directory, "link.json");
  await symlink(file, link);
  await assert.rejects(read(link), /^Error: INVALID_CONNECTION_FILE$/);
  const folder = join(directory, "folder.json");
  await mkdir(folder, { mode: 0o600 });
  await assert.rejects(read(folder), /^Error: INVALID_CONNECTION_FILE$/);
  await writeFile(file, "x".repeat(4097));
  await assert.rejects(read(file), /^Error: INVALID_CONNECTION_FILE$/);
  await writeFile(file, "{secret malformed");
  await assert.rejects(read(file), /^Error: INVALID_CONNECTION_FILE$/);
  await writeFile(file, JSON.stringify({ ...connection, origin: "https://attacker.invalid" }));
  await assert.rejects(read(file), /^Error: INVALID_CONNECTION_FILE$/);
});
