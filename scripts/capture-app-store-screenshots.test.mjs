import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, realpath, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import {
  assertScreenshotOrchestrationExternal,
  authenticateDevelopmentSignedApp,
  createCaptureProcessEnvironment,
  createScreenshotCapturePlan,
  executeCaptureProcess,
  executeScreenshotCapture,
  hashAppBundle,
  validateScreenshotCaptureContract,
} from "./capture-app-store-screenshots.mjs";

const commit = "a".repeat(40);
const tree = "b".repeat(40);

test("plans one exact development-signed five-image capture run", async () => {
  const repositoryRoot = await realpath(
    await mkdtemp(path.join(tmpdir(), "tammy-screenshot-plan-")),
  );
  try {
    await mkdir(path.join(repositoryRoot, "apps/desktop"), { recursive: true });
    await writeFile(
      path.join(repositoryRoot, "apps/desktop/package.json"),
      `${JSON.stringify({ version: "0.1.0" })}\n`,
    );
    const plan = await createScreenshotCapturePlan({
      buildNumber: "42",
      productSourceCommit: commit,
      productSourceTree: tree,
      repositoryRoot,
      runId: "01900000-0000-7000-8000-000000000001",
    });
    const releaseRoot = path.join(repositoryRoot, ".tmp/macos-release/0.1.0/build-42");
    assert.deepEqual(plan, {
      buildNumber: "42",
      captureDirectory: path.join(
        releaseRoot,
        "screenshots/01900000-0000-7000-8000-000000000001/capture",
      ),
      contractPath: path.join(
        releaseRoot,
        "screenshots/01900000-0000-7000-8000-000000000001/capture-contract.json",
      ),
      developmentApp: path.join(releaseRoot, "development/Tammy.app"),
      fixturePath: path.join(repositoryRoot, "apps/desktop/release/macos/screenshots/fixture.json"),
      marketingVersion: "0.1.0",
      playwrightConfig: path.join(
        repositoryRoot,
        "apps/desktop/playwright.app-store-screenshots.config.ts",
      ),
      productSourceCommit: commit,
      productSourceTree: tree,
      repositoryRoot,
      runRoot: path.join(releaseRoot, "screenshots/01900000-0000-7000-8000-000000000001"),
      storeMetadataPath: path.join(repositoryRoot, "apps/desktop/release/macos/store-metadata.md"),
      unsignedManifestPath: path.join(releaseRoot, "unsigned/unsigned-content.json"),
    });
  } finally {
    await rm(repositoryRoot, { force: true, recursive: true });
  }
});

test("rejects capture plans that are not exact immutable build inputs", async () => {
  const repositoryRoot = await realpath(
    await mkdtemp(path.join(tmpdir(), "tammy-screenshot-plan-invalid-")),
  );
  try {
    await mkdir(path.join(repositoryRoot, "apps/desktop"), { recursive: true });
    await writeFile(
      path.join(repositoryRoot, "apps/desktop/package.json"),
      `${JSON.stringify({ version: "0.1.0" })}\n`,
    );
    for (const changed of [
      { buildNumber: "0", productSourceCommit: commit, productSourceTree: tree },
      { buildNumber: "42", productSourceCommit: "dirty", productSourceTree: tree },
      { buildNumber: "42", productSourceCommit: commit, productSourceTree: "dirty" },
    ]) {
      await assert.rejects(
        createScreenshotCapturePlan({
          ...changed,
          repositoryRoot,
          runId: "01900000-0000-7000-8000-000000000001",
        }),
        /APP_STORE_SCREENSHOT_CAPTURE_INPUT_INVALID/,
      );
    }
  } finally {
    await rm(repositoryRoot, { force: true, recursive: true });
  }
});

test("proves screenshot orchestration is external to the packaged app", async () => {
  const temporary = await realpath(
    await mkdtemp(path.join(tmpdir(), "tammy-screenshot-app-boundary-")),
  );
  const app = path.join(temporary, "Tammy.app");
  const resources = path.join(app, "Contents/Resources");
  const macos = path.join(app, "Contents/MacOS");
  try {
    await Promise.all([mkdir(resources, { recursive: true }), mkdir(macos, { recursive: true })]);
    await Promise.all([
      writeFile(path.join(resources, "app.asar"), "ordinary packaged renderer\n"),
      writeFile(path.join(macos, "Tammy"), "ordinary executable\n"),
    ]);
    assert.equal(await assertScreenshotOrchestrationExternal(app), path.join(macos, "Tammy"));
    for (const marker of [
      "tammy-app-store-screenshots-en-au-v1",
      "app-store-screenshots.spec",
      "@playwright/test",
      "playwright-core",
      "01-overview.png",
      "02-document-review.png",
      "03-journal-trial-balance.png",
      "04-bank-reconciliation.png",
      "05-bas-draft.png",
      "TAMMY_APP_STORE_SCREENSHOT_CONTRACT",
      "TAMMY_SCREENSHOT_MODE",
      "/__e2e/seed",
      "app-store-reviewer-mode",
      "Harbour Office Supplies Pty Ltd",
      "Tammy fictional screenshot fixture",
    ]) {
      await writeFile(path.join(resources, "app.asar"), "ordinary packaged renderer\n");
      const target =
        marker === "playwright-core"
          ? path.join(resources, "app.asar.unpacked/native.node")
          : marker === "app-store-reviewer-mode"
            ? path.join(app, "Contents/Frameworks/Tammy Helper.app/Contents/MacOS/Tammy Helper")
            : path.join(resources, "app.asar");
      await mkdir(path.dirname(target), { recursive: true });
      await writeFile(target, marker);
      await assert.rejects(
        assertScreenshotOrchestrationExternal(app),
        /APP_STORE_SCREENSHOT_ORCHESTRATION_BUNDLED/,
      );
      if (target !== path.join(resources, "app.asar")) await rm(target);
    }
  } finally {
    await rm(temporary, { force: true, recursive: true });
  }
});

test("binds Task 11 equivalence and the complete development app hash", async () => {
  const temporary = await realpath(await mkdtemp(path.join(tmpdir(), "tammy-screenshot-hash-")));
  const app = path.join(temporary, "Tammy.app");
  try {
    await Promise.all([
      mkdir(path.join(app, "Contents/MacOS"), { recursive: true }),
      mkdir(path.join(app, "Contents/Resources"), { recursive: true }),
    ]);
    await Promise.all([
      writeFile(path.join(app, "Contents/MacOS/Tammy"), "launcher"),
      writeFile(path.join(app, "Contents/Resources/app.asar"), "renderer one"),
    ]);
    const calls = [];
    const first = await authenticateDevelopmentSignedApp({
      developmentApp: app,
      packagingPlan: { app, mode: "development" },
      unsignedManifest: { fixture: true },
      verifySignedCopy: async (...args) => calls.push(args),
    });
    assert.equal(calls.length, 1);
    assert.equal(calls[0][2], app);
    assert.equal(first, await hashAppBundle(app));
    await writeFile(path.join(app, "Contents/Resources/app.asar"), "renderer two");
    assert.notEqual(await hashAppBundle(app), first);
    await assert.rejects(
      authenticateDevelopmentSignedApp({
        developmentApp: app,
        packagingPlan: { app, mode: "development" },
        unsignedManifest: { fixture: true },
        verifySignedCopy: async () => {
          throw new Error("resource drift");
        },
      }),
      /APP_STORE_SCREENSHOT_DEVELOPMENT_APP_MISMATCH/,
    );
  } finally {
    await rm(temporary, { force: true, recursive: true });
  }
});

test("uses a deterministic secret-free capture process environment", () => {
  const environment = createCaptureProcessEnvironment(
    {
      HOME: "/tmp/home",
      NODE_OPTIONS: "--require=/tmp/hostile.cjs",
      PATH: "/tmp/hostile-bin",
      TAMMY_MACOS_SIGNING_IDENTITY: "secret",
      TMPDIR: "/tmp",
      USER: "tester",
    },
    { TAMMY_APP_STORE_SCREENSHOT_CONTRACT: "/tmp/contract.json" },
  );
  assert.deepEqual(environment, {
    CI: "true",
    HOME: "/tmp/home",
    LANG: "en_AU.UTF-8",
    LC_ALL: "en_AU.UTF-8",
    NO_COLOR: "1",
    TAMMY_APP_STORE_SCREENSHOT_CONTRACT: "/tmp/contract.json",
    TMPDIR: "/tmp",
    TZ: "Australia/Melbourne",
    USER: "tester",
  });
});

test("rejects a screenshot output ancestor symlink without writing outside", async () => {
  const repositoryRoot = await realpath(
    await mkdtemp(path.join(tmpdir(), "tammy-screenshot-output-")),
  );
  const outside = await realpath(await mkdtemp(path.join(tmpdir(), "tammy-screenshot-outside-")));
  try {
    await mkdir(path.join(repositoryRoot, "apps/desktop"), { recursive: true });
    await writeFile(
      path.join(repositoryRoot, "apps/desktop/package.json"),
      '{"version":"0.1.0"}\n',
    );
    await symlink(outside, path.join(repositoryRoot, ".tmp"));
    const plan = await createScreenshotCapturePlan({
      buildNumber: "42",
      productSourceCommit: commit,
      productSourceTree: tree,
      repositoryRoot,
      runId: "01900000-0000-7000-8000-000000000001",
    });
    await assert.rejects(
      executeScreenshotCapture(plan, {
        authenticateInputs: async () => {
          throw new Error("must not authenticate escaped output");
        },
      }),
      /APP_STORE_SCREENSHOT_CAPTURE_OUTPUT_INVALID/,
    );
    assert.deepEqual(await import("node:fs/promises").then(({ readdir }) => readdir(outside)), []);
  } finally {
    await Promise.all([
      rm(repositoryRoot, { force: true, recursive: true }),
      rm(outside, { force: true, recursive: true }),
    ]);
  }
});

test("rejects a mutated derived plan path before filesystem access", async () => {
  const repositoryRoot = await realpath(
    await mkdtemp(path.join(tmpdir(), "tammy-screenshot-plan-path-")),
  );
  const outside = path.join(
    await realpath(await mkdtemp(path.join(tmpdir(), "tammy-screenshot-plan-outside-"))),
    "contract.json",
  );
  try {
    await mkdir(path.join(repositoryRoot, "apps/desktop"), { recursive: true });
    await writeFile(
      path.join(repositoryRoot, "apps/desktop/package.json"),
      '{"version":"0.1.0"}\n',
    );
    const plan = await createScreenshotCapturePlan({
      buildNumber: "42",
      productSourceCommit: commit,
      productSourceTree: tree,
      repositoryRoot,
      runId: "01900000-0000-7000-8000-000000000001",
    });
    await assert.rejects(
      executeScreenshotCapture(
        { ...plan, contractPath: outside },
        {
          authenticateInputs: async () => {
            throw new Error("must not authenticate a mutated plan");
          },
        },
      ),
      /APP_STORE_SCREENSHOT_CAPTURE_PLAN_INVALID/,
    );
    await assert.rejects(readFile(outside), /ENOENT/u);
  } finally {
    await Promise.all([
      rm(repositoryRoot, { force: true, recursive: true }),
      rm(path.dirname(outside), { force: true, recursive: true }),
    ]);
  }
});

test("reaps a stubborn descendant after an abnormal capture leader exit", async (context) => {
  if (process.platform === "win32") return context.skip("POSIX process-group regression");
  const temporary = await realpath(await mkdtemp(path.join(tmpdir(), "tammy-screenshot-process-")));
  const pidPath = path.join(temporary, "descendant.pid");
  let descendantPid;
  try {
    const leader = `
      const { spawn } = require("node:child_process");
      const { writeFileSync } = require("node:fs");
      const child = spawn(process.execPath, ["-e", "process.on('SIGTERM',()=>{});setInterval(()=>{},1000)"], { stdio: "ignore" });
      writeFileSync(process.argv[1], String(child.pid));
      setTimeout(() => process.exit(23), 75);
    `;
    await assert.rejects(
      executeCaptureProcess(process.execPath, ["-e", leader, pidPath], {
        cwd: temporary,
        env: { TAMMY_APP_STORE_SCREENSHOT_CONTRACT: path.join(temporary, "contract.json") },
      }),
      /APP_STORE_SCREENSHOT_CAPTURE_FAILED/,
    );
    descendantPid = Number(await readFile(pidPath, "utf8"));
    assert.ok(Number.isSafeInteger(descendantPid) && descendantPid > 1);
    assert.throws(
      () => process.kill(descendantPid, 0),
      (error) => error?.code === "ESRCH",
    );
  } finally {
    if (descendantPid) {
      try {
        process.kill(descendantPid, "SIGKILL");
      } catch {}
    }
    await rm(temporary, { force: true, recursive: true });
  }
});

test("executes the owner with a private strict contract and validates its capture", async () => {
  const repositoryRoot = await realpath(
    await mkdtemp(path.join(tmpdir(), "tammy-screenshot-execute-")),
  );
  try {
    await mkdir(path.join(repositoryRoot, "apps/desktop"), { recursive: true });
    await writeFile(
      path.join(repositoryRoot, "apps/desktop/package.json"),
      `${JSON.stringify({ version: "0.1.0" })}\n`,
    );
    const plan = await createScreenshotCapturePlan({
      buildNumber: "42",
      productSourceCommit: commit,
      productSourceTree: tree,
      repositoryRoot,
      runId: "01900000-0000-7000-8000-000000000001",
    });
    const expectedCaptureDirectory = plan.captureDirectory;
    const expectedContractPath = plan.contractPath;
    const mutatedContractPath = path.join(repositoryRoot, "mutated-contract.json");
    const calls = [];
    const result = await executeScreenshotCapture(plan, {
      authenticateInputs: async (ownedPlan) => {
        assert.notEqual(ownedPlan, plan);
        plan.contractPath = mutatedContractPath;
        return {
          developmentSignedAppSha256: "c".repeat(64),
          fixtureSha256: "d".repeat(64),
          unsignedContentManifestSha256: "e".repeat(64),
        };
      },
      execute: async (command, args, options) => {
        calls.push({ args, command, options });
        await mkdir(expectedCaptureDirectory);
      },
      hashDevelopmentApp: async () => "c".repeat(64),
      now: () => new Date("2026-08-30T12:00:00.000Z"),
      validateCapture: async ({ captureDirectory }) => {
        assert.equal(captureDirectory, expectedCaptureDirectory);
        return { locale: "en-AU" };
      },
    });
    assert.deepEqual(result, { captureDirectory: expectedCaptureDirectory, locale: "en-AU" });
    assert.equal(calls.length, 1);
    assert.equal(calls[0].command, process.execPath);
    assert.match(calls[0].args[0], /@playwright[/+]test.*cli\.js$/u);
    assert.deepEqual(calls[0].args.slice(1), [
      "test",
      "--config",
      "playwright.app-store-screenshots.config.ts",
      "--workers=1",
    ]);
    assert.equal(calls[0].options.cwd, path.join(repositoryRoot, "apps/desktop"));
    assert.deepEqual(Object.keys(calls[0].options.env), ["TAMMY_APP_STORE_SCREENSHOT_CONTRACT"]);
    const contract = validateScreenshotCaptureContract(
      JSON.parse(await readFile(expectedContractPath, "utf8")),
    );
    await assert.rejects(readFile(mutatedContractPath), /ENOENT/u);
    assert.equal(contract.captureDirectory, expectedCaptureDirectory);
    assert.equal(contract.captureArtifactKind, "development-signed-app");
    assert.equal(contract.capturedAt, "2026-08-30T12:00:00.000Z");
  } finally {
    await rm(repositoryRoot, { force: true, recursive: true });
  }
});
