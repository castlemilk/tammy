import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createHash, randomUUID } from "node:crypto";
import {
  access,
  mkdir,
  mkdtemp,
  readdir,
  readFile,
  realpath,
  rm,
  symlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { deflateSync } from "node:zlib";

import {
  inspectPng,
  normalizeAccessibilitySnapshot,
  promoteScreenshotSet,
  scanScreenshotInputs,
  validateScreenshotFixture,
  validateScreenshotManifest,
} from "./check-app-store-screenshots.mjs";

const root = path.resolve(import.meta.dirname, "..");
const fixturePath = path.join(root, "apps/desktop/release/macos/screenshots/fixture.json");
const fixtureBytes = await readFile(fixturePath);
const fixture = JSON.parse(fixtureBytes);
const storeMetadataBytes = await readFile(
  path.join(root, "apps/desktop/release/macos/store-metadata.md"),
);
const sha256 = (value) => createHash("sha256").update(value).digest("hex");

test("normalizes the complete ARIA tree including accessibility-only names", () => {
  const snapshot = `- complementary:
  - text: Tammy
  - navigation "Primary":
    - link "Overview":
      - /url: /overview
- main:
  - heading "Banking" [level=1]
  - text: Opening balance
  - spinbutton "Opening balance": "1000.00"
  - button "Complete reconciliation" [disabled]
`;
  assert.deepEqual(normalizeAccessibilitySnapshot(snapshot), [
    "Tammy",
    "Primary",
    "Overview",
    "Banking",
    "Opening balance",
    "Opening balance",
    "1000.00",
    "Complete reconciliation",
  ]);
  assert.throws(
    () => normalizeAccessibilitySnapshot(`${snapshot}- reviewer-mode: hidden\n`),
    /APP_STORE_SCREENSHOT_ACCESSIBILITY_INVALID/,
  );
});

function crc32(bytes) {
  let crc = 0xffffffff;
  for (const byte of bytes) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit += 1) crc = (crc >>> 1) ^ (0xedb88320 & -(crc & 1));
  }
  return (crc ^ 0xffffffff) >>> 0;
}

function chunk(type, data) {
  const typeBytes = Buffer.from(type, "ascii");
  const length = Buffer.alloc(4);
  length.writeUInt32BE(data.length);
  const checksum = Buffer.alloc(4);
  checksum.writeUInt32BE(crc32(Buffer.concat([typeBytes, data])));
  return Buffer.concat([length, typeBytes, data, checksum]);
}

function png({
  beforeIdat = [],
  colorType = 2,
  height = 900,
  idatBytes,
  interlace = 0,
  marker = 0,
  width = 1440,
} = {}) {
  const channels = colorType === 2 ? 3 : 4;
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header[8] = 8;
  header[9] = colorType;
  header[10] = 0;
  header[11] = 0;
  header[12] = interlace;
  const pixels = Buffer.alloc((width * channels + 1) * height);
  pixels[1] = marker;
  return Buffer.concat([
    Buffer.from("89504e470d0a1a0a", "hex"),
    chunk("IHDR", header),
    ...beforeIdat,
    chunk("IDAT", idatBytes ?? deflateSync(pixels)),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

async function createCapture(generation = 0) {
  const temporary = await mkdtemp(path.join(tmpdir(), "tammy-screenshot-capture-"));
  const images = [];
  for (const [index, definition] of fixture.screenshots.entries()) {
    const bytes = png({ marker: index + 1 + generation * 10 });
    const snapshotName = definition.filename.replace(/\.png$/u, ".accessibility.txt");
    const snapshot = `${definition.completeAccessibilityText.join("\n")}\n`;
    await Promise.all([
      writeFile(path.join(temporary, definition.filename), bytes),
      writeFile(path.join(temporary, snapshotName), snapshot),
    ]);
    images.push({
      accessibilitySnapshot: snapshotName,
      accessibilitySnapshotSha256: sha256(snapshot),
      caption: definition.caption,
      filename: definition.filename,
      sha256: sha256(bytes),
    });
  }
  const manifest = {
    buildNumber: "42",
    candidateEventPath: null,
    candidateEventSha256: null,
    captureArtifactKind: "development-signed-app",
    capturedAt: `2026-08-30T12:${String(generation).padStart(2, "0")}:00.000Z`,
    developmentSignedAppSha256: ((generation + 13) % 16).toString(16).repeat(64),
    dimensions: { height: 900, width: 1440 },
    distributionPackageSha256: null,
    fixturePath: "apps/desktop/release/macos/screenshots/fixture.json",
    fixtureSha256: sha256(fixtureBytes),
    images,
    locale: "en-AU",
    marketingVersion: "0.1.0",
    productSourceCommit: "a".repeat(40),
    productSourceTree: "b".repeat(40),
    schemaVersion: 1,
    unsignedContentManifestSha256: "c".repeat(64),
  };
  await writeFile(path.join(temporary, "manifest.json"), `${JSON.stringify(manifest, null, 2)}\n`);
  return { manifest, temporary };
}

async function runKilledPromotion({ boundary, destinationRoot, repositoryRoot, sourceDirectory }) {
  const moduleUrl = new URL("./check-app-store-screenshots.mjs", import.meta.url).href;
  const code = `
    import { readFile } from "node:fs/promises";
    import { promoteScreenshotSet } from ${JSON.stringify(moduleUrl)};
    const fixtureBytes = await readFile(${JSON.stringify(fixturePath)});
    const fixture = JSON.parse(fixtureBytes);
    const storeMetadataBytes = await readFile(${JSON.stringify(
      path.join(root, "apps/desktop/release/macos/store-metadata.md"),
    )});
    await promoteScreenshotSet({
      destinationRoot: ${JSON.stringify(destinationRoot)},
      fixture,
      fixtureBytes,
      onBoundary: async (name) => {
        if (name === ${JSON.stringify(boundary)}) process.kill(process.pid, "SIGKILL");
      },
      repositoryRoot: ${JSON.stringify(repositoryRoot)},
      sourceDirectory: ${JSON.stringify(sourceDirectory)},
      storeMetadataBytes,
    });
  `;
  const child = spawn(process.execPath, ["--input-type=module", "--eval", code], {
    cwd: root,
    stdio: ["ignore", "pipe", "pipe"],
  });
  let stderr = "";
  child.stderr.on("data", (chunk) => {
    if (stderr.length < 4096) stderr += chunk;
  });
  const result = await new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("close", (exitCode, signal) => resolve({ exitCode, signal }));
  });
  assert.deepEqual(result, { exitCode: null, signal: "SIGKILL" }, stderr);
}

async function waitForFile(file) {
  for (let attempt = 0; attempt < 500; attempt += 1) {
    try {
      await access(file);
      return;
    } catch {
      await new Promise((resolve) => setTimeout(resolve, 10));
    }
  }
  assert.fail(`Timed out waiting for ${file}`);
}

function spawnBarrierPromotion({
  barrierDirectory,
  contender,
  destinationRoot,
  repositoryRoot,
  sourceDirectory,
}) {
  const moduleUrl = new URL("./check-app-store-screenshots.mjs", import.meta.url).href;
  const ready = path.join(barrierDirectory, `ready-${contender}`);
  const winner = path.join(barrierDirectory, `winner-${contender}`);
  const start = path.join(barrierDirectory, "start");
  const release = path.join(barrierDirectory, "release");
  const code = `
    import { access, readFile, writeFile } from "node:fs/promises";
    import { promoteScreenshotSet } from ${JSON.stringify(moduleUrl)};
    const waitFor = async (file) => {
      for (let attempt = 0; attempt < 500; attempt += 1) {
        try { await access(file); return; } catch {}
        await new Promise((resolve) => setTimeout(resolve, 10));
      }
      throw new Error("BARRIER_TIMEOUT");
    };
    const fixtureBytes = await readFile(${JSON.stringify(fixturePath)});
    const fixture = JSON.parse(fixtureBytes);
    const storeMetadataBytes = await readFile(${JSON.stringify(
      path.join(root, "apps/desktop/release/macos/store-metadata.md"),
    )});
    await promoteScreenshotSet({
      destinationRoot: ${JSON.stringify(destinationRoot)},
      fixture,
      fixtureBytes,
      onBoundary: async (name) => {
        if (name === "after-lock-candidate-fsync") {
          await writeFile(${JSON.stringify(ready)}, "ready\\n", { flag: "wx" });
          await waitFor(${JSON.stringify(start)});
        }
        if (name === "after-lock-publication") {
          await writeFile(${JSON.stringify(winner)}, "winner\\n", { flag: "wx" });
          await waitFor(${JSON.stringify(release)});
        }
      },
      repositoryRoot: ${JSON.stringify(repositoryRoot)},
      sourceDirectory: ${JSON.stringify(sourceDirectory)},
      storeMetadataBytes,
    });
  `;
  const child = spawn(process.execPath, ["--input-type=module", "--eval", code], {
    cwd: root,
    stdio: ["ignore", "pipe", "pipe"],
  });
  let stderr = "";
  child.stderr.on("data", (chunk) => {
    if (stderr.length < 4096) stderr += chunk;
  });
  const result = new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("close", (exitCode, signal) => resolve({ exitCode, signal, stderr }));
  });
  return { ready, result, winner };
}

test("validates the one strict fictional screenshot fixture", () => {
  assert.equal(validateScreenshotFixture(fixture), fixture);
  for (const changed of [
    { ...fixture, fictional: false },
    { ...fixture, business: { ...fixture.business, legalName: "Gamma Systems Pty Ltd" } },
    { ...fixture, period: { ...fixture.period, postingDate: "2026-08-30" } },
    { ...fixture, ids: { ...fixture.ids, journal: randomUUID() } },
    { ...fixture, privateKey: "-----BEGIN PRIVATE KEY-----" },
    {
      ...fixture,
      sourceDocument: { ...fixture.sourceDocument, invoiceNumber: "UNAPPROVED-123" },
    },
  ]) {
    assert.throws(() => validateScreenshotFixture(changed), /APP_STORE_SCREENSHOT_FIXTURE_INVALID/);
  }
  assert.throws(
    () => scanScreenshotInputs("ben.ebsworth@gmail.com\nAKIAIOSFODNN7EXAMPLE"),
    /APP_STORE_SCREENSHOT_INPUT_UNSAFE/,
  );
});

test("accepts only complete 1440 by 900 RGB non-interlaced PNG bytes", () => {
  const raster = Buffer.alloc((1440 * 3 + 1) * 900);
  assert.deepEqual(inspectPng(png()), {
    bitDepth: 8,
    colorType: 2,
    height: 900,
    interlace: 0,
    sha256: sha256(png()),
    width: 1440,
  });
  for (const bytes of [
    png({ width: 1280 }),
    png({ colorType: 6 }),
    png({ interlace: 1 }),
    png({ idatBytes: Buffer.from("not a zlib stream") }),
    png({ idatBytes: Buffer.concat([deflateSync(raster), Buffer.from("trailing")]) }),
    png({ idatBytes: deflateSync(Buffer.alloc((1440 * 3 + 1) * 901)) }),
    png({ beforeIdat: [chunk("PLTE", Buffer.from([0, 0, 0]))] }),
    png({ beforeIdat: [chunk("abca", Buffer.alloc(0))] }),
    png().subarray(0, 50),
  ]) {
    assert.throws(() => inspectPng(bytes), /APP_STORE_SCREENSHOT_PNG_INVALID/);
  }
});

test("validates exact images, accessibility snapshots, hashes, captions and provenance", async () => {
  const { manifest, temporary } = await createCapture();
  try {
    assert.deepEqual(
      await validateScreenshotManifest({
        captureDirectory: temporary,
        fixture,
        fixtureBytes,
        storeMetadataBytes,
      }),
      manifest,
    );
    await assert.rejects(
      validateScreenshotManifest({
        captureDirectory: temporary,
        fixture,
        fixtureBytes,
        storeMetadataBytes: Buffer.from(
          storeMetadataBytes
            .toString("utf8")
            .replace(fixture.screenshots[0].caption, "A drifting App Store caption."),
        ),
      }),
      /APP_STORE_SCREENSHOT_MANIFEST_INVALID/,
    );
    await assert.rejects(
      validateScreenshotManifest({
        captureDirectory: temporary,
        fixture,
        fixtureBytes: Buffer.from(
          `${JSON.stringify({ ...fixture, fixtureId: "different-hashed-fixture" })}\n`,
        ),
        storeMetadataBytes,
      }),
      /APP_STORE_SCREENSHOT_MANIFEST_INVALID/,
    );
    await writeFile(path.join(temporary, "02-document-review.png"), png({ marker: 99 }));
    await assert.rejects(
      validateScreenshotManifest({
        captureDirectory: temporary,
        fixture,
        fixtureBytes,
        storeMetadataBytes,
      }),
      /APP_STORE_SCREENSHOT_MANIFEST_INVALID/,
    );
  } finally {
    await rm(temporary, { force: true, recursive: true });
  }
});

test("rejects package linkage without a candidate event and post-render secret text", async () => {
  for (const mutate of [
    (manifest) => ({ ...manifest, distributionPackageSha256: "e".repeat(64) }),
    (manifest) => ({ ...manifest, acceptedByApple: true }),
  ]) {
    const { manifest, temporary } = await createCapture();
    try {
      await writeFile(
        path.join(temporary, "manifest.json"),
        `${JSON.stringify(mutate(manifest), null, 2)}\n`,
      );
      await assert.rejects(
        validateScreenshotManifest({
          captureDirectory: temporary,
          fixture,
          fixtureBytes,
          storeMetadataBytes,
        }),
        /APP_STORE_SCREENSHOT_MANIFEST_INVALID/,
      );
    } finally {
      await rm(temporary, { force: true, recursive: true });
    }
  }

  const { temporary } = await createCapture();
  try {
    await writeFile(
      path.join(temporary, "01-overview.accessibility.txt"),
      "Overview\nrecovery code: SECRET-DO-NOT-SHOW\n",
    );
    await assert.rejects(
      validateScreenshotManifest({
        captureDirectory: temporary,
        fixture,
        fixtureBytes,
        storeMetadataBytes,
      }),
      /APP_STORE_SCREENSHOT_(INPUT_UNSAFE|MANIFEST_INVALID)/,
    );
  } finally {
    await rm(temporary, { force: true, recursive: true });
  }
});

test("accessibility snapshots are an exact allowlist, not a denylist", async () => {
  for (const addedText of [
    "Customer Jane Smith",
    "customer@example.com",
    "Account 12345678",
    "Reference REAL-84729",
  ]) {
    const { manifest, temporary } = await createCapture();
    try {
      const snapshotPath = path.join(temporary, "01-overview.accessibility.txt");
      const snapshot = `${fixture.screenshots[0].completeAccessibilityText.join("\n")}\n${addedText}\n`;
      await writeFile(snapshotPath, snapshot);
      manifest.images[0].accessibilitySnapshotSha256 = sha256(snapshot);
      await writeFile(
        path.join(temporary, "manifest.json"),
        `${JSON.stringify(manifest, null, 2)}\n`,
      );
      await assert.rejects(
        validateScreenshotManifest({
          captureDirectory: temporary,
          fixture,
          fixtureBytes,
          storeMetadataBytes,
        }),
        /APP_STORE_SCREENSHOT_(INPUT_UNSAFE|MANIFEST_INVALID)/,
      );
    } finally {
      await rm(temporary, { force: true, recursive: true });
    }
  }
});

test("authenticates every package-linked candidate fact", async () => {
  const { manifest, temporary } = await createCapture();
  try {
    const event = {
      appSha256: "f".repeat(64),
      buildNumber: manifest.buildNumber,
      kind: "candidate-built",
      marketingVersion: manifest.marketingVersion,
      packageSha256: "e".repeat(64),
      productSourceCommit: manifest.productSourceCommit,
      productSourceTree: manifest.productSourceTree,
      unsignedContentManifestSha256: manifest.unsignedContentManifestSha256,
    };
    const candidateEventBytes = Buffer.from(`${JSON.stringify(event, null, 2)}\n`);
    manifest.candidateEventPath =
      "docs/release/records/macos/0.1.0/build-42/events/2026-08-30T12-00-00.000Z-candidate-built.json";
    manifest.candidateEventSha256 = sha256(candidateEventBytes);
    manifest.distributionPackageSha256 = event.packageSha256;
    await writeFile(
      path.join(temporary, "manifest.json"),
      `${JSON.stringify(manifest, null, 2)}\n`,
    );
    await assert.doesNotReject(
      validateScreenshotManifest({
        candidateEventBytes,
        captureDirectory: temporary,
        fixture,
        fixtureBytes,
        storeMetadataBytes,
      }),
    );
    await assert.rejects(
      validateScreenshotManifest({
        candidateEventBytes: Buffer.from(
          `${JSON.stringify({ ...event, packageSha256: "0".repeat(64) })}\n`,
        ),
        captureDirectory: temporary,
        fixture,
        fixtureBytes,
        storeMetadataBytes,
      }),
      /APP_STORE_SCREENSHOT_MANIFEST_INVALID/,
    );
    const developmentHashEventBytes = Buffer.from(
      `${JSON.stringify({ ...event, appSha256: manifest.developmentSignedAppSha256 })}\n`,
    );
    manifest.candidateEventSha256 = sha256(developmentHashEventBytes);
    await writeFile(
      path.join(temporary, "manifest.json"),
      `${JSON.stringify(manifest, null, 2)}\n`,
    );
    await assert.rejects(
      validateScreenshotManifest({
        candidateEventBytes: developmentHashEventBytes,
        captureDirectory: temporary,
        fixture,
        fixtureBytes,
        storeMetadataBytes,
      }),
      /APP_STORE_SCREENSHOT_MANIFEST_INVALID/,
    );
  } finally {
    await rm(temporary, { force: true, recursive: true });
  }
});

test("promotes only a validated set and preserves unknown canonical content", async () => {
  const repository = await realpath(
    await mkdtemp(path.join(tmpdir(), "tammy-screenshot-destination-")),
  );
  const destinationRoot = path.join(repository, "apps/desktop/release/macos/screenshots");
  await mkdir(destinationRoot, { recursive: true });
  const first = await createCapture();
  try {
    const canonical = await promoteScreenshotSet({
      destinationRoot,
      fixture,
      fixtureBytes,
      repositoryRoot: repository,
      sourceDirectory: first.temporary,
      storeMetadataBytes,
    });
    assert.equal(canonical, path.join(destinationRoot, "en-AU"));
    assert.equal(
      (
        await validateScreenshotManifest({
          captureDirectory: canonical,
          fixture,
          fixtureBytes,
          storeMetadataBytes,
        })
      ).locale,
      "en-AU",
    );

    await writeFile(path.join(canonical, "unknown-user-file.txt"), "preserve me\n");
    const second = await createCapture();
    try {
      await assert.rejects(
        promoteScreenshotSet({
          destinationRoot,
          fixture,
          fixtureBytes,
          repositoryRoot: repository,
          sourceDirectory: second.temporary,
          storeMetadataBytes,
        }),
        /APP_STORE_SCREENSHOT_DESTINATION_UNSAFE/,
      );
      assert.equal(
        await readFile(path.join(canonical, "unknown-user-file.txt"), "utf8"),
        "preserve me\n",
      );
    } finally {
      await rm(second.temporary, { force: true, recursive: true });
    }

    const outside = await mkdtemp(path.join(tmpdir(), "tammy-screenshot-outside-"));
    await rm(canonical, { force: true, recursive: true });
    await symlink(outside, canonical);
    const third = await createCapture();
    try {
      await assert.rejects(
        promoteScreenshotSet({
          destinationRoot,
          fixture,
          fixtureBytes,
          repositoryRoot: repository,
          sourceDirectory: third.temporary,
          storeMetadataBytes,
        }),
        /APP_STORE_SCREENSHOT_DESTINATION_UNSAFE/,
      );
    } finally {
      await rm(third.temporary, { force: true, recursive: true });
      await rm(outside, { force: true, recursive: true });
    }
  } finally {
    await rm(first.temporary, { force: true, recursive: true });
    await rm(repository, { force: true, recursive: true });
  }
});

test("recovers the last complete set across every promotion boundary", async () => {
  for (const boundary of [
    "after-staging-fsync",
    "before-backup-rename",
    "after-backup-rename",
    "after-backup-parent-fsync",
    "before-staging-rename",
    "after-staging-rename",
    "after-canonical-parent-fsync",
    "after-canonical-revalidation",
    "after-backup-removal",
    "after-final-parent-fsync",
  ]) {
    const repository = await realpath(
      await mkdtemp(path.join(tmpdir(), "tammy-screenshot-recovery-")),
    );
    const destinationRoot = path.join(repository, "apps/desktop/release/macos/screenshots");
    await mkdir(destinationRoot, { recursive: true });
    const initial = await createCapture();
    const interrupted = await createCapture();
    const recovery = await createCapture();
    try {
      await promoteScreenshotSet({
        destinationRoot,
        fixture,
        fixtureBytes,
        repositoryRoot: repository,
        sourceDirectory: initial.temporary,
        storeMetadataBytes,
      });
      await assert.rejects(
        promoteScreenshotSet({
          destinationRoot,
          fixture,
          fixtureBytes,
          onBoundary: async (name) => {
            if (name === boundary) throw new Error(`SIMULATED_CRASH:${boundary}`);
          },
          repositoryRoot: repository,
          sourceDirectory: interrupted.temporary,
          storeMetadataBytes,
        }),
        new RegExp(`SIMULATED_CRASH:${boundary}`),
      );
      const canonical = await promoteScreenshotSet({
        destinationRoot,
        fixture,
        fixtureBytes,
        repositoryRoot: repository,
        sourceDirectory: recovery.temporary,
        storeMetadataBytes,
      });
      await assert.doesNotReject(
        validateScreenshotManifest({
          captureDirectory: canonical,
          fixture,
          fixtureBytes,
          storeMetadataBytes,
        }),
      );
    } finally {
      await Promise.all([
        rm(initial.temporary, { force: true, recursive: true }),
        rm(interrupted.temporary, { force: true, recursive: true }),
        rm(recovery.temporary, { force: true, recursive: true }),
        rm(repository, { force: true, recursive: true }),
      ]);
    }
  }
});

test("recovers a stale owned lock after real child-process death with the right generation", async () => {
  const newGenerationBoundaries = new Set([
    "after-staging-rename",
    "after-canonical-parent-fsync",
    "after-canonical-revalidation",
    "after-backup-removal",
    "after-final-parent-fsync",
    "after-release-claim-publication",
    "after-release-lock-rename",
  ]);
  for (const boundary of [
    "after-lock-candidate-fsync",
    "after-lock-publication",
    "after-staging-fsync",
    "before-backup-rename",
    "after-backup-rename",
    "after-backup-parent-fsync",
    "before-staging-rename",
    "after-staging-rename",
    "after-canonical-parent-fsync",
    "after-canonical-revalidation",
    "after-backup-removal",
    "after-final-parent-fsync",
    "after-release-claim-publication",
    "after-release-lock-rename",
  ]) {
    const repository = await realpath(
      await mkdtemp(path.join(tmpdir(), "tammy-screenshot-killed-recovery-")),
    );
    const destinationRoot = path.join(repository, "apps/desktop/release/macos/screenshots");
    await mkdir(destinationRoot, { recursive: true });
    const initial = await createCapture(0);
    const interrupted = await createCapture(1);
    const recoveryProbe = await createCapture(2);
    try {
      await promoteScreenshotSet({
        destinationRoot,
        fixture,
        fixtureBytes,
        repositoryRoot: repository,
        sourceDirectory: initial.temporary,
        storeMetadataBytes,
      });
      await runKilledPromotion({
        boundary,
        destinationRoot,
        repositoryRoot: repository,
        sourceDirectory: interrupted.temporary,
      });
      await assert.rejects(
        promoteScreenshotSet({
          destinationRoot,
          fixture,
          fixtureBytes,
          onBoundary: async (name) => {
            if (name === "after-staging-fsync") throw new Error("RECOVERY_PROBE_COMPLETE");
          },
          repositoryRoot: repository,
          sourceDirectory: recoveryProbe.temporary,
          storeMetadataBytes,
        }),
        /RECOVERY_PROBE_COMPLETE/,
      );
      const recovered = await validateScreenshotManifest({
        captureDirectory: path.join(destinationRoot, "en-AU"),
        fixture,
        fixtureBytes,
        storeMetadataBytes,
      });
      assert.equal(
        recovered.capturedAt,
        newGenerationBoundaries.has(boundary)
          ? interrupted.manifest.capturedAt
          : initial.manifest.capturedAt,
        boundary,
      );
    } finally {
      await Promise.all([
        rm(initial.temporary, { force: true, recursive: true }),
        rm(interrupted.temporary, { force: true, recursive: true }),
        rm(recoveryProbe.temporary, { force: true, recursive: true }),
        rm(repository, { force: true, recursive: true }),
      ]);
    }
  }
});

test("recovers after a stale-lock reclaimer dies with its ownership marker published", async () => {
  const repository = await realpath(
    await mkdtemp(path.join(tmpdir(), "tammy-screenshot-reclaim-death-")),
  );
  const destinationRoot = path.join(repository, "apps/desktop/release/macos/screenshots");
  await mkdir(destinationRoot, { recursive: true });
  const initial = await createCapture(0);
  const staleOwner = await createCapture(1);
  const killedReclaimer = await createCapture(2);
  const recovery = await createCapture(3);
  try {
    await promoteScreenshotSet({
      destinationRoot,
      fixture,
      fixtureBytes,
      repositoryRoot: repository,
      sourceDirectory: initial.temporary,
      storeMetadataBytes,
    });
    await runKilledPromotion({
      boundary: "after-lock-publication",
      destinationRoot,
      repositoryRoot: repository,
      sourceDirectory: staleOwner.temporary,
    });
    await runKilledPromotion({
      boundary: "after-reclaim-marker-publication",
      destinationRoot,
      repositoryRoot: repository,
      sourceDirectory: killedReclaimer.temporary,
    });
    const canonicalPath = await promoteScreenshotSet({
      destinationRoot,
      fixture,
      fixtureBytes,
      repositoryRoot: repository,
      sourceDirectory: recovery.temporary,
      storeMetadataBytes,
    });
    const canonical = await validateScreenshotManifest({
      captureDirectory: canonicalPath,
      fixture,
      fixtureBytes,
      storeMetadataBytes,
    });
    assert.equal(canonical.capturedAt, recovery.manifest.capturedAt);
    assert.deepEqual(
      (await readdir(destinationRoot)).filter((name) =>
        [".promotion.lock", ".promotion.lock.reclaim"].includes(name),
      ),
      [],
    );
  } finally {
    await Promise.all([
      rm(initial.temporary, { force: true, recursive: true }),
      rm(staleOwner.temporary, { force: true, recursive: true }),
      rm(killedReclaimer.temporary, { force: true, recursive: true }),
      rm(recovery.temporary, { force: true, recursive: true }),
      rm(repository, { force: true, recursive: true }),
    ]);
  }
});

test("atomically reclaims one stale lock under a two-process barrier", async () => {
  const repository = await realpath(
    await mkdtemp(path.join(tmpdir(), "tammy-screenshot-lock-race-")),
  );
  const destinationRoot = path.join(repository, "apps/desktop/release/macos/screenshots");
  const barrierDirectory = path.join(repository, "barrier");
  await Promise.all([
    mkdir(destinationRoot, { recursive: true }),
    mkdir(barrierDirectory, { recursive: true }),
  ]);
  const initial = await createCapture(0);
  const staleOwner = await createCapture(1);
  const contenders = await Promise.all([createCapture(2), createCapture(3)]);
  try {
    await promoteScreenshotSet({
      destinationRoot,
      fixture,
      fixtureBytes,
      repositoryRoot: repository,
      sourceDirectory: initial.temporary,
      storeMetadataBytes,
    });
    await runKilledPromotion({
      boundary: "after-lock-publication",
      destinationRoot,
      repositoryRoot: repository,
      sourceDirectory: staleOwner.temporary,
    });

    const processes = contenders.map((capture, contender) =>
      spawnBarrierPromotion({
        barrierDirectory,
        contender,
        destinationRoot,
        repositoryRoot: repository,
        sourceDirectory: capture.temporary,
      }),
    );
    await Promise.all(processes.map(({ ready }) => waitForFile(ready)));
    await writeFile(path.join(barrierDirectory, "start"), "start\n", { flag: "wx" });
    await Promise.race(processes.map(({ winner }) => waitForFile(winner)));

    const firstExit = await Promise.race(
      processes.map(({ result }, contender) => result.then((value) => ({ contender, value }))),
    );
    assert.equal(firstExit.value.exitCode, 1, firstExit.value.stderr);
    assert.equal(firstExit.value.signal, null);
    assert.match(firstExit.value.stderr, /APP_STORE_SCREENSHOT_PROMOTION_LOCKED/);
    await writeFile(path.join(barrierDirectory, "release"), "release\n", { flag: "wx" });
    const results = await Promise.all(processes.map(({ result }) => result));
    assert.deepEqual(
      results
        .map(({ exitCode, signal }) => ({ exitCode, signal }))
        .sort((left, right) => Number(left.exitCode) - Number(right.exitCode)),
      [
        { exitCode: 0, signal: null },
        { exitCode: 1, signal: null },
      ],
    );

    const winners = (await readdir(barrierDirectory)).filter((name) => name.startsWith("winner-"));
    assert.equal(winners.length, 1);
    const winnerIndex = Number(winners[0].replace("winner-", ""));
    const canonical = await validateScreenshotManifest({
      captureDirectory: path.join(destinationRoot, "en-AU"),
      fixture,
      fixtureBytes,
      storeMetadataBytes,
    });
    assert.equal(canonical.capturedAt, contenders[winnerIndex].manifest.capturedAt);
    assert.equal(
      await access(path.join(destinationRoot, ".promotion.lock")).then(
        () => true,
        () => false,
      ),
      false,
    );
  } finally {
    await Promise.all([
      rm(initial.temporary, { force: true, recursive: true }),
      rm(staleOwner.temporary, { force: true, recursive: true }),
      ...contenders.map(({ temporary }) => rm(temporary, { force: true, recursive: true })),
      rm(repository, { force: true, recursive: true }),
    ]);
  }
});

test("refuses to rename or remove an unvalidated recovery backup", async () => {
  const repository = await realpath(
    await mkdtemp(path.join(tmpdir(), "tammy-screenshot-invalid-backup-")),
  );
  const destinationRoot = path.join(repository, "apps/desktop/release/macos/screenshots");
  const backup = path.join(destinationRoot, ".en-AU.backup-user-content");
  const source = await createCapture();
  await mkdir(backup, { recursive: true });
  await writeFile(path.join(backup, "unknown-user-file.txt"), "do not remove\n");
  try {
    await assert.rejects(
      promoteScreenshotSet({
        destinationRoot,
        fixture,
        fixtureBytes,
        repositoryRoot: repository,
        sourceDirectory: source.temporary,
        storeMetadataBytes,
      }),
      /APP_STORE_SCREENSHOT_DESTINATION_UNSAFE/,
    );
    assert.equal(
      await readFile(path.join(backup, "unknown-user-file.txt"), "utf8"),
      "do not remove\n",
    );
  } finally {
    await rm(repository, { force: true, recursive: true });
    await rm(source.temporary, { force: true, recursive: true });
  }
});

test("rejects a matching destination redirected through an ancestor symlink", async () => {
  const repository = await realpath(
    await mkdtemp(path.join(tmpdir(), "tammy-screenshot-symlink-repository-")),
  );
  const outside = await realpath(
    await mkdtemp(path.join(tmpdir(), "tammy-screenshot-symlink-outside-")),
  );
  const source = await createCapture();
  const desktop = path.join(repository, "apps/desktop");
  await mkdir(desktop, { recursive: true });
  await mkdir(path.join(outside, "macos/screenshots"), { recursive: true });
  await symlink(outside, path.join(desktop, "release"));
  const destinationRoot = path.join(repository, "apps/desktop/release/macos/screenshots");
  try {
    await assert.rejects(
      promoteScreenshotSet({
        destinationRoot,
        fixture,
        fixtureBytes,
        repositoryRoot: repository,
        sourceDirectory: source.temporary,
        storeMetadataBytes,
      }),
      /APP_STORE_SCREENSHOT_DESTINATION_UNSAFE/,
    );
    assert.deepEqual(await readdir(path.join(outside, "macos/screenshots")), []);
  } finally {
    await rm(source.temporary, { force: true, recursive: true });
    await rm(repository, { force: true, recursive: true });
    await rm(outside, { force: true, recursive: true });
  }
});
