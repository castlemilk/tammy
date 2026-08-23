// @vitest-environment node

import { lstat, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";

import { locatePackagedApplicationForProject } from "../../tests/e2e/fixtures";
import { removeSbrE2eResult, writePassedSbrE2eResult } from "../../tests/e2e/sbr-result";
import { generateTotp } from "../../tests/e2e/support/totp";

const temporaryRoots: string[] = [];
const HASH = "a".repeat(64);
const REVISION = "b".repeat(40);

function validResult(recordedAt = "2026-08-24T02:00:00.000Z") {
  return {
    schema: "tammy-sbr-e2e-result-v1" as const,
    source_revision: REVISION,
    profile_sha256: HASH,
    helper_sha256: HASH,
    fixture_sha256: HASH,
    socket_samples: 1,
    socket_violations: 0,
    core_path_verified: true,
    helper_path_verified: true,
    core_orphans: 0,
    helper_orphans: 0,
    playwright_status: "PASSED" as const,
    recorded_at: recordedAt,
  };
}

afterEach(async () => {
  await Promise.all(
    temporaryRoots.splice(0).map((root) => rm(root, { force: true, recursive: true })),
  );
});

describe("SBR packaged result evidence", () => {
  it("writes the exact schema atomically with mode 0600 only for a clean exact-revision pass", async () => {
    const root = await mkdtemp(path.join(os.tmpdir(), "tammy-sbr-result-"));
    temporaryRoots.push(root);
    const destination = path.join(root, ".tmp/sbr-e2e/latest/result.json");
    const now = new Date("2026-08-24T02:00:30.000Z");

    await writePassedSbrE2eResult(destination, validResult(), { expectedRevision: REVISION, now });

    expect(JSON.parse(await readFile(destination, "utf8"))).toEqual(validResult());
    expect((await lstat(destination)).mode & 0o777).toBe(0o600);
    expect(await readFile(destination, "utf8")).toBe(`${JSON.stringify(validResult())}\n`);
  });

  it.each([
    ["missing sample", { socket_samples: 0 }],
    ["socket violation", { socket_violations: 1 }],
    ["core orphan", { core_orphans: 1 }],
    ["helper orphan", { helper_orphans: 1 }],
    ["unverified helper path", { helper_path_verified: false }],
    ["forced failure", { playwright_status: "FAILED" }],
    ["stale revision", { source_revision: "c".repeat(40) }],
    ["stale timestamp", { recorded_at: "2026-08-23T02:00:00.000Z" }],
  ])("refuses PASSED for %s and removes stale output", async (_name, mutation) => {
    const root = await mkdtemp(path.join(os.tmpdir(), "tammy-sbr-result-"));
    temporaryRoots.push(root);
    const destination = path.join(root, "result.json");
    await writeFile(destination, "stale", { mode: 0o600 });

    await expect(
      writePassedSbrE2eResult(
        destination,
        { ...validResult(), ...mutation },
        {
          expectedRevision: REVISION,
          now: new Date("2026-08-24T02:00:30.000Z"),
        },
      ),
    ).rejects.toThrow("SBR_E2E_RESULT_INVALID");
    await expect(readFile(destination)).rejects.toThrow();
  });

  it("removes stale output before a run", async () => {
    const root = await mkdtemp(path.join(os.tmpdir(), "tammy-sbr-result-"));
    temporaryRoots.push(root);
    const destination = path.join(root, "result.json");
    await writeFile(destination, "stale");
    await removeSbrE2eResult(destination);
    await expect(readFile(destination)).rejects.toThrow();
  });

  it("removes stale PASSED evidence before fallible package verification", async () => {
    const root = await mkdtemp(path.join(os.tmpdir(), "tammy-sbr-result-"));
    temporaryRoots.push(root);
    const destination = path.join(root, "result.json");
    await writeFile(destination, JSON.stringify(validResult()), { mode: 0o600 });

    await expect(
      locatePackagedApplicationForProject("darwin-arm64-sbr", {
        locate: async () => {
          throw new Error("PACKAGE_VERIFICATION_FAILED");
        },
        resultPath: destination,
      }),
    ).rejects.toThrow("PACKAGE_VERIFICATION_FAILED");
    await expect(readFile(destination)).rejects.toThrow();
  });
});

describe("RFC 6238 code generation", () => {
  it.each([
    [59_000, "94287082"],
    [1_111_111_109_000, "07081804"],
    [1_111_111_111_000, "14050471"],
  ])("matches the SHA-1 reference vector at %i ms", (time, expected) => {
    expect(generateTotp("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", time, 8)).toBe(expected);
  });

  it("defaults to six digits and rejects malformed provisioning material", () => {
    expect(generateTotp("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", 59_000)).toBe("287082");
    expect(() => generateTotp("otpauth://totp/not-a-secret", 59_000)).toThrow(
      "INVALID_TOTP_SECRET",
    );
  });
});
