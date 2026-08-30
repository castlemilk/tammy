import { execFile as nodeExecFile } from "node:child_process";
import { createHash, createPrivateKey, randomBytes, sign } from "node:crypto";
import {
  chmod,
  copyFile,
  lstat,
  mkdir,
  mkdtemp,
  readFile,
  rename,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { enterSbrBuildOwnership, SBR_BUILD_LOCK_ENV } from "./sbr-build-lock.mjs";
import { authenticateSbrProfileBytes, canonicalizeSbrProfile } from "./sbr-profile-schema.mjs";

const execFile = promisify(nodeExecFile);
const HELPER_IDENTIFIER = "com.tammy.desktop.sbr-helper";
const PROFILE_ISSUED_AT = "2026-08-01T00:00:00Z";
const PROFILE_EXPIRES_AT = "2030-01-01T00:00:00Z";

export function selectSbrHelperTarget(platform, arch) {
  if (platform === "darwin" && arch === "arm64") {
    return { status: "SIMULATOR_ENABLED", target: "darwin-arm64" };
  }
  if (platform === "win32" && arch === "x64") {
    return { status: "SBR_UNAVAILABLE_ON_TARGET", target: "win32-x64" };
  }
  throw new Error(`UNSUPPORTED_SBR_TARGET:${platform}/${arch}`);
}

export function createSbrHelperBuildPlan(
  root,
  temporary = path.join(root, ".tmp/sbr-helper-build/helper"),
) {
  if (!path.isAbsolute(root) || path.normalize(root) !== root)
    throw new Error("SBR_HELPER_BUILD_INVALID");
  return {
    args: [
      "build",
      "-trimpath",
      "-buildvcs=false",
      "-ldflags=-buildid=tammy-sbr-helper-v1",
      "-o",
      temporary,
      "./cmd/tammy-sbr-helper",
    ],
    cwd: path.join(root, "services/sbr-helper"),
    destination: path.join(root, "apps/desktop/resources/sbr-helper/darwin-arm64/tammy-sbr-helper"),
    environment: { CGO_ENABLED: "1", GOARCH: "arm64", GOOS: "darwin", SOURCE_DATE_EPOCH: "0" },
    temporary,
  };
}

function sha256(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

export async function hashSbrHelperSourceTree(root) {
  const { readdir } = await import("node:fs/promises");
  const files = [];
  async function visit(directory, prefix) {
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      const relative = prefix ? `${prefix}/${entry.name}` : entry.name;
      const absolute = path.join(directory, entry.name);
      const stats = await lstat(absolute);
      if (stats.isSymbolicLink()) throw new Error("SBR_HELPER_SOURCE_INVALID");
      if (stats.isDirectory()) await visit(absolute, relative);
      else if (stats.isFile()) files.push([relative, await readFile(absolute)]);
      else throw new Error("SBR_HELPER_SOURCE_INVALID");
    }
  }
  await visit(root, "");
  files.sort(([a], [b]) => Buffer.compare(Buffer.from(a), Buffer.from(b)));
  const digest = createHash("sha256").update("tammy-sbr-helper-source-v1\0");
  for (const [name, bytes] of files)
    digest.update(name).update("\0").update(sha256(bytes)).update("\0");
  const workspace = path.resolve(root, "../..", "go.work");
  const workspaceStats = await lstat(workspace).catch(() => null);
  if (workspaceStats) {
    if (!workspaceStats.isFile() || workspaceStats.isSymbolicLink())
      throw new Error("SBR_HELPER_SOURCE_INVALID");
    digest
      .update("../../go.work\0")
      .update(sha256(await readFile(workspace)))
      .update("\0");
  }
  return digest.digest("hex");
}

export async function generateSimulatorProfile({ helper, profile, signature, signer }) {
  if (typeof signer !== "function") throw new Error("SBR_SIMULATOR_SIGNER_REQUIRED");
  const helperBytes = await readFile(helper);
  const value = {
    component_manifest_sha256: "NONE",
    endpoint_profile_sha256: "NONE",
    environment: "SIMULATOR",
    expires_at: PROFILE_EXPIRES_AT,
    helper_sha256: sha256(helperBytes),
    issued_at: PROFILE_ISSUED_AT,
    registration_manifest_sha256: "NONE",
    schema_version: 1,
    target: "darwin/arm64",
  };
  const canonical = canonicalizeSbrProfile(value, { now: new Date(PROFILE_ISSUED_AT) });
  const signed = Buffer.from(await signer(canonical));
  if (signed.length !== 64) throw new Error("SBR_SIMULATOR_SIGNATURE_INVALID");
  await mkdir(path.dirname(profile), { recursive: true });
  await writeFile(profile, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o644 });
  await writeFile(signature, `${signed.toString("base64")}\n`, { mode: 0o644 });
  return {
    helperSha256: value.helper_sha256,
    profileSha256: sha256(await readFile(profile)),
    profileSignatureSha256: sha256(await readFile(signature)),
  };
}

async function run(command, args, options) {
  return execFile(command, args, {
    ...options,
    maxBuffer: 4 * 1024 * 1024,
    timeout: 120_000,
  });
}

async function currentRevision(root, commandRunner) {
  const result = await commandRunner("git", ["rev-parse", "HEAD"], {
    cwd: root,
    env: { LANG: "C", LC_ALL: "C", PATH: process.env.PATH },
  });
  const revision = `${result?.stdout ?? ""}`.trim();
  if (!/^[0-9a-f]{40}$/.test(revision)) throw new Error("SBR_HELPER_SESSION_INVALID");
  return revision;
}

async function purgeGeneratedSbr(root) {
  if (!path.isAbsolute(root) || path.normalize(root) !== root)
    throw new Error("SBR_HELPER_BUILD_INVALID");
  await Promise.all([
    rm(path.join(root, ".tmp/sbr-helper-build"), { force: true, recursive: true }),
    rm(path.join(root, "apps/desktop/resources/sbr-helper"), {
      force: true,
      recursive: true,
    }),
    rm(path.join(root, "apps/desktop/resources/sbr"), { force: true, recursive: true }),
  ]);
}

async function buildRawTwice(root, commandRunner) {
  const staging = await mkdtemp(path.join(tmpdir(), "tammy-sbr-helper-build-"));
  const goCache = path.join(staging, "go-cache");
  const goTemporary = path.join(staging, "go-tmp");
  await Promise.all([mkdir(goCache), mkdir(goTemporary)]);
  const outputs = [path.join(staging, "one"), path.join(staging, "two")];
  for (const output of outputs) {
    const plan = createSbrHelperBuildPlan(root, output);
    await commandRunner("mise", ["exec", "--", "go", ...plan.args], {
      cwd: plan.cwd,
      env: {
        HOME: process.env.HOME,
        PATH: process.env.PATH,
        GOCACHE: goCache,
        GOTMPDIR: goTemporary,
        LANG: "C",
        LC_ALL: "C",
        ...plan.environment,
      },
    });
  }
  const raw = await Promise.all(outputs.map((output) => readFile(output)));
  if (!raw[0].equals(raw[1])) throw new Error("SBR_HELPER_REPRODUCIBILITY_FAILED");
  return { goCache, goTemporary, outputs, raw, staging };
}

export async function buildMasRawHelper({ root, commandRunner = run }) {
  if (process.platform !== "darwin" || process.arch !== "arm64")
    throw new Error(`UNSUPPORTED_SBR_TARGET:${process.platform}/${process.arch}`);
  await purgeGeneratedSbr(root);
  const built = await buildRawTwice(root, commandRunner);
  try {
    const plan = createSbrHelperBuildPlan(root);
    await mkdir(path.dirname(plan.destination), { recursive: true });
    await copyFile(built.outputs[0], plan.destination);
    await chmod(plan.destination, 0o500);
    const session = {
      helper_raw_sha256: sha256(built.raw[0]),
      mode: "MAS_RAW",
      session_nonce: randomBytes(16).toString("hex"),
      source_revision: await currentRevision(root, commandRunner),
      source_tree_sha256: await hashSbrHelperSourceTree(path.join(root, "services/sbr-helper")),
      target: "darwin-arm64",
    };
    const sessionRoot = path.join(root, ".tmp/sbr-helper-build");
    await mkdir(sessionRoot, { recursive: true });
    await writeFile(
      path.join(sessionRoot, "session.json"),
      `${JSON.stringify(session, null, 2)}\n`,
      { mode: 0o600 },
    );
    return session;
  } finally {
    await rm(built.staging, { force: true, recursive: true });
  }
}

function assertMasSession(session) {
  const expected = [
    "helper_raw_sha256",
    "mode",
    "session_nonce",
    "source_revision",
    "source_tree_sha256",
    "target",
  ];
  if (
    !session ||
    typeof session !== "object" ||
    Array.isArray(session) ||
    Object.keys(session).sort().join("\n") !== expected.sort().join("\n") ||
    session.mode !== "MAS_RAW" ||
    session.target !== "darwin-arm64" ||
    !/^[0-9a-f]{64}$/.test(session.helper_raw_sha256) ||
    !/^[0-9a-f]{64}$/.test(session.source_tree_sha256) ||
    !/^[0-9a-f]{32}$/.test(session.session_nonce) ||
    !/^[0-9a-f]{40}$/.test(session.source_revision)
  )
    throw new Error("SBR_HELPER_SESSION_INVALID");
}

export async function buildMasSimulatorProfile({
  root,
  commandRunner = run,
  allowUnsigned = false,
}) {
  if (process.platform !== "darwin" || process.arch !== "arm64")
    throw new Error(`UNSUPPORTED_SBR_TARGET:${process.platform}/${process.arch}`);
  const sessionRoot = path.join(root, ".tmp/sbr-helper-build");
  let session;
  try {
    session = JSON.parse(await readFile(path.join(sessionRoot, "session.json"), "utf8"));
  } catch {
    throw new Error("SBR_HELPER_SESSION_INVALID");
  }
  assertMasSession(session);
  if (
    session.source_revision !== (await currentRevision(root, commandRunner)) ||
    session.source_tree_sha256 !==
      (await hashSbrHelperSourceTree(path.join(root, "services/sbr-helper")))
  )
    throw new Error("SBR_HELPER_SESSION_STALE");
  const plan = createSbrHelperBuildPlan(root);
  const helperBytes = await readFile(plan.destination);
  const helperSha256 = sha256(helperBytes);
  if (
    (allowUnsigned === true && helperSha256 !== session.helper_raw_sha256) ||
    (allowUnsigned !== true && helperSha256 === session.helper_raw_sha256)
  ) {
    throw new Error(
      allowUnsigned === true
        ? "SBR_HELPER_UNSIGNED_STAGING_INVALID"
        : "SBR_HELPER_MAS_SIGNATURE_MISSING",
    );
  }
  const profileRoot = path.join(root, "apps/desktop/resources/sbr/simulator");
  const profilePath = path.join(profileRoot, "sbr-profile-v1.json");
  const signaturePath = path.join(profileRoot, "sbr-profile-v1.sig");
  await rm(path.join(root, "apps/desktop/resources/sbr"), { force: true, recursive: true });
  const privateKey = createPrivateKey(
    await readFile(path.join(root, "test/fixtures/sbr/simulator-profile-private-key.pem")),
  );
  const generated = await generateSimulatorProfile({
    helper: plan.destination,
    profile: profilePath,
    signature: signaturePath,
    signer: async (canonical) => sign(null, canonical, privateKey),
  });
  const [profileBytes, signatureBytes, publicKey] = await Promise.all([
    readFile(profilePath),
    readFile(signaturePath),
    readFile(path.join(root, "config/sbr/simulator/profile-public-key.pem")),
  ]);
  const authenticated = authenticateSbrProfileBytes({
    now: new Date(),
    profileBytes,
    publicKey,
    signatureBytes,
  });
  if (authenticated.helper_sha256 !== helperSha256)
    throw new Error("SBR_SIMULATOR_HELPER_HASH_MISMATCH");
  const goRoot = await mkdtemp(path.join(tmpdir(), "tammy-sbr-profile-go-"));
  try {
    const goCache = path.join(goRoot, "cache");
    const goTemporary = path.join(goRoot, "tmp");
    await Promise.all([mkdir(goCache), mkdir(goTemporary)]);
    await commandRunner(
      "mise",
      [
        "exec",
        "--",
        "go",
        "test",
        "./services/core/internal/sbrprofile",
        "-run",
        "TestCommittedSimulatorProfileAuthenticatesAndBindsRequestedHelper",
        "-count=1",
      ],
      {
        cwd: root,
        env: {
          HOME: process.env.HOME,
          PATH: process.env.PATH,
          GOCACHE: goCache,
          GOTMPDIR: goTemporary,
          LANG: "C",
          LC_ALL: "C",
          TAMMY_SBR_HELPER_PATH: plan.destination,
          TAMMY_SBR_PROFILE_PATH: profilePath,
          TAMMY_SBR_PROFILE_SIGNATURE_PATH: signaturePath,
        },
      },
    );
  } finally {
    await rm(goRoot, { force: true, recursive: true });
  }
  const provenance = {
    helper_raw_sha256: session.helper_raw_sha256,
    helper_sha256: helperSha256,
    profile_sha256: generated.profileSha256,
    profile_signature_sha256: generated.profileSignatureSha256,
    session_nonce: session.session_nonce,
    source_revision: session.source_revision,
    source_tree_sha256: session.source_tree_sha256,
    status: "SIMULATOR_ENABLED",
    target: "darwin-arm64",
  };
  await writeFile(
    path.join(sessionRoot, "provenance.json"),
    `${JSON.stringify(provenance, null, 2)}\n`,
    { mode: 0o600 },
  );
  return provenance;
}

export async function buildSbrHelper({
  root,
  platform = process.platform,
  arch = process.arch,
  commandRunner = run,
}) {
  const selected = selectSbrHelperTarget(platform, arch);
  if (selected.status === "SBR_UNAVAILABLE_ON_TARGET") {
    await purgeGeneratedSbr(root);
    return {
      ...selected,
      helper_raw_sha256: null,
      helper_sha256: null,
      profile_sha256: null,
      profile_signature_sha256: null,
      source_tree_sha256: null,
    };
  }
  await purgeGeneratedSbr(root);
  const staging = await mkdtemp(path.join(tmpdir(), "tammy-sbr-helper-build-"));
  try {
    const goCache = path.join(staging, "go-cache");
    const goTemporary = path.join(staging, "go-tmp");
    await Promise.all([mkdir(goCache), mkdir(goTemporary)]);
    const outputs = [path.join(staging, "one"), path.join(staging, "two")];
    for (const output of outputs) {
      const plan = createSbrHelperBuildPlan(root, output);
      await commandRunner("mise", ["exec", "--", "go", ...plan.args], {
        cwd: plan.cwd,
        env: {
          HOME: process.env.HOME,
          PATH: process.env.PATH,
          GOCACHE: goCache,
          GOTMPDIR: goTemporary,
          LANG: "C",
          LC_ALL: "C",
          ...plan.environment,
        },
      });
    }
    const raw = await Promise.all(outputs.map((output) => readFile(output)));
    if (!raw[0].equals(raw[1])) {
      throw new Error(`SBR_HELPER_REPRODUCIBILITY_FAILED:${sha256(raw[0])}:${sha256(raw[1])}`);
    }
    for (const output of outputs) {
      await commandRunner(
        "/usr/bin/codesign",
        ["--force", "--sign", "-", "--identifier", HELPER_IDENTIFIER, "--timestamp=none", output],
        { env: { PATH: "/usr/bin:/bin" } },
      );
    }
    const signed = await Promise.all(outputs.map((output) => readFile(output)));
    if (!signed[0].equals(signed[1])) throw new Error("SBR_HELPER_SIGNING_REPRODUCIBILITY_FAILED");
    const plan = createSbrHelperBuildPlan(root);
    await mkdir(path.dirname(plan.destination), { recursive: true });
    const temporaryDestination = `${plan.destination}.tmp`;
    await copyFile(outputs[0], temporaryDestination);
    await chmod(temporaryDestination, 0o500);
    await rename(temporaryDestination, plan.destination);
    const profilePath = path.join(root, "config/sbr/simulator/sbr-profile-v1.json");
    const signaturePath = path.join(root, "config/sbr/simulator/sbr-profile-v1.sig");
    const publicKey = await readFile(
      path.join(root, "config/sbr/simulator/profile-public-key.pem"),
    );
    const [profileBytes, signatureBytes] = await Promise.all([
      readFile(profilePath),
      readFile(signaturePath),
    ]).catch(() => {
      throw new Error("SBR_SIMULATOR_PROFILE_MISSING");
    });
    const authenticated = authenticateSbrProfileBytes({
      now: new Date(),
      profileBytes,
      publicKey,
      signatureBytes,
    });
    const helperSha256 = sha256(signed[0]);
    if (authenticated.helper_sha256 !== helperSha256)
      throw new Error("SBR_SIMULATOR_HELPER_HASH_MISMATCH");
    await commandRunner(
      "mise",
      [
        "exec",
        "--",
        "go",
        "test",
        "./services/core/internal/sbrprofile",
        "-run",
        "TestCommittedSimulatorProfileAuthenticatesAndBindsRequestedHelper",
        "-count=1",
      ],
      {
        cwd: root,
        env: {
          HOME: process.env.HOME,
          PATH: process.env.PATH,
          GOCACHE: goCache,
          GOTMPDIR: goTemporary,
          LANG: "C",
          LC_ALL: "C",
          TAMMY_SBR_HELPER_PATH: plan.destination,
        },
      },
    );
    const stagedProfileRoot = path.join(root, "apps/desktop/resources/sbr/simulator");
    await rm(path.join(root, "apps/desktop/resources/sbr"), { force: true, recursive: true });
    await mkdir(stagedProfileRoot, { recursive: true });
    await Promise.all([
      copyFile(profilePath, path.join(stagedProfileRoot, "sbr-profile-v1.json")),
      copyFile(signaturePath, path.join(stagedProfileRoot, "sbr-profile-v1.sig")),
    ]);
    const provenance = {
      helper_raw_sha256: sha256(raw[0]),
      helper_sha256: helperSha256,
      profile_sha256: sha256(profileBytes),
      profile_signature_sha256: sha256(signatureBytes),
      session_nonce: randomBytes(16).toString("hex"),
      source_revision: await currentRevision(root, commandRunner),
      source_tree_sha256: await hashSbrHelperSourceTree(path.join(root, "services/sbr-helper")),
      status: selected.status,
      target: selected.target,
    };
    await mkdir(path.join(root, ".tmp/sbr-helper-build"), { recursive: true });
    await writeFile(
      path.join(root, ".tmp/sbr-helper-build/provenance.json"),
      `${JSON.stringify(provenance, null, 2)}\n`,
      { mode: 0o600 },
    );
    return provenance;
  } finally {
    await rm(staging, { force: true, recursive: true });
  }
}

export async function executeSbrHelperBuild({
  root,
  args,
  environment = process.env,
  commandRunner = run,
}) {
  if (
    args.length > 1 ||
    args.some(
      (argument) => !["--mas-raw", "--mas-profile", "--mas-profile-unsigned"].includes(argument),
    )
  )
    throw new Error("SBR_HELPER_BUILD_ARGUMENTS_INVALID");
  if (args.length === 1 && environment[SBR_BUILD_LOCK_ENV] === undefined) {
    throw new Error("SBR_BUILD_LOCK_REQUIRED");
  }
  const ownership = await enterSbrBuildOwnership(root, environment);
  try {
    return args.includes("--mas-raw")
      ? await buildMasRawHelper({ root, commandRunner })
      : args.includes("--mas-profile-unsigned")
        ? await buildMasSimulatorProfile({ root, commandRunner, allowUnsigned: true })
        : args.includes("--mas-profile")
          ? await buildMasSimulatorProfile({ root, commandRunner })
          : await buildSbrHelper({ root, commandRunner });
  } finally {
    await ownership.release();
  }
}

async function main() {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const result = await executeSbrHelperBuild({
    args: process.argv.slice(2),
    root,
  });
  process.stdout.write(`${result.status ?? result.mode}:${result.target}\n`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    process.stderr.write(`${error instanceof Error ? error.message : "SBR_HELPER_BUILD_FAILED"}\n`);
    process.exitCode = 1;
  });
}
