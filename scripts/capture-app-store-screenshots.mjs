import { spawn } from "node:child_process";
import { createHash, randomUUID } from "node:crypto";
import {
  lstat,
  mkdir,
  open,
  readdir,
  readFile,
  readlink,
  realpath,
  rename,
  rm,
  writeFile,
} from "node:fs/promises";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { isDeepStrictEqual } from "node:util";

import {
  validateScreenshotFixture,
  validateScreenshotManifest,
} from "./check-app-store-screenshots.mjs";
import { validateUnsignedContentManifest } from "./macos-unsigned-content.mjs";
import { createMacOSStoreBuildPlan, verifySignedCopyEquivalence } from "./package-macos-store.mjs";

const BUILD = /^[1-9][0-9]*$/u;
const SHA40 = /^[0-9a-f]{40}$/u;
const SHA256 = /^[0-9a-f]{64}$/u;
const VERSION = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/u;
const UUID_V7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u;
const UTC = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/u;
const PLAYWRIGHT_CLI = createRequire(
  path.resolve(import.meta.dirname, "../apps/desktop/package.json"),
).resolve("@playwright/test/cli");
const MAX_APP_BYTES = 2 * 1024 * 1024 * 1024;
const CAPTURE_TIMEOUT_MS = 240_000;
const FORBIDDEN_ORCHESTRATION = [
  "tammy-app-store-screenshots-en-au-v1",
  "app-store-screenshots.spec",
  "playwright.app-store-screenshots",
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
];

function fail(code = "APP_STORE_SCREENSHOT_CAPTURE_INPUT_INVALID") {
  throw new Error(code);
}

function record(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function exactKeys(value, keys) {
  return (
    record(value) &&
    Object.keys(value).length === keys.length &&
    keys.every((key) => Object.hasOwn(value, key))
  );
}

function hash(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

async function stableBytes(file, maximum, code) {
  const before = await lstat(file, { bigint: true }).catch(() => fail(code));
  if (!before.isFile() || before.isSymbolicLink() || before.size > BigInt(maximum)) fail(code);
  const bytes = await readFile(file).catch(() => fail(code));
  const after = await lstat(file, { bigint: true }).catch(() => fail(code));
  if (
    before.dev !== after.dev ||
    before.ino !== after.ino ||
    before.size !== after.size ||
    before.mtimeNs !== after.mtimeNs ||
    bytes.length !== Number(before.size)
  ) {
    fail(code);
  }
  return bytes;
}

async function fileHash(file, maximum = 1024 * 1024 * 1024) {
  const pathStatus = await lstat(file, { bigint: true }).catch(() => fail());
  if (!pathStatus.isFile() || pathStatus.isSymbolicLink()) fail();
  const handle = await open(file, "r").catch(() => fail());
  const digest = createHash("sha256");
  let size = 0;
  try {
    const before = await handle.stat({ bigint: true });
    for await (const chunk of handle.createReadStream({ autoClose: false })) {
      size += chunk.length;
      if (size > maximum) fail();
      digest.update(chunk);
    }
    const after = await handle.stat({ bigint: true });
    if (
      before.dev !== after.dev ||
      before.ino !== after.ino ||
      before.size !== after.size ||
      before.mtimeNs !== after.mtimeNs ||
      size !== Number(before.size)
    ) {
      fail();
    }
    return digest.digest("hex");
  } finally {
    await handle.close();
  }
}

function containedRelativePath(parent, candidate) {
  const relative = path.relative(parent, candidate);
  if (
    relative === "" ||
    path.isAbsolute(relative) ||
    relative
      .split(path.sep)
      .some((segment) => segment === "" || segment === "." || segment === "..")
  ) {
    fail("APP_STORE_SCREENSHOT_CAPTURE_OUTPUT_INVALID");
  }
  return relative;
}

async function ensureContainedOutputParent(repositoryRoot, outputParent) {
  const repositoryStatus = await lstat(repositoryRoot).catch(() => fail());
  if (
    !repositoryStatus.isDirectory() ||
    repositoryStatus.isSymbolicLink() ||
    (await realpath(repositoryRoot).catch(() => fail())) !== repositoryRoot
  ) {
    fail("APP_STORE_SCREENSHOT_CAPTURE_OUTPUT_INVALID");
  }
  const relative = containedRelativePath(repositoryRoot, outputParent);
  let current = repositoryRoot;
  for (const segment of relative.split(path.sep)) {
    current = path.join(current, segment);
    await mkdir(current, { mode: 0o700 }).catch((error) => {
      if (error?.code !== "EEXIST") throw error;
    });
    const status = await lstat(current).catch(() => fail());
    if (
      !status.isDirectory() ||
      status.isSymbolicLink() ||
      (await realpath(current).catch(() => fail())) !== current
    ) {
      fail("APP_STORE_SCREENSHOT_CAPTURE_OUTPUT_INVALID");
    }
  }
}

async function collectAppBundleEntries(app) {
  const rootStatus = await lstat(app, { bigint: true }).catch(() => fail());
  if (
    !rootStatus.isDirectory() ||
    rootStatus.isSymbolicLink() ||
    (await realpath(app).catch(() => fail())) !== app
  ) {
    fail("APP_STORE_SCREENSHOT_DEVELOPMENT_APP_INVALID");
  }
  let totalBytes = 0;
  const entries = [];
  async function visit(directory) {
    const before = await lstat(directory, { bigint: true }).catch(() => fail());
    const children = await readdir(directory, { withFileTypes: true }).catch(() => fail());
    children.sort((left, right) => Buffer.compare(Buffer.from(left.name), Buffer.from(right.name)));
    for (const child of children) {
      const target = path.join(directory, child.name);
      const relative = path.relative(app, target).split(path.sep).join("/");
      if (
        !relative ||
        relative.split("/").some((segment) => !segment || segment === "." || segment === "..")
      )
        fail();
      const status = await lstat(target, { bigint: true }).catch(() => fail());
      if (status.isSymbolicLink()) {
        const linkTarget = await readlink(target).catch(() => fail());
        const resolved = await realpath(target).catch(() => fail());
        if (
          path.isAbsolute(linkTarget) ||
          (resolved !== app && !resolved.startsWith(`${app}${path.sep}`))
        ) {
          fail("APP_STORE_SCREENSHOT_DEVELOPMENT_APP_INVALID");
        }
        entries.push({ kind: "symlink", linkTarget, path: relative });
      } else if (status.isDirectory()) {
        await visit(target);
      } else if (status.isFile()) {
        totalBytes += Number(status.size);
        if (totalBytes > MAX_APP_BYTES) fail("APP_STORE_SCREENSHOT_DEVELOPMENT_APP_INVALID");
        entries.push({
          byteSize: Number(status.size),
          executable: (Number(status.mode) & 0o111) !== 0,
          kind: "file",
          path: relative,
          sha256: await fileHash(target, MAX_APP_BYTES),
        });
      } else {
        fail("APP_STORE_SCREENSHOT_DEVELOPMENT_APP_INVALID");
      }
    }
    const after = await lstat(directory, { bigint: true }).catch(() => fail());
    const afterNames = (await readdir(directory).catch(() => fail())).sort((left, right) =>
      Buffer.compare(Buffer.from(left), Buffer.from(right)),
    );
    if (
      before.dev !== after.dev ||
      before.ino !== after.ino ||
      before.mtimeNs !== after.mtimeNs ||
      !isDeepStrictEqual(
        children.map(({ name }) => name),
        afterNames,
      )
    ) {
      fail("APP_STORE_SCREENSHOT_DEVELOPMENT_APP_CHANGED");
    }
  }
  await visit(app);
  entries.sort((left, right) => Buffer.compare(Buffer.from(left.path), Buffer.from(right.path)));
  return entries;
}

export async function hashAppBundle(app) {
  return hash(Buffer.from(JSON.stringify(await collectAppBundleEntries(app)), "utf8"));
}

export async function createScreenshotCapturePlan({
  buildNumber,
  productSourceCommit,
  productSourceTree,
  repositoryRoot,
  runId,
}) {
  if (
    typeof repositoryRoot !== "string" ||
    !path.isAbsolute(repositoryRoot) ||
    path.normalize(repositoryRoot) !== repositoryRoot ||
    !BUILD.test(buildNumber) ||
    !SHA40.test(productSourceCommit) ||
    !SHA40.test(productSourceTree) ||
    !UUID_V7.test(runId)
  ) {
    fail();
  }
  const status = await lstat(repositoryRoot).catch(() => fail());
  if (
    !status.isDirectory() ||
    status.isSymbolicLink() ||
    (await realpath(repositoryRoot)) !== repositoryRoot
  ) {
    fail();
  }
  let desktopPackage;
  try {
    desktopPackage = JSON.parse(
      await stableBytes(
        path.join(repositoryRoot, "apps/desktop/package.json"),
        64 * 1024,
        "APP_STORE_SCREENSHOT_CAPTURE_INPUT_INVALID",
      ),
    );
  } catch {
    fail();
  }
  if (!record(desktopPackage) || !VERSION.test(desktopPackage.version)) fail();
  const marketingVersion = desktopPackage.version;
  const releaseRoot = path.join(
    repositoryRoot,
    ".tmp/macos-release",
    marketingVersion,
    `build-${buildNumber}`,
  );
  const runRoot = path.join(releaseRoot, "screenshots", runId);
  return {
    buildNumber,
    captureDirectory: path.join(runRoot, "capture"),
    contractPath: path.join(runRoot, "capture-contract.json"),
    developmentApp: path.join(releaseRoot, "development/Tammy.app"),
    fixturePath: path.join(repositoryRoot, "apps/desktop/release/macos/screenshots/fixture.json"),
    marketingVersion,
    playwrightConfig: path.join(
      repositoryRoot,
      "apps/desktop/playwright.app-store-screenshots.config.ts",
    ),
    productSourceCommit,
    productSourceTree,
    repositoryRoot,
    runRoot,
    storeMetadataPath: path.join(repositoryRoot, "apps/desktop/release/macos/store-metadata.md"),
    unsignedManifestPath: path.join(releaseRoot, "unsigned/unsigned-content.json"),
  };
}

export async function validateScreenshotCapturePlan(plan) {
  if (
    !exactKeys(plan, [
      "buildNumber",
      "captureDirectory",
      "contractPath",
      "developmentApp",
      "fixturePath",
      "marketingVersion",
      "playwrightConfig",
      "productSourceCommit",
      "productSourceTree",
      "repositoryRoot",
      "runRoot",
      "storeMetadataPath",
      "unsignedManifestPath",
    ]) ||
    typeof plan.runRoot !== "string"
  ) {
    fail("APP_STORE_SCREENSHOT_CAPTURE_PLAN_INVALID");
  }
  let expected;
  try {
    expected = await createScreenshotCapturePlan({
      buildNumber: plan.buildNumber,
      productSourceCommit: plan.productSourceCommit,
      productSourceTree: plan.productSourceTree,
      repositoryRoot: plan.repositoryRoot,
      runId: path.basename(plan.runRoot),
    });
  } catch {
    fail("APP_STORE_SCREENSHOT_CAPTURE_PLAN_INVALID");
  }
  if (!isDeepStrictEqual(plan, expected)) fail("APP_STORE_SCREENSHOT_CAPTURE_PLAN_INVALID");
  return Object.freeze(expected);
}

export function validateScreenshotCaptureContract(value) {
  if (
    !exactKeys(value, [
      "buildNumber",
      "captureArtifactKind",
      "captureDirectory",
      "capturedAt",
      "developmentApp",
      "developmentSignedAppSha256",
      "dimensions",
      "fixturePath",
      "fixtureSha256",
      "kind",
      "locale",
      "marketingVersion",
      "productSourceCommit",
      "productSourceTree",
      "schemaVersion",
      "storeMetadataPath",
      "timezone",
      "unsignedContentManifestPath",
      "unsignedContentManifestSha256",
    ]) ||
    value.schemaVersion !== 1 ||
    value.kind !== "app-store-screenshot-capture" ||
    value.captureArtifactKind !== "development-signed-app" ||
    value.locale !== "en-AU" ||
    value.timezone !== "Australia/Melbourne" ||
    !exactKeys(value.dimensions, ["height", "width"]) ||
    value.dimensions.width !== 1440 ||
    value.dimensions.height !== 900 ||
    !BUILD.test(value.buildNumber) ||
    !VERSION.test(value.marketingVersion) ||
    !SHA40.test(value.productSourceCommit) ||
    !SHA40.test(value.productSourceTree) ||
    !SHA256.test(value.developmentSignedAppSha256) ||
    !SHA256.test(value.fixtureSha256) ||
    !SHA256.test(value.unsignedContentManifestSha256) ||
    !UTC.test(value.capturedAt) ||
    ![
      value.captureDirectory,
      value.developmentApp,
      value.fixturePath,
      value.storeMetadataPath,
      value.unsignedContentManifestPath,
    ].every(
      (entry) =>
        typeof entry === "string" && path.isAbsolute(entry) && path.normalize(entry) === entry,
    )
  ) {
    fail("APP_STORE_SCREENSHOT_CAPTURE_CONTRACT_INVALID");
  }
  const repositoryRoot = path.resolve(path.dirname(value.fixturePath), "../../../../..");
  const releaseRoot = path.join(
    repositoryRoot,
    ".tmp/macos-release",
    value.marketingVersion,
    `build-${value.buildNumber}`,
  );
  const runRoot = path.dirname(value.captureDirectory);
  if (
    value.fixturePath !==
      path.join(repositoryRoot, "apps/desktop/release/macos/screenshots/fixture.json") ||
    value.storeMetadataPath !==
      path.join(repositoryRoot, "apps/desktop/release/macos/store-metadata.md") ||
    value.developmentApp !== path.join(releaseRoot, "development/Tammy.app") ||
    value.unsignedContentManifestPath !==
      path.join(releaseRoot, "unsigned/unsigned-content.json") ||
    value.captureDirectory !== path.join(runRoot, "capture") ||
    path.dirname(runRoot) !== path.join(releaseRoot, "screenshots") ||
    !UUID_V7.test(path.basename(runRoot))
  ) {
    fail("APP_STORE_SCREENSHOT_CAPTURE_CONTRACT_INVALID");
  }
  return value;
}

export async function assertScreenshotOrchestrationExternal(developmentApp) {
  const executable = path.join(developmentApp, "Contents/MacOS/Tammy");
  const appStatus = await lstat(developmentApp).catch(() => fail());
  if (!appStatus.isDirectory() || appStatus.isSymbolicLink()) fail();
  const names = [];
  let contentBytes = 0;
  async function visit(directory, depth) {
    if (depth > 24 || names.length > 40_000) fail();
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      const target = path.join(directory, entry.name);
      names.push(path.relative(developmentApp, target));
      if (entry.isDirectory()) await visit(target, depth + 1);
      else if (entry.isFile()) {
        const bytes = await stableBytes(
          target,
          512 * 1024 * 1024,
          "APP_STORE_SCREENSHOT_ORCHESTRATION_BUNDLED",
        );
        contentBytes += bytes.length;
        if (contentBytes > MAX_APP_BYTES) fail("APP_STORE_SCREENSHOT_ORCHESTRATION_BUNDLED");
        if (FORBIDDEN_ORCHESTRATION.some((marker) => bytes.includes(marker))) {
          fail("APP_STORE_SCREENSHOT_ORCHESTRATION_BUNDLED");
        }
      } else if (!entry.isSymbolicLink()) {
        fail("APP_STORE_SCREENSHOT_ORCHESTRATION_BUNDLED");
      }
    }
  }
  await visit(developmentApp, 0);
  const inventory = names.join("\n");
  const executableBytes = await stableBytes(
    executable,
    512 * 1024 * 1024,
    "APP_STORE_SCREENSHOT_ORCHESTRATION_BUNDLED",
  );
  for (const marker of FORBIDDEN_ORCHESTRATION) {
    if (inventory.includes(marker) || executableBytes.includes(marker)) {
      fail("APP_STORE_SCREENSHOT_ORCHESTRATION_BUNDLED");
    }
  }
  return executable;
}

export async function authenticateDevelopmentSignedApp({
  developmentApp,
  packagingPlan,
  unsignedManifest,
  verifySignedCopy = verifySignedCopyEquivalence,
}) {
  if (
    !record(packagingPlan) ||
    packagingPlan.mode !== "development" ||
    packagingPlan.app !== developmentApp ||
    typeof verifySignedCopy !== "function"
  ) {
    fail();
  }
  await verifySignedCopy(packagingPlan, unsignedManifest, developmentApp).catch(() =>
    fail("APP_STORE_SCREENSHOT_DEVELOPMENT_APP_MISMATCH"),
  );
  return hashAppBundle(developmentApp);
}

export async function authenticateScreenshotCaptureInputs(
  plan,
  {
    createBuildPlan = createMacOSStoreBuildPlan,
    verifyDevelopmentApp = authenticateDevelopmentSignedApp,
  } = {},
) {
  const [fixtureBytes, unsignedBytes] = await Promise.all([
    stableBytes(plan.fixturePath, 256 * 1024, "APP_STORE_SCREENSHOT_CAPTURE_INPUT_INVALID"),
    stableBytes(
      plan.unsignedManifestPath,
      16 * 1024 * 1024,
      "APP_STORE_SCREENSHOT_CAPTURE_INPUT_INVALID",
    ),
  ]);
  let fixture;
  let unsignedManifest;
  try {
    fixture = validateScreenshotFixture(JSON.parse(fixtureBytes));
    unsignedManifest = validateUnsignedContentManifest(JSON.parse(unsignedBytes));
  } catch {
    fail();
  }
  if (
    unsignedManifest.buildNumber !== plan.buildNumber ||
    unsignedManifest.marketingVersion !== plan.marketingVersion ||
    unsignedManifest.productSourceCommit !== plan.productSourceCommit ||
    unsignedManifest.productSourceTree !== plan.productSourceTree
  ) {
    fail();
  }
  await assertScreenshotOrchestrationExternal(plan.developmentApp);
  const packagingPlan = createBuildPlan(plan.repositoryRoot, process.env);
  if (
    packagingPlan.releaseRoot !== path.dirname(path.dirname(plan.developmentApp)) ||
    packagingPlan.unsignedManifest !== plan.unsignedManifestPath
  ) {
    fail();
  }
  return {
    developmentSignedAppSha256: await verifyDevelopmentApp({
      developmentApp: plan.developmentApp,
      packagingPlan,
      unsignedManifest,
    }),
    fixture,
    fixtureBytes,
    fixtureSha256: hash(fixtureBytes),
    storeMetadataBytes: await stableBytes(
      plan.storeMetadataPath,
      1024 * 1024,
      "APP_STORE_SCREENSHOT_CAPTURE_INPUT_INVALID",
    ),
    unsignedContentManifestSha256: hash(unsignedBytes),
  };
}

export function createCaptureProcessEnvironment(source, additions) {
  const environment = {
    CI: "true",
    LANG: "en_AU.UTF-8",
    LC_ALL: "en_AU.UTF-8",
    NO_COLOR: "1",
    TZ: "Australia/Melbourne",
  };
  for (const name of ["HOME", "TMPDIR", "USER", "LOGNAME"]) {
    const value = source[name];
    if (typeof value === "string" && value.length > 0 && !value.includes("\0")) {
      if (["HOME", "TMPDIR"].includes(name) && !path.isAbsolute(value)) fail();
      environment[name] = value;
    }
  }
  if (
    !exactKeys(additions, ["TAMMY_APP_STORE_SCREENSHOT_CONTRACT"]) ||
    typeof additions.TAMMY_APP_STORE_SCREENSHOT_CONTRACT !== "string" ||
    !path.isAbsolute(additions.TAMMY_APP_STORE_SCREENSHOT_CONTRACT)
  ) {
    fail("APP_STORE_SCREENSHOT_CAPTURE_ENVIRONMENT_INVALID");
  }
  environment.TAMMY_APP_STORE_SCREENSHOT_CONTRACT = additions.TAMMY_APP_STORE_SCREENSHOT_CONTRACT;
  return environment;
}

export function executeCaptureProcess(command, args, { cwd, env }) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd,
      detached: process.platform !== "win32",
      env: createCaptureProcessEnvironment(process.env, env),
      shell: false,
      stdio: "inherit",
    });
    let settled = false;
    let timedOut = false;
    const groupExists = () => {
      if (!child.pid || process.platform === "win32") return false;
      try {
        process.kill(-child.pid, 0);
        return true;
      } catch {
        return false;
      }
    };
    const terminate = (signal) => {
      if (!child.pid) return;
      try {
        if (process.platform === "win32") {
          if (child.exitCode === null && child.signalCode === null) child.kill(signal);
        } else process.kill(-child.pid, signal);
      } catch {}
    };
    const rejectAfterGroupCleanup = async (error) => {
      if (settled) return;
      settled = true;
      terminate("SIGTERM");
      const forceAt = Date.now() + 250;
      const deadline = Date.now() + 5_000;
      while (groupExists() && Date.now() < deadline) {
        if (Date.now() >= forceAt) terminate("SIGKILL");
        await new Promise((resume) => setTimeout(resume, 25));
      }
      terminate("SIGKILL");
      reject(error);
    };
    const timeout = setTimeout(() => {
      timedOut = true;
      void rejectAfterGroupCleanup(new Error("APP_STORE_SCREENSHOT_CAPTURE_TIMEOUT"));
    }, CAPTURE_TIMEOUT_MS);
    timeout.unref();
    child.once("error", () => {
      clearTimeout(timeout);
      void rejectAfterGroupCleanup(new Error("APP_STORE_SCREENSHOT_CAPTURE_FAILED"));
    });
    child.once("close", (code, signal) => {
      clearTimeout(timeout);
      if (code === 0 && signal === null && !groupExists()) {
        if (!settled) {
          settled = true;
          resolve();
        }
      } else {
        void rejectAfterGroupCleanup(
          new Error(
            timedOut
              ? "APP_STORE_SCREENSHOT_CAPTURE_TIMEOUT"
              : "APP_STORE_SCREENSHOT_CAPTURE_FAILED",
          ),
        );
      }
    });
  });
}

export async function executeScreenshotCapture(
  plan,
  {
    authenticateInputs = authenticateScreenshotCaptureInputs,
    execute = executeCaptureProcess,
    hashDevelopmentApp = hashAppBundle,
    now = () => new Date(),
    validateCapture = validateScreenshotManifest,
  } = {},
) {
  plan = await validateScreenshotCapturePlan(plan);
  await ensureContainedOutputParent(plan.repositoryRoot, path.dirname(plan.runRoot));
  if (
    await lstat(plan.runRoot).then(
      () => true,
      () => false,
    )
  ) {
    fail("APP_STORE_SCREENSHOT_CAPTURE_OUTPUT_EXISTS");
  }
  const authenticated = await authenticateInputs(plan);
  const contract = validateScreenshotCaptureContract({
    buildNumber: plan.buildNumber,
    captureArtifactKind: "development-signed-app",
    captureDirectory: plan.captureDirectory,
    capturedAt: now().toISOString(),
    developmentApp: plan.developmentApp,
    developmentSignedAppSha256: authenticated.developmentSignedAppSha256,
    dimensions: { height: 900, width: 1440 },
    fixturePath: plan.fixturePath,
    fixtureSha256: authenticated.fixtureSha256,
    kind: "app-store-screenshot-capture",
    locale: "en-AU",
    marketingVersion: plan.marketingVersion,
    productSourceCommit: plan.productSourceCommit,
    productSourceTree: plan.productSourceTree,
    schemaVersion: 1,
    storeMetadataPath: plan.storeMetadataPath,
    timezone: "Australia/Melbourne",
    unsignedContentManifestPath: plan.unsignedManifestPath,
    unsignedContentManifestSha256: authenticated.unsignedContentManifestSha256,
  });
  await ensureContainedOutputParent(plan.repositoryRoot, path.dirname(plan.runRoot));
  await mkdir(plan.runRoot, { recursive: false, mode: 0o700 });
  const temporaryContract = `${plan.contractPath}.tmp-${randomUUID()}`;
  try {
    await writeFile(temporaryContract, `${JSON.stringify(contract, null, 2)}\n`, { mode: 0o600 });
    await rename(temporaryContract, plan.contractPath);
    await execute(
      process.execPath,
      [PLAYWRIGHT_CLI, "test", "--config", path.basename(plan.playwrightConfig), "--workers=1"],
      {
        cwd: path.join(plan.repositoryRoot, "apps/desktop"),
        env: { TAMMY_APP_STORE_SCREENSHOT_CONTRACT: plan.contractPath },
      },
    );
    if ((await hashDevelopmentApp(plan.developmentApp)) !== contract.developmentSignedAppSha256) {
      fail("APP_STORE_SCREENSHOT_DEVELOPMENT_APP_CHANGED");
    }
    const manifest = await validateCapture({
      captureDirectory: plan.captureDirectory,
      fixture: authenticated.fixture,
      fixtureBytes: authenticated.fixtureBytes,
      storeMetadataBytes: authenticated.storeMetadataBytes,
    });
    return { captureDirectory: plan.captureDirectory, locale: manifest.locale };
  } catch (error) {
    await rm(temporaryContract, { force: true }).catch(() => {});
    throw error;
  }
}

async function main() {
  const arguments_ = process.argv.slice(2);
  if (arguments_.length === 1 && arguments_[0] === "--help") {
    process.stdout.write(
      "Capture an existing exact development-signed app with --build, --source-commit, --source-tree, and --run-id.\n",
    );
    return;
  }
  const keys = ["--build", "--source-commit", "--source-tree", "--run-id"];
  if (
    arguments_.length !== 8 ||
    keys.some((key) => {
      const index = arguments_.indexOf(key);
      return index < 0 || index % 2 !== 0 || index === arguments_.length - 1;
    })
  ) {
    fail("APP_STORE_SCREENSHOT_CAPTURE_ARGUMENTS_INVALID");
  }
  const value = (key) => arguments_[arguments_.indexOf(key) + 1];
  const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const plan = await createScreenshotCapturePlan({
    buildNumber: value("--build"),
    productSourceCommit: value("--source-commit"),
    productSourceTree: value("--source-tree"),
    repositoryRoot,
    runId: value("--run-id"),
  });
  const result = await executeScreenshotCapture(plan);
  process.stdout.write(`${JSON.stringify(result)}\n`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    process.stderr.write(
      `${error instanceof Error ? error.message : "APP_STORE_SCREENSHOT_CAPTURE_FAILED"}\n`,
    );
    process.exitCode = 1;
  });
}
