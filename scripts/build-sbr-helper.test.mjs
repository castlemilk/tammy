import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import {
  buildMasRawHelper,
  buildMasSimulatorProfile,
  createSbrHelperBuildPlan,
  generateSimulatorProfile,
  hashSbrHelperSourceTree,
  selectSbrHelperTarget,
} from "./build-sbr-helper.mjs";

test("selects only the supported helper and authenticated unavailable target", () => {
  assert.deepEqual(selectSbrHelperTarget("darwin", "arm64"), {
    status: "SIMULATOR_ENABLED",
    target: "darwin-arm64",
  });
  assert.deepEqual(selectSbrHelperTarget("win32", "x64"), {
    status: "SBR_UNAVAILABLE_ON_TARGET",
    target: "win32-x64",
  });
  assert.throws(() => selectSbrHelperTarget("linux", "x64"), /UNSUPPORTED_SBR_TARGET:linux\/x64/);
});

test("build plan pins deterministic Go inputs and the sole helper resource path", () => {
  const root = path.resolve("/workspace");
  const plan = createSbrHelperBuildPlan(root);
  assert.equal(
    plan.destination,
    path.join(root, "apps/desktop/resources/sbr-helper/darwin-arm64/tammy-sbr-helper"),
  );
  assert.deepEqual(plan.args.slice(-6), [
    "-trimpath",
    "-buildvcs=false",
    "-ldflags=-buildid= -extldflags=-Wl,-no_uuid",
    "-o",
    plan.temporary,
    "./cmd/tammy-sbr-helper",
  ]);
  assert.equal(plan.environment.GOOS, "darwin");
  assert.equal(plan.environment.GOARCH, "arm64");
  assert.equal(plan.environment.CGO_ENABLED, "1");
  assert.equal(plan.environment.SOURCE_DATE_EPOCH, "0");
  assert.equal(Object.hasOwn(plan.environment, "TAMMY_SBR_CREDENTIAL"), false);
});

test("simulator profile generator binds exact helper bytes and a test-only signer", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-sbr-profile-"));
  try {
    const helper = path.join(root, "helper");
    const profile = path.join(root, "sbr-profile-v1.json");
    const signature = path.join(root, "sbr-profile-v1.sig");
    await writeFile(helper, "signed-helper");
    const calls = [];
    const signer = async (canonical) => {
      calls.push(Buffer.from(canonical));
      return Buffer.alloc(64, 7);
    };
    const result = await generateSimulatorProfile({ helper, profile, signature, signer });
    const parsed = JSON.parse(await readFile(profile, "utf8"));
    assert.equal(parsed.helper_sha256, result.helperSha256);
    assert.equal(parsed.environment, "SIMULATOR");
    assert.equal(parsed.target, "darwin/arm64");
    assert.equal(parsed.component_manifest_sha256, "NONE");
    assert.equal(calls.length, 1);
    assert.equal(await readFile(signature, "utf8"), `${Buffer.alloc(64, 7).toString("base64")}\n`);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("production source contains no simulator private key material", async () => {
  for (const file of [
    "scripts/build-sbr-helper.mjs",
    "apps/desktop/forge.config.ts",
    "scripts/write-build-manifest.mjs",
  ]) {
    const source = await readFile(file, "utf8");
    assert.doesNotMatch(source, /BEGIN PRIVATE KEY|9d61b19deffd5a60/);
  }
});

test("Forge conditionally packages exact SBR resources and preserves helper bytes", async () => {
  const source = await readFile("apps/desktop/forge.config.ts", "utf8");
  assert.match(
    source,
    /process\.platform === "darwin"[\s\S]*"resources\/sbr-helper"[\s\S]*"resources\/sbr"/,
  );
  assert.match(source, /packagedSbrHelperSuffix/);
  assert.match(source, /isManifestBoundExecutable/);
  assert.doesNotMatch(source, /config\/sbr\/evte/);
});

test("generated helper, staged profiles, and provenance never dirty the source tree", async () => {
  const source = await readFile(".gitignore", "utf8");
  for (const entry of [
    ".tmp/sbr-helper-build/",
    "apps/desktop/resources/sbr-helper/",
    "apps/desktop/resources/sbr/",
  ])
    assert.match(source, new RegExp(`^${entry.replaceAll("/", "\\/")}$`, "m"));
});

test("MAS raw owner purges stale ignored artifacts and records this-run revision authority", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-mas-raw-"));
  try {
    await mkdir(path.join(root, "services/sbr-helper/cmd/tammy-sbr-helper"), {
      recursive: true,
    });
    await writeFile(path.join(root, "services/sbr-helper/go.mod"), "module fixture\n");
    await mkdir(path.join(root, "apps/desktop/resources/sbr/simulator"), { recursive: true });
    await writeFile(
      path.join(root, "apps/desktop/resources/sbr/simulator/stale-profile.json"),
      "stale",
    );
    await mkdir(path.join(root, ".tmp/sbr-helper-build"), { recursive: true });
    await writeFile(path.join(root, ".tmp/sbr-helper-build/provenance.json"), "stale");
    const runner = async (command, args) => {
      if (command === "git") return { stdout: `${"a".repeat(40)}\n` };
      const output = args[args.indexOf("-o") + 1];
      await writeFile(output, "deterministic raw helper");
      return { stdout: "" };
    };
    const result = await buildMasRawHelper({ root, commandRunner: runner });
    assert.equal(result.source_revision, "a".repeat(40));
    assert.match(result.session_nonce, /^[0-9a-f]{32}$/);
    assert.equal(
      await readFile(
        path.join(root, "apps/desktop/resources/sbr-helper/darwin-arm64/tammy-sbr-helper"),
        "utf8",
      ),
      "deterministic raw helper",
    );
    await assert.rejects(
      readFile(path.join(root, "apps/desktop/resources/sbr/simulator/stale-profile.json")),
      /ENOENT/,
    );
    const session = JSON.parse(
      await readFile(path.join(root, ".tmp/sbr-helper-build/session.json"), "utf8"),
    );
    assert.deepEqual(session, result);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("MAS profile owner binds final signed bytes without mutating tracked runtime profile", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-mas-profile-"));
  try {
    const helper = path.join(
      root,
      "apps/desktop/resources/sbr-helper/darwin-arm64/tammy-sbr-helper",
    );
    const sessionRoot = path.join(root, ".tmp/sbr-helper-build");
    const configRoot = path.join(root, "config/sbr/simulator");
    await Promise.all([
      mkdir(path.dirname(helper), { recursive: true }),
      mkdir(sessionRoot, { recursive: true }),
      mkdir(configRoot, { recursive: true }),
      mkdir(path.join(root, "services/sbr-helper"), { recursive: true }),
    ]);
    await writeFile(helper, "final MAS signed helper");
    await writeFile(path.join(root, "services/sbr-helper/go.mod"), "module fixture\n");
    await writeFile(
      path.join(configRoot, "profile-public-key.pem"),
      await readFile("config/sbr/simulator/profile-public-key.pem"),
    );
    await mkdir(path.join(root, "test/fixtures/sbr"), { recursive: true });
    await writeFile(
      path.join(root, "test/fixtures/sbr/simulator-profile-private-key.pem"),
      await readFile("test/fixtures/sbr/simulator-profile-private-key.pem"),
    );
    const session = {
      helper_raw_sha256: "1".repeat(64),
      mode: "MAS_RAW",
      session_nonce: "2".repeat(32),
      source_revision: "a".repeat(40),
      source_tree_sha256: await hashSbrHelperSourceTree(path.join(root, "services/sbr-helper")),
      target: "darwin-arm64",
    };
    await writeFile(
      path.join(sessionRoot, "session.json"),
      `${JSON.stringify(session, null, 2)}\n`,
    );
    const result = await buildMasSimulatorProfile({
      root,
      commandRunner: async (command) =>
        command === "git" ? { stdout: `${"a".repeat(40)}\n` } : { stdout: "" },
    });
    assert.equal(result.session_nonce, session.session_nonce);
    assert.notEqual(result.helper_sha256, session.helper_raw_sha256);
    assert.equal(result.source_revision, session.source_revision);
    await assert.rejects(readFile(path.join(configRoot, "sbr-profile-v1.json")), /ENOENT/);
    assert.equal(
      JSON.parse(
        await readFile(
          path.join(root, "apps/desktop/resources/sbr/simulator/sbr-profile-v1.json"),
          "utf8",
        ),
      ).helper_sha256,
      result.helper_sha256,
    );
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});
