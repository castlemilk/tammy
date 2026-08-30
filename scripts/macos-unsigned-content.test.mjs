import assert from "node:assert/strict";
import {
  chmod,
  lstat,
  mkdir,
  mkdtemp,
  readFile,
  rm,
  symlink,
  utimes,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import {
  authenticateUnsignedContentManifest,
  cloneUnsignedStaging,
  compareSignedCopies,
  createUnsignedContentManifest,
  validateSignedCopyAgainstUnsignedManifest,
  validateUnsignedContentManifest,
  writeUnsignedContentManifest,
} from "./macos-unsigned-content.mjs";

const sourceCommit = "a".repeat(40);
const sourceTree = "b".repeat(40);
const standardFrameworkExecutables = [
  "Contents/Frameworks/Electron Framework.framework/Versions/A/Electron Framework",
  "Contents/Frameworks/Electron Framework.framework/Versions/A/Helpers/chrome_crashpad_handler",
  "Contents/Frameworks/Electron Framework.framework/Versions/A/Libraries/libEGL.dylib",
  "Contents/Frameworks/Electron Framework.framework/Versions/A/Libraries/libGLESv2.dylib",
  "Contents/Frameworks/Electron Framework.framework/Versions/A/Libraries/libffmpeg.dylib",
  "Contents/Frameworks/Electron Framework.framework/Versions/A/Libraries/libvk_swiftshader.dylib",
  "Contents/Frameworks/Mantle.framework/Versions/A/Mantle",
  "Contents/Frameworks/ReactiveObjC.framework/Versions/A/ReactiveObjC",
  "Contents/Frameworks/Squirrel.framework/Versions/A/Resources/ShipIt",
  "Contents/Frameworks/Squirrel.framework/Versions/A/Squirrel",
];
const requiredRuntimePaths = [
  "Contents/Info.plist",
  "Contents/MacOS/Tammy",
  "Contents/Resources/PrivacyInfo.xcprivacy",
  "Contents/Resources/app.asar",
  "Contents/Resources/build/build-manifest.json",
  "Contents/Resources/core/darwin-arm64/tammy-core",
  "Contents/Resources/icon.icns",
  "Contents/Resources/sbr-helper/darwin-arm64/tammy-sbr-helper",
  "Contents/Resources/sbr/simulator/sbr-profile-v1.json",
  "Contents/Resources/sbr/simulator/sbr-profile-v1.sig",
  "Contents/Resources/sqlcipher/LICENSE",
  "Contents/Resources/sqlcipher/VERSION",
  "Contents/Resources/sqlcipher/darwin-arm64/HEADER_SHA256",
  "Contents/Resources/sqlcipher/darwin-arm64/LIBRARY_SHA256",
  "Contents/Resources/sqlcipher/darwin-arm64/include/sqlite3.h",
  "Contents/Resources/sqlcipher/darwin-arm64/lib/libsqlite3.a",
  ...standardFrameworkExecutables,
  ...["", " (GPU)", " (Plugin)", " (Renderer)"].flatMap((suffix) => [
    `Contents/Frameworks/Tammy Helper${suffix}.app/Contents/Info.plist`,
    `Contents/Frameworks/Tammy Helper${suffix}.app/Contents/MacOS/Tammy Helper${suffix}`,
  ]),
];

function bundlePlist({ bundleIdentifier, executable, helper = false }) {
  return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>${bundleIdentifier}</string>
<key>CFBundleExecutable</key><string>${executable}</string>
<key>CFBundleShortVersionString</key><string>0.1.0</string>
<key>CFBundleVersion</key><string>42</string>
${
  helper
    ? ""
    : `<key>CFBundleIconFile</key><string>icon.icns</string>
<key>TammyPrivacyPolicyURL</key><string>https://tammy-accounting.castlemilk.chatgpt.site/privacy</string>
<key>TammySupportURL</key><string>https://tammy-accounting.castlemilk.chatgpt.site/support</string>`
}
</dict></plist>\n`;
}

function manifestInput(stagingRoot) {
  return {
    buildNumber: "42",
    bundleIdentifiers: {
      app: "com.tammy.desktop",
      helpers: [
        "com.tammy.desktop.helper",
        "com.tammy.desktop.helper.GPU",
        "com.tammy.desktop.helper.Plugin",
        "com.tammy.desktop.helper.Renderer",
      ],
    },
    marketingVersion: "0.1.0",
    productSourceCommit: sourceCommit,
    productSourceTree: sourceTree,
    publicLinks: {
      privacyPolicy: "https://tammy-accounting.castlemilk.chatgpt.site/privacy",
      support: "https://tammy-accounting.castlemilk.chatgpt.site/support",
    },
    stagingRoot,
  };
}

async function createStaging(prefix = "tammy-unsigned-content-") {
  const temporary = await mkdtemp(path.join(tmpdir(), prefix));
  const stagingRoot = path.join(temporary, "Tammy.app");
  await mkdir(path.join(stagingRoot, "Contents", "Resources"), { recursive: true });
  await mkdir(path.join(stagingRoot, "Contents", "MacOS"), { recursive: true });
  await mkdir(path.join(stagingRoot, "Contents", "Frameworks", "Versions", "A"), {
    recursive: true,
  });
  for (const [bundle, executable, bundleIdentifier] of [
    ["Tammy Helper.app", "Tammy Helper", "com.tammy.desktop.helper"],
    ["Tammy Helper (GPU).app", "Tammy Helper (GPU)", "com.tammy.desktop.helper.GPU"],
    ["Tammy Helper (Plugin).app", "Tammy Helper (Plugin)", "com.tammy.desktop.helper.Plugin"],
    ["Tammy Helper (Renderer).app", "Tammy Helper (Renderer)", "com.tammy.desktop.helper.Renderer"],
  ]) {
    const contents = path.join(stagingRoot, "Contents", "Frameworks", bundle, "Contents");
    await mkdir(path.join(contents, "MacOS"), { recursive: true });
    await writeFile(
      path.join(contents, "Info.plist"),
      bundlePlist({ bundleIdentifier, executable, helper: true }),
    );
    await writeFile(path.join(contents, "MacOS", executable), `${executable}\n`);
    await chmod(path.join(contents, "MacOS", executable), 0o755);
  }
  for (const relativePath of standardFrameworkExecutables) {
    const file = path.join(stagingRoot, ...relativePath.split("/"));
    await mkdir(path.dirname(file), { recursive: true });
    await writeFile(file, `${path.basename(file)}\n`);
    await chmod(file, 0o755);
  }
  await Promise.all([
    mkdir(path.join(stagingRoot, "Contents", "Resources", "build"), { recursive: true }),
    mkdir(path.join(stagingRoot, "Contents", "Resources", "core", "darwin-arm64"), {
      recursive: true,
    }),
    mkdir(path.join(stagingRoot, "Contents", "Resources", "sbr", "simulator"), {
      recursive: true,
    }),
    mkdir(path.join(stagingRoot, "Contents", "Resources", "sbr-helper", "darwin-arm64"), {
      recursive: true,
    }),
    mkdir(path.join(stagingRoot, "Contents", "Resources", "sqlcipher", "darwin-arm64", "include"), {
      recursive: true,
    }),
    mkdir(path.join(stagingRoot, "Contents", "Resources", "sqlcipher", "darwin-arm64", "lib"), {
      recursive: true,
    }),
  ]);
  await writeFile(
    path.join(stagingRoot, "Contents", "Info.plist"),
    bundlePlist({
      bundleIdentifier: "com.tammy.desktop",
      executable: "Tammy",
    }),
  );
  await writeFile(path.join(stagingRoot, "Contents", "MacOS", "Tammy"), "executable\n");
  await chmod(path.join(stagingRoot, "Contents", "MacOS", "Tammy"), 0o755);
  const resources = path.join(stagingRoot, "Contents", "Resources");
  await Promise.all([
    writeFile(path.join(resources, "PrivacyInfo.xcprivacy"), "privacy\n"),
    writeFile(path.join(resources, "app.asar"), "asar\n"),
    writeFile(path.join(resources, "build", "build-manifest.json"), "build\n"),
    writeFile(path.join(resources, "core", "darwin-arm64", "tammy-core"), "core\n"),
    writeFile(path.join(resources, "icon.icns"), "icon\n"),
    writeFile(path.join(resources, "sbr", "simulator", "sbr-profile-v1.json"), "profile\n"),
    writeFile(path.join(resources, "sbr", "simulator", "sbr-profile-v1.sig"), "signature\n"),
    writeFile(path.join(resources, "sbr-helper", "darwin-arm64", "tammy-sbr-helper"), "helper\n"),
    writeFile(path.join(resources, "sqlcipher", "LICENSE"), "license\n"),
    writeFile(path.join(resources, "sqlcipher", "VERSION"), "4.15.0\n"),
    writeFile(path.join(resources, "sqlcipher", "darwin-arm64", "HEADER_SHA256"), "hash\n"),
    writeFile(path.join(resources, "sqlcipher", "darwin-arm64", "LIBRARY_SHA256"), "hash\n"),
    writeFile(
      path.join(resources, "sqlcipher", "darwin-arm64", "include", "sqlite3.h"),
      "header\n",
    ),
    writeFile(
      path.join(resources, "sqlcipher", "darwin-arm64", "lib", "libsqlite3.a"),
      "library\n",
    ),
  ]);
  await Promise.all([
    chmod(path.join(resources, "core", "darwin-arm64", "tammy-core"), 0o755),
    chmod(path.join(resources, "sbr-helper", "darwin-arm64", "tammy-sbr-helper"), 0o755),
  ]);
  await writeFile(
    path.join(stagingRoot, "Contents", "Frameworks", "Versions", "A", "Electron"),
    "framework\n",
  );
  await symlink("Versions/A", path.join(stagingRoot, "Contents", "Frameworks", "Current"));
  return { stagingRoot, temporary };
}

test("creates and authenticates a strict timestamp-independent unsigned manifest", async () => {
  const { stagingRoot, temporary } = await createStaging();
  try {
    const manifest = await createUnsignedContentManifest(manifestInput(stagingRoot));
    assert.equal(validateUnsignedContentManifest(manifest), manifest);
    assert.deepEqual(
      manifest.entries.map(({ path: relativePath }) => relativePath),
      [...manifest.entries.map(({ path: relativePath }) => relativePath)].sort(),
    );
    assert.equal(JSON.stringify(manifest).includes("mtime"), false);
    assert.equal(JSON.stringify(manifest).includes("uid"), false);
    assert.deepEqual(
      manifest.entries.find(({ path: relativePath }) => relativePath.endsWith("/Current")),
      {
        executable: false,
        kind: "symlink",
        linkTarget: "Versions/A",
        path: "Contents/Frameworks/Current",
      },
    );
    assert.match(manifest.stagingDirectorySha256, /^[0-9a-f]{64}$/);
    assert.equal(await authenticateUnsignedContentManifest(stagingRoot, manifest), manifest);

    const now = new Date("2030-01-01T00:00:00.000Z");
    await utimes(path.join(stagingRoot, "Contents", "Resources", "app.asar"), now, now);
    assert.equal(
      (await createUnsignedContentManifest(manifestInput(stagingRoot))).stagingDirectorySha256,
      manifest.stagingDirectorySha256,
    );
    await writeFile(path.join(stagingRoot, "Contents", "Resources", "app.asar"), "changed\n");
    await assert.rejects(
      authenticateUnsignedContentManifest(stagingRoot, manifest),
      /MACOS_UNSIGNED_CONTENT_INVALID/,
    );
  } finally {
    await rm(temporary, { force: true, recursive: true });
  }
});

test("requires the complete MAS runtime inventory and bundle fact bindings", async () => {
  for (const relativePath of requiredRuntimePaths) {
    const { stagingRoot, temporary } = await createStaging("tammy-runtime-inventory-");
    try {
      await rm(path.join(stagingRoot, ...relativePath.split("/")));
      await assert.rejects(
        createUnsignedContentManifest(manifestInput(stagingRoot)),
        /MACOS_UNSIGNED_CONTENT_INVALID/,
      );
    } finally {
      await rm(temporary, { force: true, recursive: true });
    }
  }

  for (const changed of [
    bundlePlist({ bundleIdentifier: "com.example.wrong", executable: "Tammy" }),
    bundlePlist({ bundleIdentifier: "com.tammy.desktop", executable: "Not Tammy" }),
    bundlePlist({ bundleIdentifier: "com.tammy.desktop", executable: "Tammy" }).replace(
      "<string>42</string>",
      "<string>41</string>",
    ),
  ]) {
    const { stagingRoot, temporary } = await createStaging("tammy-bundle-facts-");
    try {
      await writeFile(path.join(stagingRoot, "Contents/Info.plist"), changed);
      await assert.rejects(
        createUnsignedContentManifest(manifestInput(stagingRoot)),
        /MACOS_UNSIGNED_CONTENT_INVALID/,
      );
    } finally {
      await rm(temporary, { force: true, recursive: true });
    }
  }

  const { stagingRoot, temporary } = await createStaging("tammy-helper-executable-facts-");
  try {
    const helperPlist = path.join(
      stagingRoot,
      "Contents/Frameworks/Tammy Helper (Renderer).app/Contents/Info.plist",
    );
    await writeFile(
      helperPlist,
      bundlePlist({
        bundleIdentifier: "com.tammy.desktop.helper.Renderer",
        executable: "Wrong Renderer",
        helper: true,
      }),
    );
    await assert.rejects(
      createUnsignedContentManifest(manifestInput(stagingRoot)),
      /MACOS_UNSIGNED_CONTENT_INVALID/,
    );
  } finally {
    await rm(temporary, { force: true, recursive: true });
  }
});

test("rejects unsafe symlinks and non-canonical manifest structure", async () => {
  for (const target of ["/private/tmp/outside", "../../outside", "missing"]) {
    const { stagingRoot, temporary } = await createStaging("tammy-unsigned-link-");
    try {
      await symlink(target, path.join(stagingRoot, "Contents", "Resources", "unsafe"));
      await assert.rejects(
        createUnsignedContentManifest(manifestInput(stagingRoot)),
        /MACOS_UNSIGNED_CONTENT_INVALID/,
      );
    } finally {
      await rm(temporary, { force: true, recursive: true });
    }
  }

  const { stagingRoot, temporary } = await createStaging("tammy-unsigned-shape-");
  try {
    const manifest = await createUnsignedContentManifest(manifestInput(stagingRoot));
    for (const changed of [
      { ...manifest, createdAt: "2026-08-30T00:00:00.000Z" },
      { ...manifest, entries: [...manifest.entries].reverse() },
      { ...manifest, entries: [...manifest.entries, manifest.entries[0]] },
      {
        ...manifest,
        entries: [{ ...manifest.entries[0], path: "../escape" }, ...manifest.entries.slice(1)],
      },
    ]) {
      assert.throws(
        () => validateUnsignedContentManifest(changed),
        /MACOS_UNSIGNED_CONTENT_INVALID/,
      );
    }
  } finally {
    await rm(temporary, { force: true, recursive: true });
  }
});

test("atomically writes a manifest and clones only authenticated staging bytes", async () => {
  const { stagingRoot, temporary } = await createStaging("tammy-unsigned-clone-");
  const destination = path.join(temporary, "development", "Tammy.app");
  const manifestPath = path.join(temporary, "unsigned-content.json");
  const concurrentManifestPath = path.join(temporary, "concurrent-unsigned-content.json");
  try {
    const manifest = await createUnsignedContentManifest(manifestInput(stagingRoot));
    await writeUnsignedContentManifest(manifestPath, manifest);
    assert.deepEqual(JSON.parse(await readFile(manifestPath, "utf8")), manifest);
    await assert.rejects(
      writeUnsignedContentManifest(manifestPath, manifest),
      /MACOS_UNSIGNED_CONTENT_DESTINATION_EXISTS/,
    );
    const concurrent = await Promise.allSettled([
      writeUnsignedContentManifest(concurrentManifestPath, manifest),
      writeUnsignedContentManifest(concurrentManifestPath, manifest),
    ]);
    assert.equal(concurrent.filter(({ status }) => status === "fulfilled").length, 1);
    assert.equal(concurrent.filter(({ status }) => status === "rejected").length, 1);
    assert.match(
      concurrent.find(({ status }) => status === "rejected").reason.message,
      /MACOS_UNSIGNED_CONTENT_DESTINATION_EXISTS/,
    );
    await cloneUnsignedStaging({ destination, manifest, source: stagingRoot });
    assert.equal(await authenticateUnsignedContentManifest(destination, manifest), manifest);
    assert.equal(
      (await lstat(path.join(destination, "Contents", "Frameworks", "Current"))).isSymbolicLink(),
      true,
    );
    await assert.rejects(
      cloneUnsignedStaging({ destination, manifest, source: stagingRoot }),
      /MACOS_UNSIGNED_CONTENT_DESTINATION_EXISTS/,
    );
  } finally {
    await rm(temporary, { force: true, recursive: true });
  }
});

test("compares signed copies with only explicit signature and profile differences", async () => {
  const { stagingRoot, temporary } = await createStaging("tammy-signed-equivalence-");
  const developmentApp = path.join(temporary, "development", "Tammy.app");
  const distributionApp = path.join(temporary, "distribution", "Tammy.app");
  try {
    const normalizedPaths = ["Contents/Resources/build-manifest.json"];
    await writeFile(path.join(stagingRoot, normalizedPaths[0]), "unsigned mode facts\n");
    const manifest = await createUnsignedContentManifest(manifestInput(stagingRoot));
    await cloneUnsignedStaging({ destination: developmentApp, manifest, source: stagingRoot });
    await cloneUnsignedStaging({ destination: distributionApp, manifest, source: stagingRoot });
    for (const [app, value] of [
      [developmentApp, "development-signature"],
      [distributionApp, "distribution-signature"],
    ]) {
      await mkdir(path.join(app, "Contents", "_CodeSignature"), { recursive: true });
      await writeFile(path.join(app, "Contents", "_CodeSignature", "CodeResources"), value);
      await writeFile(path.join(app, "Contents", "embedded.provisionprofile"), value);
    }
    const signedPaths = ["Contents/MacOS/Tammy"];
    await writeFile(path.join(developmentApp, signedPaths[0]), "development-signed\n");
    await writeFile(path.join(distributionApp, signedPaths[0]), "distribution-signed\n");
    await writeFile(path.join(developmentApp, normalizedPaths[0]), "development mode facts\n");
    await writeFile(path.join(distributionApp, normalizedPaths[0]), "distribution mode facts\n");
    const baseSemantics = {
      buildNumber: "42",
      bundleIdentifier: "com.tammy.desktop",
      entitlementIntent: ["app-sandbox", "network-client"],
      marketingVersion: "0.1.0",
      publicLinks: manifest.publicLinks,
      unsignedSha256: manifest.entries.find(({ path: value }) => value === signedPaths[0]).sha256,
    };
    await assert.doesNotReject(
      compareSignedCopies({
        developmentApp,
        distributionApp,
        inspectSignedFile: async (_app, _relativePath, mode) => ({
          ...baseSemantics,
          signingMode: mode,
        }),
        normalizeFile: async () => ({ payload: "same authenticated intent" }),
        normalizedPaths,
        signedPaths,
        unsignedManifest: manifest,
      }),
    );

    await assert.rejects(
      compareSignedCopies({
        developmentApp,
        distributionApp,
        inspectSignedFile: async (_app, _relativePath, mode) => ({
          ...baseSemantics,
          signingMode: mode,
        }),
        normalizeFile: async (_app, _relativePath, mode) => ({ mode }),
        normalizedPaths,
        signedPaths,
        unsignedManifest: manifest,
      }),
      /MACOS_SIGNED_COPY_MISMATCH/,
    );

    const fakeSignature = path.join(
      distributionApp,
      "Contents",
      "Resources",
      "_CodeSignature",
      "CodeResources",
    );
    await mkdir(path.dirname(fakeSignature), { recursive: true });
    await writeFile(fakeSignature, "not a code-bundle signature\n");
    await assert.rejects(
      compareSignedCopies({
        developmentApp,
        distributionApp,
        inspectSignedFile: async (_app, _relativePath, mode) => ({
          ...baseSemantics,
          signingMode: mode,
        }),
        normalizeFile: async () => ({ payload: "same authenticated intent" }),
        normalizedPaths,
        signedPaths,
        unsignedManifest: manifest,
      }),
      /MACOS_SIGNED_COPY_MISMATCH/,
    );
    await rm(path.dirname(fakeSignature), { force: true, recursive: true });

    await assert.doesNotReject(
      validateSignedCopyAgainstUnsignedManifest({
        app: developmentApp,
        inspectSignedFile: async (_app, _relativePath, mode) => ({
          ...baseSemantics,
          signingMode: mode,
        }),
        mode: "development",
        normalizeFile: async () => ({ payload: "authenticated mode-bound facts" }),
        normalizedPaths,
        signedPaths,
        unsignedManifest: manifest,
      }),
    );

    await writeFile(path.join(distributionApp, "Contents", "Resources", "app.asar"), "drift\n");
    await assert.rejects(
      compareSignedCopies({
        developmentApp,
        distributionApp,
        inspectSignedFile: async (_app, _relativePath, mode) => ({
          ...baseSemantics,
          signingMode: mode,
        }),
        normalizeFile: async () => ({ payload: "same authenticated intent" }),
        normalizedPaths,
        signedPaths,
        unsignedManifest: manifest,
      }),
      /MACOS_SIGNED_COPY_MISMATCH/,
    );

    await writeFile(path.join(developmentApp, "Contents", "Resources", "app.asar"), "drift\n");
    await assert.rejects(
      validateSignedCopyAgainstUnsignedManifest({
        app: developmentApp,
        inspectSignedFile: async (_app, _relativePath, mode) => ({
          ...baseSemantics,
          signingMode: mode,
        }),
        mode: "development",
        normalizeFile: async () => ({ payload: "authenticated mode-bound facts" }),
        normalizedPaths,
        signedPaths,
        unsignedManifest: manifest,
      }),
      /MACOS_SIGNED_COPY_MISMATCH/,
    );
  } finally {
    await rm(temporary, { force: true, recursive: true });
  }
});
