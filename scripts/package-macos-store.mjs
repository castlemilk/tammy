import { execFile as nodeExecFile, spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import {
  cp,
  lstat,
  mkdir,
  mkdtemp,
  open,
  readFile,
  realpath,
  rename,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { isDeepStrictEqual, promisify } from "node:util";

import { signAsync } from "@electron/osx-sign";
import plist from "plist";

import { parseCanonicalBuildManifest } from "./build-manifest-schema.mjs";
import { validateMacOSReleaseEnvironment } from "./check-macos-store.mjs";
import { readProductSource } from "./macos-release-provenance.mjs";
import {
  authenticateUnsignedContentManifest,
  cloneUnsignedStaging,
  compareSignedCopies,
  createUnsignedContentManifest,
  validateSignedCopyAgainstUnsignedManifest,
  validateUnsignedContentManifest,
  writeUnsignedContentManifest,
} from "./macos-unsigned-content.mjs";
import { acquireSbrBuildLock, SBR_BUILD_LOCK_ENV } from "./sbr-build-lock.mjs";
import { authenticateSbrProfileBytes } from "./sbr-profile-schema.mjs";

const MAX_CAPTURED_OUTPUT_BYTES = 16 * 1024;
const execFile = promisify(nodeExecFile);

function expandEntitlementVariables(value, teamID) {
  if (typeof value === "string") {
    return value
      .replaceAll("$(TeamIdentifierPrefix)", `${teamID}.`)
      .replaceAll("$(AppIdentifierPrefix)", `${teamID}.`);
  }
  if (Array.isArray(value)) return value.map((item) => expandEntitlementVariables(item, teamID));
  if (value !== null && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).map(([key, child]) => [key, expandEntitlementVariables(child, teamID)]),
    );
  }
  return value;
}

async function expectedEntitlementObject(plan, filename) {
  const source = path.join(plan.root, "apps", "desktop", "release", "macos", filename);
  let value;
  try {
    value = expandEntitlementVariables(
      plist.parse(await readFile(source, "utf8")),
      plan.environment.TAMMY_MACOS_TEAM_ID,
    );
  } catch {
    throw new Error("MACOS_SIGNED_COPY_MISMATCH");
  }
  if (filename === "entitlements.mas.plist") {
    value["com.apple.application-identifier"] =
      `${plan.environment.TAMMY_MACOS_TEAM_ID}.com.tammy.desktop`;
    value["com.apple.developer.team-identifier"] = plan.environment.TAMMY_MACOS_TEAM_ID;
  }
  return value;
}

export async function prepareMacOSEntitlements(plan) {
  const temporary = await mkdtemp(path.join(tmpdir(), "tammy-mas-entitlements-"));
  const files = {};
  try {
    for (const filename of [
      "entitlements.mas.plist",
      "entitlements.mas.child.plist",
      "entitlements.mas.core.plist",
      "entitlements.mas.sbr-helper.plist",
    ]) {
      const target = path.join(temporary, filename);
      await writeFile(target, plist.build(await expectedEntitlementObject(plan, filename)), {
        encoding: "utf8",
        mode: 0o600,
      });
      files[filename] = target;
    }
    return {
      files: Object.freeze(files),
      async cleanup() {
        await rm(temporary, { force: true, recursive: true });
      },
    };
  } catch (error) {
    await rm(temporary, { force: true, recursive: true }).catch(() => {});
    throw error;
  }
}

function command(command, args, environment = "release") {
  return Object.freeze({ args: Object.freeze(args), command, environment });
}

export function createMacOSStoreBuildPlan(root, sourceEnvironment) {
  if (!path.isAbsolute(root)) throw new Error("MACOS_RELEASE_INPUT_INVALID");
  const releaseEnvironment = {
    ...sourceEnvironment,
    TAMMY_MACOS_TARGET: "mas/arm64",
  };
  const release = validateMacOSReleaseEnvironment(releaseEnvironment);
  const desktopPackage = JSON.parse(
    readFileSync(path.join(root, "apps", "desktop", "package.json"), "utf8"),
  );
  if (
    typeof desktopPackage.version !== "string" ||
    !/^\d+\.\d+\.\d+$/.test(desktopPackage.version)
  ) {
    throw new Error("MACOS_RELEASE_INPUT_INVALID");
  }
  const releaseRoot = path.join(
    root,
    ".tmp",
    "macos-release",
    desktopPackage.version,
    `build-${release.buildNumber}`,
  );
  const forgeOutputRoot = path.join(releaseRoot, ".forge-unsigned");
  const forgeApp = path.join(forgeOutputRoot, "Tammy-mas-arm64", "Tammy.app");
  const unsignedRoot = path.join(releaseRoot, "unsigned");
  const unsignedApp = path.join(unsignedRoot, "Tammy.app");
  const unsignedManifest = path.join(unsignedRoot, "unsigned-content.json");
  const modeRoot = path.join(releaseRoot, release.mode);
  const app = path.join(modeRoot, "Tammy.app");
  const signingRoot = path.join(releaseRoot, `.${release.mode}-staging`);
  const signingApp = path.join(signingRoot, "Tammy.app");
  const core = path.join(
    root,
    "apps",
    "desktop",
    "resources",
    "core",
    "darwin-arm64",
    "tammy-core",
  );
  const coreEntitlements = path.join(
    root,
    "apps",
    "desktop",
    "release",
    "macos",
    "entitlements.mas.core.plist",
  );
  const helper = path.join(
    root,
    "apps",
    "desktop",
    "resources",
    "sbr-helper",
    "darwin-arm64",
    "tammy-sbr-helper",
  );
  const helperEntitlements = path.join(
    root,
    "apps",
    "desktop",
    "release",
    "macos",
    "entitlements.mas.sbr-helper.plist",
  );
  const pkg =
    release.mode === "distribution"
      ? path.join(modeRoot, `Tammy-${desktopPackage.version}-build.${release.buildNumber}.pkg`)
      : undefined;
  const signingPkg =
    release.mode === "distribution"
      ? path.join(signingRoot, `Tammy-${desktopPackage.version}-build.${release.buildNumber}.pkg`)
      : undefined;
  const stageCommands = [
    command(process.execPath, [path.join(root, "scripts", "check-macos-store.mjs"), "--release"]),
    command(process.execPath, [path.join(root, "scripts", "build-sbr-helper.mjs"), "--mas-raw"]),
    command(process.execPath, [
      path.join(root, "scripts", "build-sbr-helper.mjs"),
      "--mas-profile-unsigned",
    ]),
    command("pnpm", ["core:build"]),
    command("pnpm", ["build:manifest"]),
    command(
      "pnpm",
      ["--dir", "apps/desktop", "package", "--platform=mas", "--arch=arm64"],
      "unsigned",
    ),
  ];
  const resourceSignCommands = [
    command("/usr/bin/codesign", [
      "--force",
      "--sign",
      sourceEnvironment.TAMMY_MACOS_SIGNING_IDENTITY,
      "--entitlements",
      helperEntitlements,
      "--identifier",
      "com.tammy.desktop.sbr-helper",
      release.mode === "distribution" ? "--timestamp" : "--timestamp=none",
      helper,
    ]),
    command("/usr/bin/codesign", [
      "--verify",
      "--strict",
      "-R",
      `=identifier "com.tammy.desktop.sbr-helper" and anchor apple generic and certificate leaf[subject.OU] = "${sourceEnvironment.TAMMY_MACOS_TEAM_ID}"`,
      helper,
    ]),
    command(process.execPath, [
      path.join(root, "scripts", "build-sbr-helper.mjs"),
      "--mas-profile",
    ]),
    command("/usr/bin/codesign", [
      "--force",
      "--sign",
      sourceEnvironment.TAMMY_MACOS_SIGNING_IDENTITY,
      "--entitlements",
      coreEntitlements,
      "--identifier",
      "com.tammy.desktop.core",
      "--timestamp=none",
      core,
    ]),
    command("/usr/bin/codesign", [
      "--verify",
      "--strict",
      "-R",
      `=identifier "com.tammy.desktop.core" and anchor apple generic and certificate leaf[subject.OU] = "${sourceEnvironment.TAMMY_MACOS_TEAM_ID}"`,
      core,
    ]),
    command("pnpm", ["build:manifest"]),
  ];
  const finalCommands = [
    command("/usr/bin/codesign", [
      "--verify",
      "--deep",
      "--strict",
      "-R",
      `=identifier "com.tammy.desktop" and anchor apple generic and certificate leaf[subject.OU] = "${sourceEnvironment.TAMMY_MACOS_TEAM_ID}"`,
      signingApp,
    ]),
  ];
  if (pkg !== undefined) {
    finalCommands.push(
      command("/usr/bin/productbuild", [
        "--component",
        signingApp,
        "/Applications",
        "--sign",
        sourceEnvironment.TAMMY_MACOS_INSTALLER_IDENTITY,
        signingPkg,
      ]),
    );
  }
  return Object.freeze({
    app,
    commands: Object.freeze([...stageCommands, ...resourceSignCommands, ...finalCommands]),
    environment: Object.freeze({
      ...releaseEnvironment,
      TAMMY_RELEASE_PROFILE: "mas",
      VITE_TAMMY_PRIVACY_POLICY_URL: sourceEnvironment.TAMMY_MACOS_PRIVACY_POLICY_URL,
      VITE_TAMMY_SUPPORT_URL: sourceEnvironment.TAMMY_MACOS_SUPPORT_URL,
    }),
    finalCommands: Object.freeze(finalCommands),
    forgeApp,
    forgeOutputRoot,
    mode: release.mode,
    modeRoot,
    releaseRoot,
    resourceSignCommands: Object.freeze(resourceSignCommands),
    root,
    signingApp,
    signingPkg,
    signingRoot,
    stageCommands: Object.freeze(stageCommands),
    unsignedApp,
    unsignedEnvironment: Object.freeze({
      HOME: sourceEnvironment.HOME,
      PATH: sourceEnvironment.PATH,
      TAMMY_MACOS_ARTIFACT_PHASE: "unsigned-staging",
      TAMMY_MACOS_BUILD_NUMBER: release.buildNumber,
      TAMMY_MACOS_EXPORT_COMPLIANCE: sourceEnvironment.TAMMY_MACOS_EXPORT_COMPLIANCE,
      TAMMY_MACOS_PRIVACY_POLICY_URL: sourceEnvironment.TAMMY_MACOS_PRIVACY_POLICY_URL,
      TAMMY_MACOS_SUPPORT_URL: sourceEnvironment.TAMMY_MACOS_SUPPORT_URL,
      TAMMY_MACOS_TARGET: "mas/arm64",
      TAMMY_MACOS_TEAM_ID: sourceEnvironment.TAMMY_MACOS_TEAM_ID,
      TAMMY_MACOS_UNSIGNED_OUTPUT_ROOT: forgeOutputRoot,
      TAMMY_RELEASE_PROFILE: "mas",
      VITE_TAMMY_PRIVACY_POLICY_URL: sourceEnvironment.TAMMY_MACOS_PRIVACY_POLICY_URL,
      VITE_TAMMY_SUPPORT_URL: sourceEnvironment.TAMMY_MACOS_SUPPORT_URL,
    }),
    unsignedManifest,
    unsignedRoot,
    version: desktopPackage.version,
    ...(pkg === undefined ? {} : { pkg }),
  });
}

async function exists(candidate) {
  try {
    await lstat(candidate);
    return true;
  } catch (error) {
    if (error?.code === "ENOENT") return false;
    throw error;
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
    throw new Error("MACOS_RELEASE_OUTPUT_INVALID");
  }
  return relative;
}

async function ensureReleaseRoot(plan) {
  const rootStatus = await lstat(plan.root).catch(() => null);
  const resolvedRoot = await realpath(plan.root).catch(() => null);
  if (!rootStatus?.isDirectory() || rootStatus.isSymbolicLink() || resolvedRoot !== plan.root) {
    throw new Error("MACOS_RELEASE_OUTPUT_INVALID");
  }
  const relative = containedRelativePath(plan.root, plan.releaseRoot);
  let current = plan.root;
  for (const segment of relative.split(path.sep)) {
    current = path.join(current, segment);
    await mkdir(current).catch((error) => {
      if (error?.code !== "EEXIST") throw error;
    });
    const status = await lstat(current).catch(() => null);
    if (!status?.isDirectory() || status.isSymbolicLink()) {
      throw new Error("MACOS_RELEASE_OUTPUT_INVALID");
    }
  }
}

async function assertSafeOutputDirectory(plan, candidate) {
  await ensureReleaseRoot(plan);
  const relative = containedRelativePath(plan.releaseRoot, candidate);
  let current = plan.releaseRoot;
  for (const segment of relative.split(path.sep)) {
    current = path.join(current, segment);
    const status = await lstat(current).catch(() => null);
    if (!status?.isDirectory() || status.isSymbolicLink()) {
      throw new Error("MACOS_RELEASE_OUTPUT_INVALID");
    }
  }
}

async function safeRemoveOutputDirectory(plan, candidate) {
  await ensureReleaseRoot(plan);
  containedRelativePath(plan.releaseRoot, candidate);
  const status = await lstat(candidate).catch((error) => {
    if (error?.code === "ENOENT") return null;
    throw error;
  });
  if (status === null) return;
  if (!status.isDirectory() || status.isSymbolicLink()) {
    throw new Error("MACOS_RELEASE_OUTPUT_INVALID");
  }
  await rm(candidate, { recursive: true });
}

async function safeRenameOutputDirectory(plan, source, destination) {
  await assertSafeOutputDirectory(plan, source);
  containedRelativePath(plan.releaseRoot, destination);
  if (await exists(destination)) throw new Error("MACOS_RELEASE_OUTPUT_INVALID");
  await rename(source, destination);
  await syncDirectory(plan.releaseRoot);
}

async function syncDirectory(directory) {
  const handle = await open(directory, "r");
  try {
    await handle.sync();
  } finally {
    await handle.close();
  }
}

export async function publishUnsignedMacOSStaging(plan) {
  await ensureReleaseRoot(plan);
  const source = await readProductSource(plan.root);
  if (source.marketingVersion !== plan.version) {
    throw new Error("MACOS_UNSIGNED_PRODUCT_SOURCE_INVALID");
  }
  if (
    !source.ledger.entries.some(
      (entry) =>
        entry.marketingVersion === source.marketingVersion &&
        entry.buildNumber === plan.environment.TAMMY_MACOS_BUILD_NUMBER,
    )
  ) {
    throw new Error("MACOS_UNSIGNED_PRODUCT_SOURCE_INVALID");
  }
  await assertSafeOutputDirectory(plan, plan.forgeApp).catch(() => {
    throw new Error("MACOS_UNSIGNED_FORGE_OUTPUT_INVALID");
  });
  const staging = await mkdtemp(path.join(plan.releaseRoot, ".unsigned-staging-"));
  const stagedApp = path.join(staging, "Tammy.app");
  try {
    await cp(plan.forgeApp, stagedApp, {
      dereference: false,
      preserveTimestamps: false,
      recursive: true,
      verbatimSymlinks: true,
    });
    const manifest = await createUnsignedContentManifest({
      buildNumber: plan.environment.TAMMY_MACOS_BUILD_NUMBER,
      bundleIdentifiers: {
        app: "com.tammy.desktop",
        helpers: [
          "com.tammy.desktop.helper",
          "com.tammy.desktop.helper.GPU",
          "com.tammy.desktop.helper.Plugin",
          "com.tammy.desktop.helper.Renderer",
        ],
      },
      marketingVersion: source.marketingVersion,
      productSourceCommit: source.productSourceCommit,
      productSourceTree: source.productSourceTree,
      publicLinks: {
        privacyPolicy: plan.environment.TAMMY_MACOS_PRIVACY_POLICY_URL,
        support: plan.environment.TAMMY_MACOS_SUPPORT_URL,
      },
      stagingRoot: stagedApp,
    });
    await writeUnsignedContentManifest(path.join(staging, "unsigned-content.json"), manifest);
    if (await exists(plan.unsignedRoot)) {
      const existing = validateUnsignedContentManifest(
        JSON.parse(await readFile(plan.unsignedManifest, "utf8")),
      );
      await authenticateUnsignedContentManifest(plan.unsignedApp, existing);
      if (!isDeepStrictEqual(existing, manifest)) {
        throw new Error("MACOS_UNSIGNED_STAGING_CONFLICT");
      }
      return existing;
    }
    await safeRenameOutputDirectory(plan, staging, plan.unsignedRoot);
    return manifest;
  } finally {
    await safeRemoveOutputDirectory(plan, staging).catch(() => {});
    await safeRemoveOutputDirectory(plan, plan.forgeOutputRoot).catch(() => {});
  }
}

export async function prepareSignedMacOSCopy(plan, manifest) {
  validateUnsignedContentManifest(manifest);
  await ensureReleaseRoot(plan);
  if (await exists(plan.modeRoot)) throw new Error("MACOS_SIGNED_COPY_EXISTS");
  await safeRemoveOutputDirectory(plan, plan.signingRoot);
  await cloneUnsignedStaging({
    destination: plan.signingApp,
    manifest,
    source: plan.unsignedApp,
  });
  const sourceResources = path.join(plan.root, "apps", "desktop", "resources");
  const destinationResources = path.join(plan.signingApp, "Contents", "Resources");
  for (const resource of ["build", "core", "sbr", "sbr-helper"]) {
    const destination = path.join(destinationResources, resource);
    await rm(destination, { force: true, recursive: true });
    await cp(path.join(sourceResources, resource), destination, {
      dereference: false,
      preserveTimestamps: false,
      recursive: true,
      verbatimSymlinks: true,
    });
  }
  return {
    app: plan.signingApp,
    async cleanup() {
      await safeRemoveOutputDirectory(plan, plan.signingRoot);
    },
    async publish() {
      if (await exists(plan.modeRoot)) throw new Error("MACOS_SIGNED_COPY_EXISTS");
      await safeRenameOutputDirectory(plan, plan.signingRoot, plan.modeRoot);
    },
  };
}

export async function signMacOSCopy(plan, app, suppliedEntitlements) {
  const ownedEntitlements =
    suppliedEntitlements === undefined ? await prepareMacOSEntitlements(plan) : null;
  const entitlements = suppliedEntitlements ?? ownedEntitlements;
  const coreSuffix = path.join("Contents", "Resources", "core", "darwin-arm64", "tammy-core");
  const helperSuffix = path.join(
    "Contents",
    "Resources",
    "sbr-helper",
    "darwin-arm64",
    "tammy-sbr-helper",
  );
  try {
    await signAsync({
      app,
      identity: plan.environment.TAMMY_MACOS_SIGNING_IDENTITY,
      identityValidation: true,
      ignore: (file) => [coreSuffix, helperSuffix].some((suffix) => file.endsWith(suffix)),
      optionsForFile: (file) => ({
        entitlements: file.endsWith(`${path.sep}Tammy.app`)
          ? entitlements.files["entitlements.mas.plist"]
          : entitlements.files["entitlements.mas.child.plist"],
      }),
      platform: "mas",
      preAutoEntitlements: false,
      provisioningProfile: plan.environment.TAMMY_MACOS_PROVISIONING_PROFILE,
      type: plan.mode,
    });
  } finally {
    await ownedEntitlements?.cleanup();
  }
}

export async function verifyUnsignedMacOSInputs(plan, manifest) {
  await authenticateUnsignedContentManifest(plan.unsignedApp, manifest);
  for (const [relativePath, source] of [
    [
      "Contents/Resources/core/darwin-arm64/tammy-core",
      path.join(plan.root, "apps", "desktop", "resources", "core", "darwin-arm64", "tammy-core"),
    ],
    [
      "Contents/Resources/sbr-helper/darwin-arm64/tammy-sbr-helper",
      path.join(
        plan.root,
        "apps",
        "desktop",
        "resources",
        "sbr-helper",
        "darwin-arm64",
        "tammy-sbr-helper",
      ),
    ],
  ]) {
    const expected = manifest.entries.find((entry) => entry.path === relativePath);
    if (expected?.kind !== "file" || (await hashPackage(source)) !== expected.sha256) {
      throw new Error("MACOS_UNSIGNED_INPUT_MISMATCH");
    }
  }
}

export async function normalizeCodeSignedExecutable(file, { requireSignature = true } = {}) {
  if (typeof file !== "string" || !path.isAbsolute(file)) {
    throw new Error("MACOS_SIGNED_COPY_MISMATCH");
  }
  const temporary = await mkdtemp(path.join(tmpdir(), "tammy-codesign-normalize-"));
  const copy = path.join(temporary, path.basename(file));
  try {
    await cp(file, copy, { preserveTimestamps: false });
    try {
      await execFile("/usr/bin/codesign", ["--remove-signature", copy], {
        encoding: "utf8",
        maxBuffer: 1024 * 1024,
      });
    } catch {
      if (requireSignature) throw new Error("MACOS_SIGNED_COPY_MISMATCH");
    }
    return await hashPackage(copy);
  } finally {
    await rm(temporary, { force: true, recursive: true }).catch(() => {});
  }
}

export async function validateSignedModeBoundResources({ app, plan, unsignedManifest }) {
  validateUnsignedContentManifest(unsignedManifest);
  const resources = path.join(app, "Contents", "Resources");
  const paths = {
    build: path.join(resources, "build", "build-manifest.json"),
    core: path.join(resources, "core", "darwin-arm64", "tammy-core"),
    helper: path.join(resources, "sbr-helper", "darwin-arm64", "tammy-sbr-helper"),
    profile: path.join(resources, "sbr", "simulator", "sbr-profile-v1.json"),
    signature: path.join(resources, "sbr", "simulator", "sbr-profile-v1.sig"),
    sqlcipher: path.join(resources, "sqlcipher", "darwin-arm64", "lib", "libsqlite3.a"),
  };
  try {
    const [buildBytes, profileBytes, publicKey, signatureBytes] = await Promise.all([
      readFile(paths.build),
      readFile(paths.profile),
      readFile(path.join(plan.root, "config", "sbr", "simulator", "profile-public-key.pem")),
      readFile(paths.signature),
    ]);
    const build = parseCanonicalBuildManifest(buildBytes, "darwin-arm64");
    const authenticated = authenticateSbrProfileBytes({
      now: new Date(),
      profileBytes,
      publicKey,
      signatureBytes,
    });
    const [coreSha256, helperSha256, profileSha256, signatureSha256, sqlcipherSha256] =
      await Promise.all([
        hashPackage(paths.core),
        hashPackage(paths.helper),
        hashPackage(paths.profile),
        hashPackage(paths.signature),
        hashPackage(paths.sqlcipher),
      ]);
    if (
      build.source_revision !== unsignedManifest.productSourceCommit ||
      build.core_sha256 !== coreSha256 ||
      build.sbr.helper_sha256 !== helperSha256 ||
      build.sbr.profile_sha256 !== profileSha256 ||
      build.sbr.profile_signature_sha256 !== signatureSha256 ||
      build.sqlcipher.library_sha256 !== sqlcipherSha256 ||
      authenticated.helper_sha256 !== helperSha256
    ) {
      throw new Error("MACOS_SIGNED_COPY_MISMATCH");
    }
    return { authenticated, build };
  } catch {
    throw new Error("MACOS_SIGNED_COPY_MISMATCH");
  }
}

export async function verifySignedCopyEquivalence(plan, manifest, candidateApp = plan.signingApp) {
  const otherMode = plan.mode === "development" ? "distribution" : "development";
  const otherApp = path.join(plan.releaseRoot, otherMode, "Tammy.app");
  const counterpartExists = await exists(otherApp);
  if (!counterpartExists && plan.mode === "distribution") {
    throw new Error("MACOS_SIGNED_COPY_COUNTERPART_MISSING");
  }
  const signedPaths = manifest.entries
    .filter(
      (entry) =>
        entry.kind === "file" &&
        entry.executable &&
        [
          "Contents/MacOS/",
          "Contents/Frameworks/",
          "Contents/Resources/core/",
          "Contents/Resources/sbr-helper/",
        ].some((prefix) => entry.path.startsWith(prefix)),
    )
    .map((entry) => entry.path);
  const unsignedExecutableHashes = new Map();
  const signedInspection = new Map();
  const modeBoundValidation = new Map();

  const expectedIdentifier = (relativePath) => {
    if (relativePath === "Contents/MacOS/Tammy") return "com.tammy.desktop";
    if (relativePath.startsWith("Contents/Resources/core/")) return "com.tammy.desktop.core";
    if (relativePath.startsWith("Contents/Resources/sbr-helper/")) {
      return "com.tammy.desktop.sbr-helper";
    }
    for (const [name, identifier] of [
      ["Tammy Helper.app", "com.tammy.desktop.helper"],
      ["Tammy Helper (GPU).app", "com.tammy.desktop.helper.GPU"],
      ["Tammy Helper (Plugin).app", "com.tammy.desktop.helper.Plugin"],
      ["Tammy Helper (Renderer).app", "com.tammy.desktop.helper.Renderer"],
    ]) {
      if (relativePath.startsWith(`Contents/Frameworks/${name}/`)) return identifier;
    }
    return undefined;
  };
  const expectedEntitlements = async (relativePath) => {
    const file = relativePath.startsWith("Contents/Resources/core/")
      ? "entitlements.mas.core.plist"
      : relativePath.startsWith("Contents/Resources/sbr-helper/")
        ? "entitlements.mas.sbr-helper.plist"
        : relativePath === "Contents/MacOS/Tammy"
          ? "entitlements.mas.plist"
          : "entitlements.mas.child.plist";
    return expectedEntitlementObject(plan, file);
  };
  const inspectSignedFile = async (app, relativePath, mode) => {
    const cacheKey = `${app}\0${relativePath}\0${mode}`;
    if (signedInspection.has(cacheKey)) return signedInspection.get(cacheKey);
    const file = path.join(app, ...relativePath.split("/"));
    const [details, entitlements] = await Promise.all([
      execFile("/usr/bin/codesign", ["-d", "--verbose=4", file], {
        encoding: "utf8",
        maxBuffer: 1024 * 1024,
      }),
      execFile("/usr/bin/codesign", ["-d", "--entitlements", ":-", file], {
        encoding: "utf8",
        maxBuffer: 1024 * 1024,
      }),
    ]);
    const identifier = details.stderr.match(/(?:^|\n)Identifier=([^\n]+)(?:\n|$)/)?.[1];
    if (identifier === undefined) throw new Error("MACOS_SIGNED_COPY_MISMATCH");
    const requiredIdentifier = expectedIdentifier(relativePath);
    if (requiredIdentifier !== undefined && identifier !== requiredIdentifier) {
      throw new Error("MACOS_SIGNED_COPY_MISMATCH");
    }
    let actualEntitlements;
    try {
      actualEntitlements = plist.parse(entitlements.stdout);
    } catch {
      throw new Error("MACOS_SIGNED_COPY_MISMATCH");
    }
    if (!isDeepStrictEqual(actualEntitlements, await expectedEntitlements(relativePath))) {
      throw new Error("MACOS_SIGNED_COPY_MISMATCH");
    }
    let unsignedNormalized = unsignedExecutableHashes.get(relativePath);
    if (unsignedNormalized === undefined) {
      unsignedNormalized = await normalizeCodeSignedExecutable(
        path.join(plan.unsignedApp, ...relativePath.split("/")),
        { requireSignature: false },
      );
      unsignedExecutableHashes.set(relativePath, unsignedNormalized);
    }
    if ((await normalizeCodeSignedExecutable(file)) !== unsignedNormalized) {
      throw new Error("MACOS_SIGNED_COPY_MISMATCH");
    }
    const result = {
      buildNumber: manifest.buildNumber,
      bundleIdentifier: identifier,
      entitlementIntent: [
        `sha256:${createHash("sha256").update(JSON.stringify(actualEntitlements)).digest("hex")}`,
      ],
      marketingVersion: manifest.marketingVersion,
      publicLinks: manifest.publicLinks,
      signingMode: mode,
      unsignedSha256: manifest.entries.find((entry) => entry.path === relativePath).sha256,
    };
    signedInspection.set(cacheKey, result);
    return result;
  };
  const normalizedPaths = [
    "Contents/Resources/build/build-manifest.json",
    "Contents/Resources/sbr/simulator/sbr-profile-v1.json",
    "Contents/Resources/sbr/simulator/sbr-profile-v1.sig",
  ];
  const validateModeBoundResources = async (app) => {
    if (modeBoundValidation.has(app)) return modeBoundValidation.get(app);
    const validation = validateSignedModeBoundResources({ app, plan, unsignedManifest: manifest });
    modeBoundValidation.set(app, validation);
    return validation;
  };
  const normalizeFile = async (app, relativePath) => {
    const { authenticated, build } = await validateModeBoundResources(app);
    if (relativePath === normalizedPaths[0]) {
      return {
        ...build,
        core_sha256: "MODE_BOUND",
        sbr: {
          ...build.sbr,
          helper_sha256: "MODE_BOUND",
          profile_sha256: "MODE_BOUND",
          profile_signature_sha256: "MODE_BOUND",
        },
      };
    }
    return { ...authenticated, helper_sha256: "MODE_BOUND" };
  };
  if (!counterpartExists) {
    await validateSignedCopyAgainstUnsignedManifest({
      app: candidateApp,
      inspectSignedFile,
      mode: plan.mode,
      normalizeFile,
      normalizedPaths,
      signedPaths,
      unsignedManifest: manifest,
    });
    return;
  }
  const developmentApp = plan.mode === "development" ? candidateApp : otherApp;
  const distributionApp = plan.mode === "distribution" ? candidateApp : otherApp;
  await compareSignedCopies({
    developmentApp,
    distributionApp,
    inspectSignedFile,
    normalizeFile,
    normalizedPaths,
    signedPaths,
    unsignedManifest: manifest,
  });
}

async function run(commandSpec, options) {
  return new Promise((resolve, reject) => {
    const captureOutput = options.captureOutput === true;
    const output = { stderr: "", stdout: "" };
    let capturedOutputBytes = 0;
    let outputOverflowed = false;
    const child = spawn(commandSpec.command, commandSpec.args, {
      cwd: options.cwd,
      env: options.environment,
      shell: false,
      stdio: captureOutput ? ["ignore", "pipe", "pipe"] : "inherit",
    });
    if (captureOutput) {
      const capture = (stream) => (chunk) => {
        if (outputOverflowed) return;
        const chunkBytes = Buffer.byteLength(chunk);
        if (capturedOutputBytes + chunkBytes > MAX_CAPTURED_OUTPUT_BYTES) {
          outputOverflowed = true;
          child.kill();
          return;
        }
        capturedOutputBytes += chunkBytes;
        output[stream] += chunk;
      };
      child.stdout.on("data", capture("stdout"));
      child.stderr.on("data", capture("stderr"));
    }
    child.once("error", () => reject(new Error("MACOS_STORE_COMMAND_FAILED")));
    child.once("close", (code, signal) => {
      if (outputOverflowed) {
        reject(new Error("MACOS_STORE_COMMAND_OUTPUT_INVALID"));
      } else if (signal !== null || !Number.isInteger(code)) {
        reject(new Error("MACOS_STORE_COMMAND_FAILED"));
      } else if (code === 0 || options.allowNonZero === true) {
        resolve({ ...output, exitCode: code, signal });
      } else reject(new Error(`MACOS_STORE_COMMAND_FAILED:${code ?? signal ?? "unknown"}`));
    });
  });
}

async function hashPackage(pkg) {
  const hash = createHash("sha256");
  let file;
  try {
    file = await open(pkg, "r");
    const stream = file.createReadStream();
    for await (const chunk of stream) hash.update(chunk);
    return hash.digest("hex");
  } catch {
    throw new Error("MACOS_STORE_PACKAGE_HASH_INVALID");
  } finally {
    await file?.close().catch(() => {});
  }
}

function classifyGatekeeperAssessment(result) {
  if (!Number.isInteger(result?.exitCode) || result.signal !== null) {
    throw new Error("MACOS_STORE_GATEKEEPER_ASSESSMENT_INVALID");
  }
  const output = [result?.stdout, result?.stderr]
    .filter((value) => typeof value === "string")
    .join("\n");
  if (output.trim().length === 0) throw new Error("MACOS_STORE_GATEKEEPER_OUTPUT_MISSING");
  const accepted = /(?:^|\W)accepted(?:\W|$)/iu.test(output);
  const rejected = /(?:^|\W)rejected(?:\W|$)/iu.test(output);
  if (accepted === rejected) throw new Error("MACOS_STORE_GATEKEEPER_OUTPUT_UNCLASSIFIABLE");
  if (result.exitCode === 0 && accepted) return "accepted";
  if (result.exitCode === 3 && rejected) return "rejected";
  throw new Error("MACOS_STORE_GATEKEEPER_ASSESSMENT_INVALID");
}

async function runCommand(commandRunner, commandSpec, options, failure) {
  try {
    return await commandRunner(commandSpec, options);
  } catch (error) {
    if (error instanceof Error && error.message === "MACOS_STORE_COMMAND_OUTPUT_INVALID") {
      throw error;
    }
    throw new Error(failure);
  }
}

export async function executeMacOSStoreBuild(
  plan,
  {
    appSigner = signMacOSCopy,
    commandRunner = run,
    packageHasher = hashPackage,
    entitlementPreparer = prepareMacOSEntitlements,
    signedCopyPreparer = prepareSignedMacOSCopy,
    unsignedStager = publishUnsignedMacOSStaging,
    unsignedInputVerifier = verifyUnsignedMacOSInputs,
    equivalenceVerifier = verifySignedCopyEquivalence,
    write = (line) => process.stdout.write(line),
  } = {},
) {
  await ensureReleaseRoot(plan);
  const lock = await acquireSbrBuildLock(plan.root);
  try {
    await safeRemoveOutputDirectory(plan, plan.forgeOutputRoot);
    const commandOptions = {
      cwd: plan.root,
      environment: { ...plan.environment, [SBR_BUILD_LOCK_ENV]: lock.token },
    };
    for (const step of plan.stageCommands) {
      await runCommand(
        commandRunner,
        step,
        {
          ...commandOptions,
          environment: {
            ...(step.environment === "unsigned" ? plan.unsignedEnvironment : plan.environment),
            [SBR_BUILD_LOCK_ENV]: lock.token,
          },
        },
        "MACOS_STORE_COMMAND_FAILED",
      );
    }
    const manifest = await unsignedStager(plan);
    await unsignedInputVerifier(plan, manifest);
    const entitlements = await entitlementPreparer(plan);
    try {
      for (const step of plan.resourceSignCommands) {
        const preparedStep = {
          ...step,
          args: step.args.map((argument) => {
            const filename = path.basename(argument);
            return Object.hasOwn(entitlements.files, filename)
              ? entitlements.files[filename]
              : argument;
          }),
        };
        await runCommand(commandRunner, preparedStep, commandOptions, "MACOS_STORE_COMMAND_FAILED");
      }
      const signedCopy = await signedCopyPreparer(plan, manifest);
      let published = false;
      let pkgSha256;
      let gatekeeperAssessment;
      try {
        await appSigner(plan, signedCopy.app, entitlements);
        for (const step of plan.finalCommands) {
          await runCommand(commandRunner, step, commandOptions, "MACOS_STORE_COMMAND_FAILED");
        }
        if (plan.pkg !== undefined) {
          await runCommand(
            commandRunner,
            command("/usr/sbin/pkgutil", ["--check-signature", plan.signingPkg]),
            { ...commandOptions, captureOutput: true },
            "MACOS_STORE_PACKAGE_SIGNATURE_INVALID",
          );
          try {
            pkgSha256 = await packageHasher(plan.signingPkg);
          } catch {
            throw new Error("MACOS_STORE_PACKAGE_HASH_INVALID");
          }
          if (typeof pkgSha256 !== "string" || !/^[a-f0-9]{64}$/u.test(pkgSha256)) {
            throw new Error("MACOS_STORE_PACKAGE_HASH_INVALID");
          }
          const gatekeeperOutput = await runCommand(
            commandRunner,
            command("/usr/sbin/spctl", [
              "--assess",
              "--type",
              "install",
              "--verbose=4",
              plan.signingPkg,
            ]),
            { ...commandOptions, allowNonZero: true, captureOutput: true },
            "MACOS_STORE_GATEKEEPER_ASSESSMENT_INVALID",
          );
          gatekeeperAssessment = classifyGatekeeperAssessment(gatekeeperOutput);
        }
        await equivalenceVerifier(plan, manifest, signedCopy.app);
        await signedCopy.publish();
        published = true;
      } finally {
        if (!published) await signedCopy.cleanup();
      }
      if (plan.pkg === undefined) {
        const result = { app: plan.app };
        write(`${JSON.stringify(result)}\n`);
        return result;
      }
      const result = {
        app: plan.app,
        pkg: plan.pkg,
        pkgSha256,
        gatekeeperAssessment,
      };
      write(`${JSON.stringify(result)}\n`);
      return result;
    } finally {
      await entitlements.cleanup();
    }
  } finally {
    await lock.release();
  }
}

async function main() {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const releaseEnvironment = { ...process.env, TAMMY_MACOS_TARGET: "mas/arm64" };
  const plan = createMacOSStoreBuildPlan(root, releaseEnvironment);
  await executeMacOSStoreBuild(plan);
}

if (process.argv[1] && pathToFileURL(process.argv[1]).href === import.meta.url) {
  main().catch((error) => {
    process.stderr.write(
      `${error instanceof Error ? error.message : "MACOS_STORE_BUILD_FAILED"}\n`,
    );
    process.exitCode = 1;
  });
}
