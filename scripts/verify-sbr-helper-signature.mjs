import { execFile as nodeExecFile } from "node:child_process";
import { lstat, readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { parseMacOSRepositoryPlist } from "./check-macos-store.mjs";
import { authenticateSbrProfileBytes } from "./sbr-profile-schema.mjs";

const execFile = promisify(nodeExecFile);
const IDENTIFIER = "com.tammy.desktop.sbr-helper";

function exactDetail(text, name) {
  const matches = [...text.matchAll(new RegExp(`^${name}=([^\\r\\n]+)$`, "gm"))];
  if (matches.length !== 1) throw new Error("SBR_HELPER_SIGNATURE_INVALID");
  return matches[0][1];
}

async function productionRunner(command, args, options) {
  return execFile(command, args, {
    ...options,
    encoding: "utf8",
    maxBuffer: 1024 * 1024,
    timeout: 10_000,
  });
}

export async function verifySbrHelperSignature({
  appBundle,
  mode,
  teamID,
  commandRunner = productionRunner,
  verifyProfile = true,
}) {
  if (!path.isAbsolute(appBundle) || !["ordinary", "mas"].includes(mode))
    throw new Error("SBR_HELPER_SIGNATURE_INVALID");
  const helper = path.join(
    appBundle,
    "Contents/Resources/sbr-helper/darwin-arm64/tammy-sbr-helper",
  );
  const stats = await lstat(helper).catch(() => null);
  if (!stats?.isFile() || stats.isSymbolicLink()) throw new Error("SBR_HELPER_SIGNATURE_INVALID");
  try {
    const commandOptions = {
      env: { LANG: "C", LC_ALL: "C", PATH: "/usr/bin:/bin" },
    };
    await commandRunner(
      "/usr/bin/codesign",
      ["--verify", "--strict", "--verbose=2", helper],
      commandOptions,
    );
    const details = await commandRunner("/usr/bin/codesign", ["-dvvv", helper], commandOptions);
    const expectedRequirement =
      mode === "ordinary"
        ? `=identifier "${IDENTIFIER}"`
        : `=identifier "${IDENTIFIER}" and anchor apple generic and certificate leaf[subject.OU] = "${teamID}"`;
    await commandRunner(
      "/usr/bin/codesign",
      ["--verify", "--strict", "-R", expectedRequirement, helper],
      commandOptions,
    );
    const entitlements = await commandRunner(
      "/usr/bin/codesign",
      ["-d", "--entitlements", ":-", "--xml", helper],
      commandOptions,
    ).catch(() => ({ stdout: "", stderr: "" }));
    const detailText = `${details.stdout ?? ""}${details.stderr ?? ""}`;
    const entitlementText = `${entitlements.stdout ?? ""}`;
    if (exactDetail(detailText, "Identifier") !== IDENTIFIER) throw new Error();
    if (mode === "ordinary") {
      if (
        exactDetail(detailText, "Signature") !== "adhoc" ||
        exactDetail(detailText, "TeamIdentifier") !== "not set" ||
        entitlementText.trim() !== ""
      )
        throw new Error();
    } else {
      if (
        !teamID ||
        !/^[A-Z0-9]{10}$/.test(teamID) ||
        exactDetail(detailText, "TeamIdentifier") !== teamID
      )
        throw new Error();
      const parsed = parseMacOSRepositoryPlist(entitlementText);
      const expected = {
        "com.apple.security.app-sandbox": true,
        "com.apple.security.files.user-selected.read-only": true,
        "com.apple.security.application-groups": [`${teamID}.com.tammy.desktop`],
        "keychain-access-groups": [`${teamID}.com.tammy.desktop.sbr`],
      };
      if (JSON.stringify(parsed) !== JSON.stringify(expected)) throw new Error();
    }
    if (verifyProfile) {
      const manifest = JSON.parse(
        await readFile(
          path.join(appBundle, "Contents/Resources/build/build-manifest.json"),
          "utf8",
        ),
      );
      const { createHash } = await import("node:crypto");
      const profilePath = path.join(
        appBundle,
        "Contents/Resources/sbr/simulator/sbr-profile-v1.json",
      );
      const signaturePath = path.join(
        appBundle,
        "Contents/Resources/sbr/simulator/sbr-profile-v1.sig",
      );
      const [helperBytes, profileBytes, signatureBytes, publicKey] = await Promise.all([
        readFile(helper),
        readFile(profilePath),
        readFile(signaturePath),
        readFile(
          path.resolve(import.meta.dirname, "../config/sbr/simulator/profile-public-key.pem"),
        ),
      ]);
      const hash = (bytes) => createHash("sha256").update(bytes).digest("hex");
      const profile = authenticateSbrProfileBytes({ profileBytes, publicKey, signatureBytes });
      if (
        manifest.sbr?.helper_sha256 !== hash(helperBytes) ||
        profile.helper_sha256 !== hash(helperBytes) ||
        manifest.sbr?.profile_sha256 !== hash(profileBytes) ||
        manifest.sbr?.profile_signature_sha256 !== hash(signatureBytes)
      )
        throw new Error();
    }
    return { helper, identifier: IDENTIFIER, mode };
  } catch {
    throw new Error("SBR_HELPER_SIGNATURE_INVALID");
  }
}

async function main() {
  const argv = process.argv.slice(2);
  const mode = argv.includes("--mas") ? "mas" : argv.includes("--ordinary") ? "ordinary" : null;
  if (!mode || argv.length !== 1) throw new Error("SBR_HELPER_SIGNATURE_ARGUMENTS_INVALID");
  const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../apps/desktop");
  const appBundle = path.join(
    desktopRoot,
    "out",
    `Tammy-${mode === "mas" ? "mas" : "darwin"}-arm64`,
    "Tammy.app",
  );
  await verifySbrHelperSignature({ appBundle, mode, teamID: process.env.TAMMY_MACOS_TEAM_ID });
  process.stdout.write(`SBR_HELPER_SIGNATURE_VERIFIED:${mode}\n`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url))
  main().catch((error) => {
    process.stderr.write(
      `${error instanceof Error ? error.message : "SBR_HELPER_SIGNATURE_INVALID"}\n`,
    );
    process.exitCode = 1;
  });
