import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { lstat, readdir, readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

async function listRegularResourceFiles(root) {
  const files = [];
  async function visit(directory, prefix) {
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      const absolute = path.join(directory, entry.name);
      const relative = prefix ? `${prefix}/${entry.name}` : entry.name;
      const stats = await lstat(absolute);
      assert.ok(!stats.isSymbolicLink());
      if (stats.isDirectory()) await visit(absolute, relative);
      else {
        assert.ok(stats.isFile());
        files.push(relative);
      }
    }
  }
  await visit(root, "");
  return files.sort();
}

function packagedResources(desktopRoot) {
  if (process.platform === "darwin") {
    return path.join(
      desktopRoot,
      "out",
      `Tammy-${process.platform}-${process.arch}`,
      "Tammy.app",
      "Contents",
      "Resources",
    );
  }
  if (process.platform === "win32") {
    return path.join(desktopRoot, "out", `Tammy-${process.platform}-${process.arch}`, "resources");
  }
  throw new Error("UNSUPPORTED_PACKAGED_TARGET");
}

test("the macOS application bundle has a valid signature", {
  skip: process.platform !== "darwin",
}, async () => {
  const desktopRoot = path.resolve(import.meta.dirname, "../..");
  const appBundle = path.join(
    desktopRoot,
    "out",
    `Tammy-${process.platform}-${process.arch}`,
    "Tammy.app",
  );

  await execFileAsync(
    "/usr/bin/codesign",
    ["--verify", "--deep", "--strict", "--verbose=2", appBundle],
    { encoding: "utf8", maxBuffer: 1024 * 1024, timeout: 10_000 },
  );
});

test("the ad-hoc macOS executable does not enable hardened runtime", {
  skip: process.platform !== "darwin",
}, async () => {
  const desktopRoot = path.resolve(import.meta.dirname, "../..");
  const appExecutable = path.join(
    desktopRoot,
    "out",
    `Tammy-${process.platform}-${process.arch}`,
    "Tammy.app",
    "Contents",
    "MacOS",
    "Tammy",
  );

  const { stderr } = await execFileAsync("/usr/bin/codesign", ["-dvvv", appExecutable], {
    encoding: "utf8",
    maxBuffer: 1024 * 1024,
    timeout: 10_000,
  });
  assert.match(stderr, /Signature=adhoc/);
  assert.doesNotMatch(stderr, /flags=.*\bruntime\b/);
});

test("the macOS bundle omits unused sensitive permissions and unrestricted transport", {
  skip: process.platform !== "darwin",
}, async () => {
  const desktopRoot = path.resolve(import.meta.dirname, "../..");
  const infoPlist = path.join(
    desktopRoot,
    "out",
    `Tammy-${process.platform}-${process.arch}`,
    "Tammy.app",
    "Contents",
    "Info.plist",
  );
  const { stdout } = await execFileAsync(
    "/usr/bin/plutil",
    ["-convert", "json", "-o", "-", infoPlist],
    { encoding: "utf8", maxBuffer: 1024 * 1024, timeout: 10_000 },
  );
  const info = JSON.parse(stdout);

  for (const key of [
    "NSAppTransportSecurity",
    "NSAudioCaptureUsageDescription",
    "NSBluetoothAlwaysUsageDescription",
    "NSBluetoothPeripheralUsageDescription",
    "NSCameraUsageDescription",
    "NSMicrophoneUsageDescription",
  ]) {
    assert.equal(Object.hasOwn(info, key), false, `${key} must not be shipped`);
  }
});

test("the package authenticates its SQLCipher core and licence resources", async () => {
  const desktopRoot = path.resolve(import.meta.dirname, "../..");
  const repositoryRoot = path.resolve(desktopRoot, "../..");
  const resources = packagedResources(desktopRoot);
  const target = `${process.platform}-${process.arch}`;
  const executable = process.platform === "win32" ? "tammy-core.exe" : "tammy-core";
  const core = path.join(resources, "core", target, executable);
  const cipherRoot = path.join(resources, "sqlcipher");
  const library = path.join(cipherRoot, target, "lib", "libsqlite3.a");
  const header = path.join(cipherRoot, target, "include", "sqlite3.h");
  const [
    manifestBytes,
    version,
    declaredHash,
    declaredHeaderHash,
    libraryBytes,
    headerBytes,
    packagedLicense,
    pinnedLicense,
  ] = await Promise.all([
    readFile(path.join(resources, "build", "build-manifest.json")),
    readFile(path.join(cipherRoot, "VERSION"), "utf8"),
    readFile(path.join(cipherRoot, target, "LIBRARY_SHA256"), "utf8"),
    readFile(path.join(cipherRoot, target, "HEADER_SHA256"), "utf8"),
    readFile(library),
    readFile(header),
    readFile(path.join(cipherRoot, "LICENSE")),
    readFile(path.join(repositoryRoot, "third_party/sqlcipher/LICENSE")),
  ]);
  const manifest = JSON.parse(manifestBytes.toString("utf8"));
  const librarySha256 = createHash("sha256").update(libraryBytes).digest("hex");
  const headerSha256 = createHash("sha256").update(headerBytes).digest("hex");
  assert.equal(version, "4.15.0\n");
  assert.equal(declaredHash, `${librarySha256}\n`);
  assert.equal(declaredHeaderHash, `${headerSha256}\n`);
  assert.equal(manifest.sqlcipher.version, "4.15.0");
  assert.equal(manifest.sqlcipher.runtime_version, "4.15.0 community");
  assert.equal(manifest.sqlcipher.library_sha256, librarySha256);
  assert.deepEqual(packagedLicense, pinnedLicense);
  const expectedResourceFiles = [
    "LICENSE",
    "VERSION",
    `${target}/HEADER_SHA256`,
    `${target}/LIBRARY_SHA256`,
    `${target}/include/sqlite3.h`,
    `${target}/lib/libsqlite3.a`,
    ...(process.platform === "win32"
      ? ["OPENSSL_LICENSE", `${target}/OPENSSL_LIBRARY_SHA256`, `${target}/lib/libcrypto.a`]
      : []),
  ].sort();
  assert.deepEqual(await listRegularResourceFiles(cipherRoot), expectedResourceFiles);

  const { stdout } = await execFileAsync(core, ["--sqlcipher-status"], {
    encoding: "utf8",
    maxBuffer: 1024 * 1024,
    timeout: 10_000,
    windowsHide: true,
  });
  assert.deepEqual(JSON.parse(stdout), {
    library_sha256: librarySha256,
    ordinary_sqlite_fallback: false,
    runtime_version: "4.15.0 community",
    version: "4.15.0",
  });

  if (process.platform === "darwin") {
    const { stdout: linkedLibraries } = await execFileAsync("/usr/bin/otool", ["-L", core], {
      encoding: "utf8",
      maxBuffer: 1024 * 1024,
      timeout: 10_000,
    });
    assert.doesNotMatch(linkedLibraries, /(?:lib)?sqlite|sqlcipher/i);
  } else if (process.platform === "win32") {
    const provider = path.join(cipherRoot, target, "lib", "libcrypto.a");
    const [providerBytes, declaredProviderHash, packagedProviderLicense, pinnedProviderLicense] =
      await Promise.all([
        readFile(provider),
        readFile(path.join(cipherRoot, target, "OPENSSL_LIBRARY_SHA256"), "utf8"),
        readFile(path.join(cipherRoot, "OPENSSL_LICENSE")),
        readFile(path.join(repositoryRoot, "third_party/sqlcipher/OPENSSL_LICENSE")),
      ]);
    const providerSha256 = createHash("sha256").update(providerBytes).digest("hex");
    assert.equal(declaredProviderHash, `${providerSha256}\n`);
    assert.deepEqual(packagedProviderLicense, pinnedProviderLicense);

    const inspectionTool = process.env.TAMMY_SQLCIPHER_LLVM_READOBJ;
    assert.equal(typeof inspectionTool, "string");
    assert.ok(path.isAbsolute(inspectionTool));
    const inspectionToolStats = await lstat(inspectionTool);
    assert.ok(inspectionToolStats.isFile());
    assert.ok(!inspectionToolStats.isSymbolicLink());
    const { stdout: imports } = await execFileAsync(inspectionTool, ["--coff-imports", core], {
      encoding: "utf8",
      maxBuffer: 4 * 1024 * 1024,
      timeout: 10_000,
      windowsHide: true,
    });
    assert.match(imports, /Name: [^\r\n]*\.dll/i);
    assert.doesNotMatch(imports, /(?:sqlite|sqlcipher|libcrypto)[^\r\n]*\.dll/i);
  }
});
