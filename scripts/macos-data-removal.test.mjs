import assert from "node:assert/strict";
import { lstat, mkdir, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import {
  DATA_REMOVAL_INVENTORY,
  removeTammyData,
  validateDataRemovalInventory,
  validateIsolatedHome,
  validateKeychainService,
  validateTeamID,
} from "./macos-data-removal.mjs";

const expectedServices = [
  "com.tammy.workspace",
  "com.tammy.attempt-journal-anchor.v1",
  "com.tammy.audit-mirror",
  "com.tammy.sbr.production",
];

async function pathExists(target) {
  try {
    await lstat(target);
    return true;
  } catch (error) {
    if (error?.code === "ENOENT") return false;
    throw error;
  }
}

async function createFixture(context) {
  const isolatedHome = await mkdtemp(
    path.join(process.env.TMPDIR ?? os.tmpdir(), "tammy-removal-"),
  );
  context.after(() => rm(isolatedHome, { recursive: true, force: true }));

  const targets = {
    tammyContainer: path.join(isolatedHome, "Library/Containers/com.tammy.desktop"),
    containerSentinel: path.join(isolatedHome, "Library/Containers/com.example.sentinel"),
    tammyGroup: path.join(isolatedHome, "Library/Group Containers/TEAM123456.com.tammy.desktop"),
    groupSentinel: path.join(
      isolatedHome,
      "Library/Group Containers/TEAM123456.com.example.sentinel",
    ),
  };
  await Promise.all([
    mkdir(path.join(targets.tammyContainer, "Data/Library/Application Support/Tammy"), {
      recursive: true,
    }),
    mkdir(targets.containerSentinel, { recursive: true }),
    mkdir(targets.tammyGroup, { recursive: true }),
    mkdir(targets.groupSentinel, { recursive: true }),
  ]);
  await Promise.all([
    writeFile(
      path.join(targets.tammyContainer, "Data/Library/Application Support/Tammy/ledger.db"),
      "tammy",
    ),
    writeFile(path.join(targets.containerSentinel, "sentinel.txt"), "container sentinel"),
    writeFile(path.join(targets.tammyGroup, "tammy.txt"), "tammy"),
    writeFile(path.join(targets.groupSentinel, "sentinel.txt"), "group sentinel"),
  ]);
  return { isolatedHome, targets };
}

test("owns the exact production local-data inventory", () => {
  assert.deepEqual(DATA_REMOVAL_INVENTORY, {
    schemaVersion: 1,
    containerRelativePath: "Library/Containers/com.tammy.desktop",
    groupContainerSuffix: "com.tammy.desktop",
    keychainServices: expectedServices,
  });
  assert.deepEqual(validateDataRemovalInventory(DATA_REMOVAL_INVENTORY), DATA_REMOVAL_INVENTORY);
});

test("published inventory cannot be mutated into a broader removal boundary", () => {
  assert.equal(Object.isFrozen(DATA_REMOVAL_INVENTORY), true);
  assert.equal(Object.isFrozen(DATA_REMOVAL_INVENTORY.keychainServices), true);
  assert.throws(
    () => DATA_REMOVAL_INVENTORY.keychainServices.push("com.example.sentinel"),
    TypeError,
  );
});

test("removes only Tammy filesystem and Keychain data from an isolated home", async (context) => {
  const { isolatedHome, targets } = await createFixture(context);
  const entries = new Map([
    ...expectedServices.map((service) => [service, ["tammy-secret"]]),
    ["com.example.sentinel", ["sentinel-secret"]],
  ]);
  const deletedServices = [];
  const keychain = {
    async deleteGenericPasswords(service) {
      deletedServices.push(service);
      entries.delete(service);
    },
  };

  const result = await removeTammyData({ isolatedHome, teamID: "TEAM123456", keychain });

  assert.deepEqual(result, {
    removedPaths: [targets.tammyContainer, targets.tammyGroup],
    removedKeychainServices: expectedServices,
  });
  assert.deepEqual(deletedServices, expectedServices);
  assert.equal(await pathExists(targets.tammyContainer), false);
  assert.equal(await pathExists(targets.tammyGroup), false);
  assert.equal(
    await readFile(path.join(targets.containerSentinel, "sentinel.txt"), "utf8"),
    "container sentinel",
  );
  assert.equal(
    await readFile(path.join(targets.groupSentinel, "sentinel.txt"), "utf8"),
    "group sentinel",
  );
  assert.deepEqual(entries, new Map([["com.example.sentinel", ["sentinel-secret"]]]));
});

test("refuses unsafe isolated-home and Team ID inputs", async () => {
  for (const unsafeHome of ["/", os.homedir(), "relative/home"]) {
    await assert.rejects(
      () =>
        removeTammyData({
          isolatedHome: unsafeHome,
          teamID: "TEAM123456",
          keychain: { async deleteGenericPasswords() {} },
        }),
      /isolated home/i,
    );
    assert.throws(() => validateIsolatedHome(unsafeHome), /isolated home/i);
  }
  for (const invalidTeamID of [
    "",
    "team123456",
    "TEAM-12345",
    "TEAM12345",
    "TEAM1234567",
    "../ESCAPE1",
  ]) {
    assert.throws(() => validateTeamID(invalidTeamID), /Team ID/i);
  }
});

for (const targetName of ["tammyContainer", "tammyGroup"]) {
  test(`refuses a symlinked ${targetName} before deleting any data`, async (context) => {
    const { isolatedHome, targets } = await createFixture(context);
    const outside = await mkdtemp(path.join(process.env.TMPDIR ?? os.tmpdir(), "tammy-outside-"));
    context.after(() => rm(outside, { recursive: true, force: true }));
    await rm(targets[targetName], { recursive: true });
    await symlink(outside, targets[targetName], "dir");
    let keychainCalls = 0;

    await assert.rejects(
      () =>
        removeTammyData({
          isolatedHome,
          teamID: "TEAM123456",
          keychain: {
            async deleteGenericPasswords() {
              keychainCalls += 1;
            },
          },
        }),
      /symbolic link/i,
    );

    const untouchedTarget =
      targetName === "tammyContainer" ? targets.tammyGroup : targets.tammyContainer;
    assert.equal(
      await pathExists(untouchedTarget),
      true,
      "all paths validate before deletion starts",
    );
    assert.equal(keychainCalls, 0);
  });
}

test("refuses a target whose ancestor resolves outside the isolated home", async (context) => {
  const { isolatedHome, targets } = await createFixture(context);
  const outside = await mkdtemp(path.join(process.env.TMPDIR ?? os.tmpdir(), "tammy-outside-"));
  context.after(() => rm(outside, { recursive: true, force: true }));
  const containersDirectory = path.join(isolatedHome, "Library/Containers");
  await rm(containersDirectory, { recursive: true });
  await mkdir(path.join(outside, "com.tammy.desktop"), { recursive: true });
  await symlink(outside, containersDirectory, "dir");

  await assert.rejects(
    () =>
      removeTammyData({
        isolatedHome,
        teamID: "TEAM123456",
        keychain: { async deleteGenericPasswords() {} },
      }),
    /outside the isolated home/i,
  );
  assert.equal(await pathExists(targets.tammyGroup), true);
});

test("refuses unknown and development Keychain services", () => {
  for (const service of [
    "com.example.sentinel",
    "com.tammy.sbr.development",
    "com.tammy.sbr.simulator",
    "com.tammy.workspace.dev",
  ]) {
    assert.throws(() => validateKeychainService(service), /Keychain service/i);
  }
  for (const service of expectedServices) assert.equal(validateKeychainService(service), service);
});

test("owner has no executable CLI or shell deletion path", async () => {
  const source = await readFile(new URL("./macos-data-removal.mjs", import.meta.url), "utf8");
  assert.doesNotMatch(
    source,
    /import\.meta\.main|child_process|execFile|spawn\(|\/usr\/bin\/security|rmSync/,
  );
});
