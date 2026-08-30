import assert from "node:assert/strict";
import { execFile, execFileSync } from "node:child_process";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { promisify } from "node:util";

import { DATA_REMOVAL_INVENTORY, removeTammyData } from "./macos-data-removal.mjs";

const execFileAsync = promisify(execFile);
const macOSMajor =
  process.platform === "darwin"
    ? Number.parseInt(
        execFileSync("/usr/bin/sw_vers", ["-productVersion"], { encoding: "utf8" }).split(".")[0],
        10,
      )
    : 0;
const skipReason =
  process.platform !== "darwin" || macOSMajor < 14 ? "REQUIRES_MACOS_14_KEYCHAIN" : false;

test("removes Tammy entries from only an explicit temporary macOS Keychain", {
  skip: skipReason,
}, async (context) => {
  const isolatedHome = await mkdtemp(
    path.join(process.env.TMPDIR ?? os.tmpdir(), "tammy-keychain-removal-"),
  );
  const keychainPath = path.join(isolatedHome, "tammy-removal-test.keychain-db");
  const keychainPassword = "tammy-temporary-keychain-password";
  let keychainCreated = false;

  const security = async (...args) => {
    assert.ok(
      !["default-keychain", "list-keychains"].includes(args[0]),
      "the integration test must not read or change the user's default or search-list Keychains",
    );
    assert.doesNotMatch(args.join(" "), /login(?:\.keychain(?:-db)?)?/i);
    assert.ok(
      args.includes(keychainPath),
      "every security invocation must identify the temporary Keychain explicitly",
    );
    return execFileAsync("/usr/bin/security", args, { encoding: "utf8" });
  };
  context.after(() => rm(isolatedHome, { recursive: true, force: true }));

  try {
    await security("create-keychain", "-p", keychainPassword, keychainPath);
    keychainCreated = true;
    await security("unlock-keychain", "-p", keychainPassword, keychainPath);
    for (const service of [...DATA_REMOVAL_INVENTORY.keychainServices, "com.example.sentinel"]) {
      const accounts =
        service === "com.example.sentinel"
          ? ["tammy-removal-test"]
          : ["tammy-removal-test-a", "tammy-removal-test-b"];
      for (const account of accounts) {
        await security(
          "add-generic-password",
          "-a",
          account,
          "-s",
          service,
          "-w",
          `secret-for-${service}`,
          keychainPath,
        );
      }
    }

    await Promise.all([
      mkdir(path.join(isolatedHome, DATA_REMOVAL_INVENTORY.containerRelativePath), {
        recursive: true,
      }),
      mkdir(path.join(isolatedHome, "Library/Group Containers/TEAM123456.com.tammy.desktop"), {
        recursive: true,
      }),
    ]);

    const keychain = {
      async deleteGenericPasswords(service) {
        while (true) {
          try {
            await security("find-generic-password", "-s", service, keychainPath);
          } catch (error) {
            if (error?.code === 44) return;
            throw error;
          }
          await security("delete-generic-password", "-s", service, keychainPath);
        }
      },
    };
    await removeTammyData({ isolatedHome, teamID: "TEAM123456", keychain });

    for (const service of DATA_REMOVAL_INVENTORY.keychainServices) {
      await assert.rejects(
        () => security("find-generic-password", "-s", service, "-w", keychainPath),
        (error) => error?.code === 44,
      );
    }
    const sentinel = await security(
      "find-generic-password",
      "-s",
      "com.example.sentinel",
      "-w",
      keychainPath,
    );
    assert.equal(sentinel.stdout.trim(), "secret-for-com.example.sentinel");
  } finally {
    if (keychainCreated) await security("delete-keychain", keychainPath).catch(() => {});
  }
});
