import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtemp, readdir, readFile, rm } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const owner = path.join(repositoryRoot, "scripts/sbr-incomplete.mjs");

function runOwner(arguments_, cwd) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [owner, ...arguments_], {
      cwd,
      env: {},
      stdio: ["ignore", "pipe", "pipe"],
    });
    let stderr = "";
    let stdout = "";
    child.stdout.on("data", (chunk) => (stdout += chunk));
    child.stderr.on("data", (chunk) => (stderr += chunk));
    child.on("error", reject);
    child.on("close", (code, signal) => resolve({ code, signal, stderr, stdout }));
  });
}

test("closed incomplete modes fail with one exact stable code and no output", async () => {
  for (const mode of [
    "accounting-fresh",
    "simulator",
    "evte",
    "doctor",
    "registration",
    "test",
    "evidence",
  ]) {
    const directory = await mkdtemp(path.join("/private/tmp", `tammy-sbr-${mode}-`));
    try {
      const result = await runOwner([mode], directory);
      assert.deepEqual(result, {
        code: 1,
        signal: null,
        stderr: `SBR_IMPLEMENTATION_INCOMPLETE:${mode}\n`,
        stdout: "",
      });
      assert.deepEqual(await readdir(directory), [], `${mode} creates no output`);
    } finally {
      await rm(directory, { force: true, recursive: true });
    }
  }
});

test("the owner rejects every value outside its closed enum", async () => {
  const directory = await mkdtemp(path.join("/private/tmp", "tammy-sbr-invalid-"));
  try {
    for (const arguments_ of [[], ["production"], ["simulator", "extra"]]) {
      assert.deepEqual(await runOwner(arguments_, directory), {
        code: 2,
        signal: null,
        stderr: "SBR_INCOMPLETE_MODE_INVALID\n",
        stdout: "",
      });
    }
    assert.deepEqual(await readdir(directory), []);
  } finally {
    await rm(directory, { force: true, recursive: true });
  }
});

test("the incomplete owner contains no child-process or output owner", async () => {
  const source = await readFile(owner, "utf8");
  assert.doesNotMatch(source, /node:child_process|\bspawn(?:Sync)?\b|\bexec(?:File|Sync)?\b/);
  assert.doesNotMatch(source, /node:fs|writeFile|mkdir|createWriteStream/);
});
