import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import { verifySbrHelperSignature } from "./verify-sbr-helper-signature.mjs";

test("ordinary verification requires exact helper identity and no entitlements", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-sbr-signature-"));
  try {
    const helper = path.join(
      root,
      "Tammy.app/Contents/Resources/sbr-helper/darwin-arm64/tammy-sbr-helper",
    );
    await mkdir(path.dirname(helper), { recursive: true });
    await writeFile(helper, "helper");
    const commands = [];
    const commandRunner = async (command, args, options) => {
      commands.push([command, args]);
      assert.equal(options.env.LC_ALL, "C");
      assert.equal(options.env.LANG, "C");
      if (args.includes("-dvvv"))
        return {
          stderr:
            "Identifier=com.tammy.desktop.sbr-helper\nTeamIdentifier=not set\nSignature=adhoc\n",
          stdout: "",
        };
      if (args.includes("-R")) return { stderr: "", stdout: "" };
      if (args.includes("--entitlements")) return { stderr: "", stdout: "" };
      return { stderr: "", stdout: "" };
    };
    const result = await verifySbrHelperSignature({
      appBundle: path.join(root, "Tammy.app"),
      mode: "ordinary",
      commandRunner,
      verifyProfile: false,
    });
    assert.equal(result.identifier, "com.tammy.desktop.sbr-helper");
    assert.equal(commands.length, 4);
    assert.deepEqual(commands.find(([, args]) => args.includes("-R"))?.[1].slice(0, 4), [
      "--verify",
      "--strict",
      "-R",
      '=identifier "com.tammy.desktop.sbr-helper"',
    ]);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("MAS verification rejects network entitlements and wrong Team ID", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-sbr-signature-"));
  try {
    const helper = path.join(
      root,
      "Tammy.app/Contents/Resources/sbr-helper/darwin-arm64/tammy-sbr-helper",
    );
    await mkdir(path.dirname(helper), { recursive: true });
    await writeFile(helper, "helper");
    const runner = async (_command, args) => {
      if (args.includes("-dvvv"))
        return {
          stderr: "Identifier=com.tammy.desktop.sbr-helper\nTeamIdentifier=OTHER12345\n",
          stdout: "",
        };
      if (args.includes("-R")) return { stderr: "", stdout: "" };
      if (args.includes("--entitlements"))
        return {
          stdout: "<plist><dict><key>com.apple.security.network.client</key><true/></dict></plist>",
          stderr: "",
        };
      return { stdout: "", stderr: "" };
    };
    await assert.rejects(
      verifySbrHelperSignature({
        appBundle: path.join(root, "Tammy.app"),
        mode: "mas",
        teamID: "ABCDE12345",
        commandRunner: runner,
        verifyProfile: false,
      }),
      /SBR_HELPER_SIGNATURE_INVALID/,
    );
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("MAS verification accepts only the expanded pinned Team authority", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-sbr-signature-"));
  try {
    const helper = path.join(
      root,
      "Tammy.app/Contents/Resources/sbr-helper/darwin-arm64/tammy-sbr-helper",
    );
    await mkdir(path.dirname(helper), { recursive: true });
    await writeFile(helper, "helper");
    const runner = async (_command, args) => {
      if (args.includes("-dvvv"))
        return {
          stderr: "Identifier=com.tammy.desktop.sbr-helper\nTeamIdentifier=ABCDE12345\n",
          stdout: "",
        };
      if (args.includes("-R")) return { stderr: "", stdout: "" };
      if (args.includes("--entitlements"))
        return {
          stderr: "",
          stdout:
            '<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>com.apple.security.app-sandbox</key><true/><key>com.apple.security.files.user-selected.read-only</key><true/><key>com.apple.security.application-groups</key><array><string>ABCDE12345.com.tammy.desktop</string></array><key>keychain-access-groups</key><array><string>ABCDE12345.com.tammy.desktop.sbr</string></array></dict></plist>',
        };
      return { stderr: "", stdout: "" };
    };
    const result = await verifySbrHelperSignature({
      appBundle: path.join(root, "Tammy.app"),
      commandRunner: runner,
      mode: "mas",
      teamID: "ABCDE12345",
      verifyProfile: false,
    });
    assert.equal(result.mode, "mas");
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("MAS verifier structurally rejects hostile entitlement values and extras", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-sbr-signature-"));
  try {
    const helper = path.join(
      root,
      "Tammy.app/Contents/Resources/sbr-helper/darwin-arm64/tammy-sbr-helper",
    );
    await mkdir(path.dirname(helper), { recursive: true });
    await writeFile(helper, "helper");
    const baseline = `<key>com.apple.security.app-sandbox</key><true/><key>com.apple.security.files.user-selected.read-only</key><true/><key>com.apple.security.application-groups</key><array><string>ABCDE12345.com.tammy.desktop</string></array><key>keychain-access-groups</key><array><string>ABCDE12345.com.tammy.desktop.sbr</string></array>`;
    for (const entitlementsBody of [
      baseline.replace("<true/>", "<false/>"),
      `${baseline}<key>com.apple.security.cs.allow-jit</key><true/>`,
      `${baseline}<key>com.apple.security.network.client</key><true/>`,
      baseline.replace(
        "</array><key>keychain-access-groups",
        "<string>ABCDE12345.com.other</string></array><key>keychain-access-groups",
      ),
    ]) {
      const runner = async (_command, args, options) => {
        assert.equal(options.env.LC_ALL, "C");
        assert.equal(options.env.LANG, "C");
        if (args.includes("-dvvv"))
          return {
            stderr: "Identifier=com.tammy.desktop.sbr-helper\nTeamIdentifier=ABCDE12345\n",
            stdout: "",
          };
        if (args.includes("-R")) return { stderr: "", stdout: "" };
        if (args.includes("--entitlements"))
          return {
            stderr: "",
            stdout: `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict>${entitlementsBody}</dict></plist>`,
          };
        return { stderr: "", stdout: "" };
      };
      await assert.rejects(
        verifySbrHelperSignature({
          appBundle: path.join(root, "Tammy.app"),
          commandRunner: runner,
          mode: "mas",
          teamID: "ABCDE12345",
          verifyProfile: false,
        }),
        /SBR_HELPER_SIGNATURE_INVALID/,
      );
    }
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});

test("misleading requirement text cannot replace codesign requirement evaluation", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-sbr-signature-"));
  try {
    const helper = path.join(
      root,
      "Tammy.app/Contents/Resources/sbr-helper/darwin-arm64/tammy-sbr-helper",
    );
    await mkdir(path.dirname(helper), { recursive: true });
    await writeFile(helper, "helper");
    let evaluated = false;
    const runner = async (_command, args) => {
      if (args.includes("-dvvv"))
        return {
          stderr:
            'Identifier=com.tammy.desktop.sbr-helper\nTeamIdentifier=ABCDE12345\ndesignated => anchor apple or identifier "com.tammy.desktop.sbr-helper"\n',
          stdout: "",
        };
      if (args.includes("-R")) {
        evaluated = true;
        throw new Error("requirement rejected");
      }
      return { stderr: "", stdout: "" };
    };
    await assert.rejects(
      verifySbrHelperSignature({
        appBundle: path.join(root, "Tammy.app"),
        commandRunner: runner,
        mode: "mas",
        teamID: "ABCDE12345",
        verifyProfile: false,
      }),
      /SBR_HELPER_SIGNATURE_INVALID/,
    );
    assert.equal(evaluated, true);
  } finally {
    await rm(root, { force: true, recursive: true });
  }
});
