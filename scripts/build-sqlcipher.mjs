import { execFile as nodeExecFile } from "node:child_process";
import { createHash } from "node:crypto";
import { cp, lstat, mkdir, mkdtemp, readFile, rename, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import {
  extractPinnedSource,
  hashSourceTree,
  SQLCIPHER_RELEASE,
  vendorSqlcipher,
} from "./vendor-sqlcipher.mjs";

const BUILD_TIMEOUT_MS = 10 * 60_000;
const OPENSSL_LICENSE_SHA256 = "657443fd2340ed2d45d44ceb57df718bc810abcc060a9032f93759c4afab9fa2";
const OPENSSL_SOURCE_SHA256 = "d71a811bfbd9153d7b30cbe476263302ee4b04a9a47ffea6e6a782326805c93f";
const OPENSSL_VERSION = "3.5.7";
const OPENSSL_RELEASE = Object.freeze({
  archiveName: "openssl-3.5.7.tar.gz",
  globalPax: "52 comment=8cf17aaeb4599f8af87fefd810b5b5fee90fe69e\n",
  rootDirectory: "openssl-openssl-3.5.7",
  sha256: OPENSSL_SOURCE_SHA256,
  url: "https://codeload.github.com/openssl/openssl/tar.gz/refs/tags/openssl-3.5.7",
});
const DEFINES = Object.freeze([
  "SQLITE_HAS_CODEC",
  "SQLITE_EXTRA_INIT=sqlcipher_extra_init",
  "SQLITE_EXTRA_SHUTDOWN=sqlcipher_extra_shutdown",
  "SQLCIPHER_CRYPTO_CC",
  "SQLITE_TEMP_STORE=2",
  "SQLITE_THREADSAFE=1",
  "SQLITE_OMIT_LOAD_EXTENSION",
  "SQLITE_DQS=0",
]);
const WINDOWS_DEFINES = Object.freeze(
  DEFINES.map((value) => (value === "SQLCIPHER_CRYPTO_CC" ? "SQLCIPHER_CRYPTO_OPENSSL" : value)),
);

const TARGETS = Object.freeze({
  "darwin/arm64": Object.freeze({
    arch: "arm64",
    goarch: "arm64",
    goos: "darwin",
    platform: "darwin",
    resourceTarget: "darwin-arm64",
  }),
  "win32/x64": Object.freeze({
    arch: "x64",
    goarch: "amd64",
    goos: "windows",
    platform: "win32",
    resourceTarget: "win32-x64",
  }),
});

function fail(code = "SQLCIPHER_BUILD_INPUT_INVALID") {
  throw new Error(code);
}

export function selectSqlcipherTarget(platform, arch) {
  const selected = TARGETS[`${platform}/${arch}`];
  if (!selected) fail("UNSUPPORTED_SQLCIPHER_TARGET");
  return { ...selected };
}

function assertContained(parent, candidate) {
  const relative = path.relative(parent, candidate);
  if (
    relative === "" ||
    relative === ".." ||
    relative.startsWith(`..${path.sep}`) ||
    path.isAbsolute(relative)
  ) {
    fail();
  }
}

export function createSqlcipherBuildPlan({
  archiver,
  buildRoot,
  compiler,
  generatorCompiler,
  librarian,
  linker,
  hostArch,
  hostPlatform,
  make,
  manifestTool,
  opensslInclude,
  opensslLicense,
  opensslLibrary,
  opensslSourceSha256,
  opensslVersion,
  resourceCompiler,
  root,
  sourceRoot,
  target,
}) {
  const selected = selectSqlcipherTarget(target?.platform, target?.arch);
  const actualHostPlatform = hostPlatform ?? selected.platform;
  const actualHostArch = hostArch ?? selected.arch;
  if (actualHostPlatform !== selected.platform || actualHostArch !== selected.arch) {
    fail("SQLCIPHER_CROSS_BUILD_UNSUPPORTED");
  }
  for (const candidate of [root, sourceRoot, buildRoot, compiler]) {
    if (
      typeof candidate !== "string" ||
      !path.isAbsolute(candidate) ||
      path.normalize(candidate) !== candidate
    ) {
      fail();
    }
  }
  assertContained(root, sourceRoot);
  assertContained(root, buildRoot);
  const resourceRoot = path.join(root, "apps/desktop/resources/sqlcipher");
  const targetRoot = path.join(resourceRoot, selected.resourceTarget);
  const windows = selected.platform === "win32";
  if (windows) {
    for (const candidate of [
      archiver,
      generatorCompiler,
      librarian,
      linker,
      make,
      manifestTool,
      opensslInclude,
      opensslLicense,
      opensslLibrary,
      resourceCompiler,
    ]) {
      if (
        typeof candidate !== "string" ||
        !path.isAbsolute(candidate) ||
        path.normalize(candidate) !== candidate
      ) {
        fail("SQLCIPHER_WINDOWS_TOOLCHAIN_INVALID");
      }
    }
    if (opensslSourceSha256 !== OPENSSL_SOURCE_SHA256 || opensslVersion !== OPENSSL_VERSION) {
      fail("SQLCIPHER_WINDOWS_TOOLCHAIN_INVALID");
    }
  }
  return Object.freeze({
    amalgamationWrapper: path.join(root, "scripts/sqlcipher-amalgamation.c"),
    archiver: windows ? archiver : "/usr/bin/libtool",
    buildRoot,
    compiler,
    defines: windows ? WINDOWS_DEFINES : DEFINES,
    frameworks: windows ? [] : ["CoreFoundation", "Security"],
    generatorCompiler: windows ? generatorCompiler : undefined,
    librarian: windows ? librarian : undefined,
    linker: windows ? linker : undefined,
    library: path.join(buildRoot, "libsqlite3.a"),
    make: windows ? make : "/usr/bin/make",
    manifestTool: windows ? manifestTool : undefined,
    opensslInclude: windows ? opensslInclude : undefined,
    opensslLicense: windows ? opensslLicense : undefined,
    opensslLibrary: windows ? opensslLibrary : undefined,
    ordinaryReader: windows
      ? path.join(root, ".tmp/sqlcipher/ordinary/win32-x64/ordinary-sqlite3.exe")
      : undefined,
    ordinaryReaderSource: windows ? path.join(root, "scripts/ordinary-sqlite-probe.c") : undefined,
    resourceCompiler: windows ? resourceCompiler : undefined,
    resources: Object.freeze({
      header: path.join(targetRoot, "include/sqlite3.h"),
      headerSha256: path.join(targetRoot, "HEADER_SHA256"),
      license: path.join(resourceRoot, "LICENSE"),
      library: path.join(targetRoot, "lib/libsqlite3.a"),
      librarySha256: path.join(targetRoot, "LIBRARY_SHA256"),
      version: path.join(resourceRoot, "VERSION"),
      ...(windows
        ? {
            opensslLicense: path.join(resourceRoot, "OPENSSL_LICENSE"),
            opensslLibrary: path.join(targetRoot, "lib/libcrypto.a"),
            opensslLibrarySha256: path.join(targetRoot, "OPENSSL_LIBRARY_SHA256"),
          }
        : {}),
    }),
    sourceRoot,
    target: Object.freeze(selected),
  });
}

function productionExecFile(command, args, options) {
  return new Promise((resolve, reject) => {
    nodeExecFile(command, args, options, (error, stdout, stderr) => {
      if (error) reject(error);
      else resolve({ stderr, stdout });
    });
  });
}

async function requireRegularFile(candidate, code) {
  const stats = await lstat(candidate).catch(() => null);
  if (!stats?.isFile() || stats.isSymbolicLink()) fail(code);
}

async function requireDirectory(candidate, code) {
  const stats = await lstat(candidate).catch(() => null);
  if (!stats?.isDirectory() || stats.isSymbolicLink()) fail(code);
}

async function requireWindowsDirectoryList(value) {
  if (typeof value !== "string" || value === "") {
    fail("SQLCIPHER_WINDOWS_TOOLCHAIN_INVALID");
  }
  const directories = value.split(";");
  if (directories.some((candidate) => candidate === "")) {
    fail("SQLCIPHER_WINDOWS_TOOLCHAIN_INVALID");
  }
  for (const candidate of directories) {
    if (!path.isAbsolute(candidate) || path.normalize(candidate) !== candidate) {
      fail("SQLCIPHER_WINDOWS_TOOLCHAIN_INVALID");
    }
    await requireDirectory(candidate, "SQLCIPHER_WINDOWS_TOOLCHAIN_INVALID");
  }
}

async function findUniqueRegularFile(root, filename) {
  const matches = [];
  async function visit(directory) {
    for (const entry of await import("node:fs/promises").then(({ readdir }) =>
      readdir(directory, { withFileTypes: true }),
    )) {
      const candidate = path.join(directory, entry.name);
      const stats = await lstat(candidate);
      if (stats.isSymbolicLink()) fail("SQLCIPHER_OPENSSL_BUILD_INVALID");
      if (stats.isDirectory()) await visit(candidate);
      else if (stats.isFile() && entry.name.toLowerCase() === filename.toLowerCase()) {
        matches.push(candidate);
      }
    }
  }
  await visit(root);
  if (matches.length !== 1) fail("SQLCIPHER_OPENSSL_BUILD_INVALID");
  return matches[0];
}

async function execute(execFile, command, args, options = {}) {
  try {
    return await execFile(command, args, {
      ...options,
      encoding: "utf8",
      maxBuffer: 4 * 1024 * 1024,
      shell: false,
      timeout: BUILD_TIMEOUT_MS,
      windowsHide: true,
    });
  } catch {
    fail("SQLCIPHER_BUILD_FAILED");
  }
}

async function compileMac(plan, source, execFile) {
  const commonFlags = plan.defines.map((value) => `-D${value}`);
  const cflags = ["-O2", "-g0", "-fPIC", ...commonFlags].join(" ");
  await execute(
    execFile,
    "/bin/sh",
    [path.join(source, "configure"), "--with-tempstore=yes", "--disable-shared", "--enable-static"],
    {
      cwd: source,
      env: {
        ...process.env,
        CC: plan.compiler,
        CFLAGS: cflags,
      },
    },
  );
  await execute(execFile, plan.make, ["-j2", "sqlite3.c", "sqlite3.h"], { cwd: source });
  await execute(
    execFile,
    plan.compiler,
    [
      "-O2",
      "-g0",
      "-fPIC",
      `-ffile-prefix-map=${source}=.`,
      ...commonFlags,
      `-I${source}`,
      "-c",
      plan.amalgamationWrapper,
      "-o",
      path.join(plan.buildRoot, "sqlite3.o"),
    ],
    { cwd: plan.buildRoot },
  );
  await execute(
    execFile,
    plan.archiver,
    ["-static", "-D", "-o", plan.library, path.join(plan.buildRoot, "sqlite3.o")],
    { cwd: plan.buildRoot },
  );
}

async function compileWindows(plan, source, execFile) {
  const commonFlags = plan.defines.map((value) => `-D${value}`);
  await execute(
    execFile,
    plan.make,
    [
      "/nologo",
      "/f",
      "Makefile.msc",
      `CC=${plan.generatorCompiler}`,
      `NCC=${plan.generatorCompiler}`,
      `LD=${plan.linker}`,
      `LTLIB=${plan.librarian}`,
      `RC=${plan.resourceCompiler}`,
      "sqlite3.c",
      "sqlite3.h",
    ],
    { cwd: source },
  );
  await execute(
    execFile,
    plan.compiler,
    [
      "-O2",
      "-g0",
      ...commonFlags,
      `-I${plan.opensslInclude}`,
      `-I${source}`,
      "-c",
      plan.amalgamationWrapper,
      "-o",
      path.join(plan.buildRoot, "sqlite3.o"),
    ],
    { cwd: plan.buildRoot },
  );
  await execute(
    execFile,
    plan.archiver,
    ["rcsD", plan.library, path.join(plan.buildRoot, "sqlite3.o")],
    { cwd: plan.buildRoot },
  );
  await mkdir(path.dirname(plan.ordinaryReader), { recursive: true, mode: 0o700 });
  await execute(
    execFile,
    plan.compiler,
    [
      "-O2",
      "-g0",
      "-DSQLITE_THREADSAFE=1",
      "-DSQLITE_OMIT_LOAD_EXTENSION",
      "-DSQLITE_DQS=0",
      `-I${source}`,
      `--ld-path=${plan.linker}`,
      "-Wl,/Brepro",
      plan.amalgamationWrapper,
      plan.ordinaryReaderSource,
      "-o",
      plan.ordinaryReader,
    ],
    { cwd: plan.buildRoot },
  );
}

export async function buildWindowsProvider({
  buildRoot,
  environment,
  execFile,
  extractSource = extractPinnedSource,
  repositoryRoot,
}) {
  const perl = environment.TAMMY_SQLCIPHER_PERL;
  const make = environment.TAMMY_SQLCIPHER_NMAKE;
  const compiler = environment.TAMMY_SQLCIPHER_NMAKE_CC;
  const comspec = environment.TAMMY_SQLCIPHER_COMSPEC;
  const librarian = environment.TAMMY_SQLCIPHER_LIB;
  const linker = environment.TAMMY_SQLCIPHER_LINK;
  const manifestTool = environment.TAMMY_SQLCIPHER_MT;
  const resourceCompiler = environment.TAMMY_SQLCIPHER_RC;
  for (const candidate of [
    perl,
    make,
    compiler,
    librarian,
    linker,
    manifestTool,
    resourceCompiler,
  ]) {
    if (
      typeof candidate !== "string" ||
      !path.isAbsolute(candidate) ||
      path.normalize(candidate) !== candidate
    ) {
      fail("SQLCIPHER_WINDOWS_TOOLCHAIN_INVALID");
    }
    await requireRegularFile(candidate, "SQLCIPHER_WINDOWS_TOOLCHAIN_INVALID");
  }
  if (
    typeof comspec !== "string" ||
    !path.isAbsolute(comspec) ||
    path.normalize(comspec) !== comspec ||
    path.basename(comspec).toLowerCase() !== "cmd.exe" ||
    path.basename(path.dirname(comspec)).toLowerCase() !== "system32"
  ) {
    fail("SQLCIPHER_WINDOWS_TOOLCHAIN_INVALID");
  }
  await requireRegularFile(comspec, "SQLCIPHER_WINDOWS_TOOLCHAIN_INVALID");
  const systemRoot = path.dirname(path.dirname(comspec));
  await Promise.all([
    requireDirectory(systemRoot, "SQLCIPHER_WINDOWS_TOOLCHAIN_INVALID"),
    requireDirectory(path.dirname(comspec), "SQLCIPHER_WINDOWS_TOOLCHAIN_INVALID"),
    requireDirectory(buildRoot, "SQLCIPHER_WINDOWS_TOOLCHAIN_INVALID"),
    requireWindowsDirectoryList(environment.INCLUDE),
    requireWindowsDirectoryList(environment.LIB),
  ]);
  const extracted = path.join(buildRoot, "openssl-source");
  const source = await extractSource({ destination: extracted, pin: OPENSSL_RELEASE });
  const license = path.join(source, "LICENSE.txt");
  const [licenseBytes, pinnedLicense] = await Promise.all([
    readFile(license),
    readFile(path.join(repositoryRoot, "third_party/sqlcipher/OPENSSL_LICENSE")),
  ]);
  if (
    !licenseBytes.equals(pinnedLicense) ||
    createHash("sha256").update(licenseBytes).digest("hex") !== OPENSSL_LICENSE_SHA256
  ) {
    fail("SQLCIPHER_OPENSSL_LICENSE_INVALID");
  }
  const install = path.join(buildRoot, "openssl-install");
  const toolPath = [
    path.dirname(perl),
    path.dirname(make),
    path.dirname(compiler),
    path.dirname(librarian),
    path.dirname(linker),
    path.dirname(manifestTool),
    path.dirname(resourceCompiler),
    path.dirname(comspec),
  ]
    .filter((candidate, index, candidates) => candidates.indexOf(candidate) === index)
    .join(";");
  const toolEnvironment = {
    AR: librarian,
    CC: compiler,
    ComSpec: comspec,
    INCLUDE: environment.INCLUDE,
    LANG: "C",
    LC_ALL: "C",
    LD: linker,
    LIB: environment.LIB,
    MT: manifestTool,
    PATH: toolPath,
    PERL: perl,
    RC: resourceCompiler,
    SOURCE_DATE_EPOCH: "1781042040",
    SystemRoot: systemRoot,
    TEMP: buildRoot,
    TMP: buildRoot,
    TZ: "UTC",
  };
  await execute(
    execFile,
    perl,
    [
      path.join(source, "Configure"),
      "VC-WIN64A",
      "no-asm",
      "no-shared",
      "no-tests",
      "no-apps",
      "no-module",
      "no-legacy",
      `--prefix=${install}`,
      `--openssldir=${path.join(install, "ssl")}`,
    ],
    { cwd: source, env: toolEnvironment },
  );
  const generatedMakefile = await readFile(path.join(source, "makefile"), "utf8");
  const expectedAssignments = new Map([
    ["CC", compiler],
    ["AR", librarian],
    ["LD", linker],
    ["MT", manifestTool],
    ["PERL", perl],
    ["RC", resourceCompiler],
  ]);
  const effectiveAssignments = new Map();
  for (const line of generatedMakefile.replaceAll("\r\n", "\n").split("\n")) {
    const match = /^[\t ]*(CC|AR|LD|MT|PERL|RC)[\t ]*(\?=|\+=|:=|=)[\t ]*(.*?)[\t ]*$/.exec(line);
    if (!match) continue;
    const [, name, operator, rawValue] = match;
    const value = rawValue.trim();
    const unquoted =
      value.length >= 2 &&
      ((value.startsWith('"') && value.endsWith('"')) ||
        (value.startsWith("'") && value.endsWith("'")))
        ? value.slice(1, -1)
        : value;
    if (operator === "?=" && effectiveAssignments.has(name)) continue;
    if (operator === "+=") {
      effectiveAssignments.set(name, `${effectiveAssignments.get(name) ?? ""} ${unquoted}`.trim());
      continue;
    }
    effectiveAssignments.set(name, unquoted);
  }
  for (const [name, expected] of expectedAssignments) {
    if (effectiveAssignments.get(name) !== expected) {
      fail("SQLCIPHER_OPENSSL_TOOLCHAIN_DRIFT");
    }
  }
  const exactCompiler = `CC=${compiler}`;
  const exactTools = [
    exactCompiler,
    `AR=${librarian}`,
    `LD=${linker}`,
    `MT=${manifestTool}`,
    `PERL=${perl}`,
    `RC=${resourceCompiler}`,
  ];
  await execute(execFile, make, ["/nologo", ...exactTools, "build_libs"], {
    cwd: source,
    env: toolEnvironment,
  });
  await execute(execFile, make, ["/nologo", ...exactTools, "install_dev"], {
    cwd: source,
    env: toolEnvironment,
  });
  const include = path.join(install, "include");
  await requireDirectory(include, "SQLCIPHER_OPENSSL_BUILD_INVALID");
  const versionHeader = await readFile(path.join(include, "openssl/opensslv.h"), "utf8");
  if (!versionHeader.includes(`OPENSSL_VERSION_STR "${OPENSSL_VERSION}"`)) {
    fail("SQLCIPHER_OPENSSL_BUILD_INVALID");
  }
  return Object.freeze({
    include,
    library: await findUniqueRegularFile(path.join(install, "lib"), "libcrypto.lib"),
    license,
  });
}

function sameDirectoryIdentity(current, expected) {
  return Boolean(
    current?.isDirectory() &&
      !current.isSymbolicLink() &&
      current.dev === expected.dev &&
      current.ino === expected.ino,
  );
}

async function stageResources(plan, source, { cacheIdentity, cacheRoot, resourceHooks = {} }) {
  const resourceRoot = path.dirname(plan.resources.license);
  const resourceParent = path.dirname(resourceRoot);
  const parentIdentity = await lstat(resourceParent, { bigint: true }).catch(() => null);
  if (
    !sameDirectoryIdentity(
      await lstat(cacheRoot, { bigint: true }).catch(() => null),
      cacheIdentity,
    ) ||
    !parentIdentity?.isDirectory() ||
    parentIdentity.isSymbolicLink()
  ) {
    fail("SQLCIPHER_RESOURCE_ROOT_INVALID");
  }
  const initialStats = await lstat(resourceRoot, { bigint: true }).catch(() => null);
  if (initialStats !== null) {
    if (!initialStats.isDirectory() || initialStats.isSymbolicLink()) {
      fail("SQLCIPHER_RESOURCE_ROOT_INVALID");
    }
    let retirementDirectory;
    try {
      retirementDirectory = await mkdtemp(path.join(cacheRoot, ".retired-resources-"));
    } catch {
      fail("SQLCIPHER_RESOURCE_ROOT_INVALID");
    }
    const retirementIdentity = await lstat(retirementDirectory, { bigint: true }).catch(() => null);
    if (!retirementIdentity?.isDirectory() || retirementIdentity.isSymbolicLink()) {
      fail("SQLCIPHER_RESOURCE_ROOT_INVALID");
    }
    const retiredRoot = path.join(retirementDirectory, "sqlcipher");
    const context = Object.freeze({ cacheRoot, resourceParent, resourceRoot, retiredRoot });
    if (resourceHooks.beforeResourceRetirementRename) {
      await resourceHooks.beforeResourceRetirementRename(context);
    }
    const [currentCache, currentParent, currentRetirement] = await Promise.all([
      lstat(cacheRoot, { bigint: true }).catch(() => null),
      lstat(resourceParent, { bigint: true }).catch(() => null),
      lstat(retirementDirectory, { bigint: true }).catch(() => null),
    ]);
    if (
      !sameDirectoryIdentity(currentCache, cacheIdentity) ||
      !sameDirectoryIdentity(currentParent, parentIdentity) ||
      !sameDirectoryIdentity(currentRetirement, retirementIdentity)
    ) {
      fail("SQLCIPHER_RESOURCE_ROOT_INVALID");
    }
    try {
      await rename(resourceRoot, retiredRoot);
    } catch {
      fail("SQLCIPHER_RESOURCE_ROOT_INVALID");
    }
    const [movedRoot, cacheAfterRename, parentAfterRename, retirementAfterRename] =
      await Promise.all([
        lstat(retiredRoot, { bigint: true }).catch(() => null),
        lstat(cacheRoot, { bigint: true }).catch(() => null),
        lstat(resourceParent, { bigint: true }).catch(() => null),
        lstat(retirementDirectory, { bigint: true }).catch(() => null),
      ]);
    if (
      !sameDirectoryIdentity(movedRoot, initialStats) ||
      !sameDirectoryIdentity(cacheAfterRename, cacheIdentity) ||
      !sameDirectoryIdentity(parentAfterRename, parentIdentity) ||
      !sameDirectoryIdentity(retirementAfterRename, retirementIdentity)
    ) {
      fail("SQLCIPHER_RESOURCE_ROOT_INVALID");
    }
  }
  if (resourceHooks.beforeNewResourceRootCreate) {
    await resourceHooks.beforeNewResourceRootCreate(
      Object.freeze({ cacheRoot, resourceParent, resourceRoot }),
    );
  }
  const [cacheBeforeCreate, parentBeforeCreate] = await Promise.all([
    lstat(cacheRoot, { bigint: true }).catch(() => null),
    lstat(resourceParent, { bigint: true }).catch(() => null),
  ]);
  if (
    !sameDirectoryIdentity(cacheBeforeCreate, cacheIdentity) ||
    !sameDirectoryIdentity(parentBeforeCreate, parentIdentity)
  ) {
    fail("SQLCIPHER_RESOURCE_ROOT_INVALID");
  }
  try {
    await mkdir(resourceRoot, { mode: 0o700 });
  } catch {
    fail("SQLCIPHER_RESOURCE_ROOT_INVALID");
  }
  const stagedIdentity = await lstat(resourceRoot, { bigint: true }).catch(() => null);
  if (!stagedIdentity?.isDirectory() || stagedIdentity.isSymbolicLink()) {
    fail("SQLCIPHER_RESOURCE_ROOT_INVALID");
  }
  await Promise.all([
    mkdir(path.dirname(plan.resources.header), { recursive: true }),
    mkdir(path.dirname(plan.resources.library), { recursive: true }),
  ]);
  await Promise.all([
    cp(path.join(source, "sqlite3.h"), plan.resources.header, { force: true }),
    cp(plan.library, plan.resources.library, { force: true }),
    cp(path.join(source, "LICENSE.md"), plan.resources.license, { force: true }),
    writeFile(plan.resources.version, `${SQLCIPHER_RELEASE.version}\n`, { mode: 0o600 }),
  ]);
  const libraryBytes = await readFile(plan.resources.library);
  const headerBytes = await readFile(plan.resources.header);
  const librarySha256 = createHash("sha256").update(libraryBytes).digest("hex");
  const headerSha256 = createHash("sha256").update(headerBytes).digest("hex");
  await Promise.all([
    writeFile(plan.resources.headerSha256, `${headerSha256}\n`, { mode: 0o600 }),
    writeFile(plan.resources.librarySha256, `${librarySha256}\n`, { mode: 0o600 }),
  ]);
  let opensslLibrarySha256;
  if (plan.target.platform === "win32") {
    const licenseBytes = await readFile(plan.opensslLicense);
    if (createHash("sha256").update(licenseBytes).digest("hex") !== OPENSSL_LICENSE_SHA256) {
      fail("SQLCIPHER_OPENSSL_LICENSE_INVALID");
    }
    const providerBytes = await readFile(plan.opensslLibrary);
    opensslLibrarySha256 = createHash("sha256").update(providerBytes).digest("hex");
    await Promise.all([
      cp(plan.opensslLibrary, plan.resources.opensslLibrary, { force: true }),
      cp(plan.opensslLicense, plan.resources.opensslLicense, { force: true }),
      writeFile(plan.resources.opensslLibrarySha256, `${opensslLibrarySha256}\n`, { mode: 0o600 }),
    ]);
  }
  const [finalRoot, finalCache, finalParent] = await Promise.all([
    lstat(resourceRoot, { bigint: true }).catch(() => null),
    lstat(cacheRoot, { bigint: true }).catch(() => null),
    lstat(resourceParent, { bigint: true }).catch(() => null),
  ]);
  if (
    !sameDirectoryIdentity(finalRoot, stagedIdentity) ||
    !sameDirectoryIdentity(finalCache, cacheIdentity) ||
    !sameDirectoryIdentity(finalParent, parentIdentity)
  ) {
    fail("SQLCIPHER_RESOURCE_ROOT_INVALID");
  }
  return { headerSha256, librarySha256, opensslLibrarySha256 };
}

export async function buildSqlcipher({
  arch = process.arch,
  environment = process.env,
  execFile = productionExecFile,
  platform = process.platform,
  root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), ".."),
  providerBuilder = buildWindowsProvider,
  resourceHooks = {},
  sourceTreeHasher = hashSourceTree,
  vendor = vendorSqlcipher,
} = {}) {
  const target = selectSqlcipherTarget(platform, arch);
  const amalgamationWrapper = path.join(root, "scripts/sqlcipher-amalgamation.c");
  await requireRegularFile(amalgamationWrapper, "SQLCIPHER_AMALGAMATION_WRAPPER_INVALID");
  const cacheRoot = path.join(root, ".tmp/sqlcipher");
  await mkdir(cacheRoot, { recursive: true, mode: 0o700 });
  const cacheIdentity = await lstat(cacheRoot, { bigint: true });
  if (!cacheIdentity.isDirectory() || cacheIdentity.isSymbolicLink()) {
    fail("SQLCIPHER_RESOURCE_ROOT_INVALID");
  }
  const vendored = await vendor({ cacheRoot });
  if (vendored.sourceTreeSha256 !== SQLCIPHER_RELEASE.sourceTreeSha256) {
    fail("SQLCIPHER_SOURCE_INVALID");
  }
  const buildRoot = await mkdtemp(path.join(cacheRoot, `.build-${target.resourceTarget}-`));
  try {
    const source = path.join(buildRoot, SQLCIPHER_RELEASE.rootDirectory);
    await cp(vendored.sourceRoot, source, { recursive: true, errorOnExist: true });
    if ((await sourceTreeHasher(source)) !== SQLCIPHER_RELEASE.sourceTreeSha256) {
      fail("SQLCIPHER_SOURCE_INVALID");
    }
    const provider =
      target.platform === "win32"
        ? await providerBuilder({
            buildRoot,
            environment,
            execFile,
            repositoryRoot: root,
          })
        : undefined;
    const plan = createSqlcipherBuildPlan({
      archiver: environment.TAMMY_SQLCIPHER_AR,
      buildRoot,
      compiler: target.platform === "darwin" ? "/usr/bin/clang" : environment.TAMMY_SQLCIPHER_CC,
      generatorCompiler: environment.TAMMY_SQLCIPHER_NMAKE_CC,
      librarian: environment.TAMMY_SQLCIPHER_LIB,
      linker: environment.TAMMY_SQLCIPHER_LINK,
      hostArch: arch,
      hostPlatform: platform,
      make: environment.TAMMY_SQLCIPHER_NMAKE,
      manifestTool: environment.TAMMY_SQLCIPHER_MT,
      opensslInclude: provider?.include,
      opensslLicense: provider?.license,
      opensslLibrary: provider?.library,
      opensslSourceSha256: target.platform === "win32" ? OPENSSL_SOURCE_SHA256 : undefined,
      opensslVersion: target.platform === "win32" ? OPENSSL_VERSION : undefined,
      resourceCompiler: environment.TAMMY_SQLCIPHER_RC,
      root,
      sourceRoot: vendored.sourceRoot,
      target,
    });
    await requireRegularFile(plan.compiler, "SQLCIPHER_COMPILER_MISSING");
    if (plan.amalgamationWrapper !== amalgamationWrapper) {
      fail("SQLCIPHER_AMALGAMATION_WRAPPER_INVALID");
    }
    await requireDirectory(plan.sourceRoot, "SQLCIPHER_SOURCE_MISSING");
    if (target.platform === "win32") {
      await Promise.all([
        requireRegularFile(plan.archiver, "SQLCIPHER_ARCHIVER_MISSING"),
        requireRegularFile(plan.generatorCompiler, "SQLCIPHER_COMPILER_MISSING"),
        requireRegularFile(plan.librarian, "SQLCIPHER_COMPILER_MISSING"),
        requireRegularFile(plan.linker, "SQLCIPHER_COMPILER_MISSING"),
        requireRegularFile(plan.make, "SQLCIPHER_MAKE_MISSING"),
        requireRegularFile(plan.manifestTool, "SQLCIPHER_COMPILER_MISSING"),
        requireDirectory(plan.opensslInclude, "SQLCIPHER_OPENSSL_MISSING"),
        requireRegularFile(plan.opensslLibrary, "SQLCIPHER_OPENSSL_MISSING"),
        requireRegularFile(plan.opensslLicense, "SQLCIPHER_OPENSSL_MISSING"),
        requireRegularFile(plan.resourceCompiler, "SQLCIPHER_COMPILER_MISSING"),
        requireRegularFile(plan.ordinaryReaderSource, "SQLCIPHER_ORDINARY_READER_SOURCE_MISSING"),
      ]);
      await compileWindows(plan, source, execFile);
      await requireRegularFile(plan.ordinaryReader, "SQLCIPHER_ORDINARY_READER_MISSING");
    } else {
      await compileMac(plan, source, execFile);
    }
    await requireRegularFile(plan.library, "SQLCIPHER_LIBRARY_MISSING");
    const { headerSha256, librarySha256, opensslLibrarySha256 } = await stageResources(
      plan,
      source,
      { cacheIdentity, cacheRoot, resourceHooks },
    );
    return Object.freeze({
      include: path.dirname(plan.resources.header),
      headerSha256,
      library: plan.resources.library,
      librarySha256,
      opensslLibrarySha256,
      ordinaryReader: plan.ordinaryReader,
      target: target.resourceTarget,
      version: SQLCIPHER_RELEASE.version,
      windowsLocalVerification: target.platform === "win32" ? "CI_WINDOWS11_REQUIRED" : undefined,
    });
  } finally {
    await rm(buildRoot, { force: true, recursive: true });
  }
}

async function main() {
  process.stdout.write(`${JSON.stringify(await buildSqlcipher())}\n`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  main().catch((error) => {
    process.stderr.write(
      `${JSON.stringify({ error: error instanceof Error ? error.message : "SQLCIPHER_BUILD_FAILED" })}\n`,
    );
    process.exitCode = 1;
  });
}
