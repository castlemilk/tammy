import { lstat, readFile, realpath, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

const expectedInventory = {
  schemaVersion: 1,
  containerRelativePath: "Library/Containers/com.tammy.desktop",
  groupContainerSuffix: "com.tammy.desktop",
  keychainServices: [
    "com.tammy.workspace",
    "com.tammy.attempt-journal-anchor.v1",
    "com.tammy.audit-mirror",
    "com.tammy.sbr.production",
  ],
};

const inventoryError = (message) => new Error(`MACOS_DATA_REMOVAL_INVENTORY_INVALID: ${message}`);

function hasExactKeys(value, keys) {
  return (
    value !== null &&
    typeof value === "object" &&
    !Array.isArray(value) &&
    Object.keys(value).length === keys.length &&
    keys.every((key) => Object.hasOwn(value, key))
  );
}

export function validateDataRemovalInventory(value) {
  const keys = [
    "schemaVersion",
    "containerRelativePath",
    "groupContainerSuffix",
    "keychainServices",
  ];
  if (!hasExactKeys(value, keys)) throw inventoryError("unexpected fields");
  if (value.schemaVersion !== expectedInventory.schemaVersion)
    throw inventoryError("schemaVersion drift");
  if (value.containerRelativePath !== expectedInventory.containerRelativePath)
    throw inventoryError("container path drift");
  if (value.groupContainerSuffix !== expectedInventory.groupContainerSuffix)
    throw inventoryError("group-container suffix drift");
  if (
    !Array.isArray(value.keychainServices) ||
    value.keychainServices.length !== expectedInventory.keychainServices.length ||
    value.keychainServices.some(
      (service, index) => service !== expectedInventory.keychainServices[index],
    )
  ) {
    throw inventoryError("Keychain service drift");
  }
  return value;
}

const inventoryUrl = new URL("../apps/desktop/release/macos/data-removal.json", import.meta.url);
const loadedInventory = validateDataRemovalInventory(
  JSON.parse(await readFile(inventoryUrl, "utf8")),
);
export const DATA_REMOVAL_INVENTORY = Object.freeze({
  ...loadedInventory,
  keychainServices: Object.freeze([...loadedInventory.keychainServices]),
});

export function validateIsolatedHome(isolatedHome) {
  if (typeof isolatedHome !== "string" || !path.isAbsolute(isolatedHome)) {
    throw new Error("A safe absolute isolated home is required");
  }
  if (
    ["*", "?", "[", "]", "{", "}"].some((character) => isolatedHome.includes(character)) ||
    /[!+@]\(/.test(isolatedHome)
  ) {
    throw new Error("The isolated home must not contain glob syntax");
  }
  const normalized = path.resolve(isolatedHome);
  if (normalized === path.parse(normalized).root || normalized === path.resolve(os.homedir())) {
    throw new Error("The isolated home cannot be a filesystem root or the real user home");
  }
  return normalized;
}

export function validateTeamID(teamID) {
  if (typeof teamID !== "string" || !/^[A-Z0-9]{10}$/.test(teamID)) {
    throw new Error("Team ID must be exactly 10 uppercase ASCII letters or digits");
  }
  return teamID;
}

export function validateKeychainService(service) {
  if (!DATA_REMOVAL_INVENTORY.keychainServices.includes(service)) {
    throw new Error("Keychain service is not in the exact production removal inventory");
  }
  return service;
}

function isContained(home, target) {
  const relative = path.relative(home, target);
  return (
    relative !== "" &&
    relative !== ".." &&
    !relative.startsWith(`..${path.sep}`) &&
    !path.isAbsolute(relative)
  );
}

async function validateTarget(isolatedHomeRealPath, target) {
  if (!isContained(isolatedHomeRealPath, target))
    throw new Error("Removal target escapes the isolated home");
  let metadata;
  try {
    metadata = await lstat(target);
  } catch (error) {
    if (error?.code === "ENOENT") return;
    throw error;
  }
  if (metadata.isSymbolicLink()) throw new Error("Removal target must not be a symbolic link");
  const targetRealPath = await realpath(target);
  if (!isContained(isolatedHomeRealPath, targetRealPath))
    throw new Error("Removal target resolves outside the isolated home");
}

export async function removeTammyData({ isolatedHome, teamID, keychain } = {}) {
  const normalizedHome = validateIsolatedHome(isolatedHome);
  const validatedTeamID = validateTeamID(teamID);
  if (!keychain || typeof keychain.deleteGenericPasswords !== "function") {
    throw new Error("An injected Keychain adapter is required");
  }
  const homeMetadata = await lstat(normalizedHome);
  if (homeMetadata.isSymbolicLink() || !homeMetadata.isDirectory()) {
    throw new Error("The isolated home must be a real directory, not a symbolic link");
  }
  const isolatedHomeRealPath = await realpath(normalizedHome);
  const targets = [
    path.join(isolatedHomeRealPath, DATA_REMOVAL_INVENTORY.containerRelativePath),
    path.join(
      isolatedHomeRealPath,
      "Library/Group Containers",
      `${validatedTeamID}.${DATA_REMOVAL_INVENTORY.groupContainerSuffix}`,
    ),
  ];

  await Promise.all(targets.map((target) => validateTarget(isolatedHomeRealPath, target)));
  for (const target of targets) await rm(target, { recursive: true, force: true });
  for (const service of DATA_REMOVAL_INVENTORY.keychainServices) {
    await keychain.deleteGenericPasswords(validateKeychainService(service));
  }

  return {
    removedPaths: targets,
    removedKeychainServices: [...DATA_REMOVAL_INVENTORY.keychainServices],
  };
}
