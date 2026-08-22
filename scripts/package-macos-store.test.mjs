import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, unlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import * as packageMacOSStore from "./package-macos-store.mjs";
import { SBR_BUILD_LOCK_ENV } from "./sbr-build-lock.mjs";

const { createMacOSStoreBuildPlan } = packageMacOSStore;

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function environment(mode = "distribution") {
  return {
    TAMMY_MACOS_BUILD_NUMBER: "42",
    TAMMY_MACOS_EXPORT_COMPLIANCE: "exempt",
    ...(mode === "distribution"
      ? {
          TAMMY_MACOS_INSTALLER_IDENTITY:
            "3rd Party Mac Developer Installer: Tammy Pty Ltd (ABCDE12345)",
        }
      : {}),
    TAMMY_MACOS_PROVISIONING_PROFILE: "/private/tmp/tammy.provisionprofile",
    TAMMY_MACOS_PRIVACY_POLICY_URL: "https://example.com/tammy/privacy",
    TAMMY_MACOS_SIGNING_IDENTITY: `${
      mode === "distribution" ? "Apple Distribution" : "Apple Development"
    }: Tammy Pty Ltd (ABCDE12345)`,
    TAMMY_MACOS_SIGNING_MODE: mode,
    TAMMY_MACOS_SUPPORT_URL: "https://example.com/tammy/support",
    TAMMY_MACOS_TEAM_ID: "ABCDE12345",
  };
}

test("exposes an injectable local package execution seam", () => {
  assert.equal(typeof packageMacOSStore.executeMacOSStoreBuild, "function");
});

test("distribution plan checks, builds, packages MAS and creates a signed flat package", () => {
  const plan = createMacOSStoreBuildPlan(root, environment());
  const commands = plan.commands.map(({ command, args }) => [command, ...args]);
  assert.equal(plan.app, path.join(root, "apps/desktop/out/Tammy-mas-arm64/Tammy.app"));
  assert.match(plan.pkg ?? "", /Tammy-0\.1\.0-build\.42\.pkg$/);
  assert.deepEqual(commands, [
    [process.execPath, path.join(root, "scripts/check-macos-store.mjs"), "--release"],
    [process.execPath, path.join(root, "scripts/build-sbr-helper.mjs"), "--mas-raw"],
    [
      "/usr/bin/codesign",
      "--force",
      "--sign",
      environment().TAMMY_MACOS_SIGNING_IDENTITY,
      "--entitlements",
      path.join(root, "apps/desktop/release/macos/entitlements.mas.sbr-helper.plist"),
      "--identifier",
      "com.tammy.desktop.sbr-helper",
      "--timestamp",
      path.join(root, "apps/desktop/resources/sbr-helper/darwin-arm64/tammy-sbr-helper"),
    ],
    [
      "/usr/bin/codesign",
      "--verify",
      "--strict",
      "-R",
      '=identifier "com.tammy.desktop.sbr-helper" and anchor apple generic and certificate leaf[subject.OU] = "ABCDE12345"',
      path.join(root, "apps/desktop/resources/sbr-helper/darwin-arm64/tammy-sbr-helper"),
    ],
    [process.execPath, path.join(root, "scripts/build-sbr-helper.mjs"), "--mas-profile"],
    ["pnpm", "core:build"],
    [
      "/usr/bin/codesign",
      "--force",
      "--sign",
      environment().TAMMY_MACOS_SIGNING_IDENTITY,
      "--entitlements",
      path.join(root, "apps/desktop/release/macos/entitlements.mas.core.plist"),
      "--timestamp=none",
      path.join(root, "apps/desktop/resources/core/darwin-arm64/tammy-core"),
    ],
    [
      "/usr/bin/codesign",
      "--verify",
      "--strict",
      path.join(root, "apps/desktop/resources/core/darwin-arm64/tammy-core"),
    ],
    ["pnpm", "build:manifest"],
    ["pnpm", "--dir", "apps/desktop", "package", "--platform=mas", "--arch=arm64"],
    [
      process.execPath,
      path.join(root, "apps/desktop/scripts/find-packaged-app.mjs"),
      "--verify",
      "--platform",
      "mas",
      "--source-manifest",
      path.join(root, "apps/desktop/resources/build/build-manifest.json"),
    ],
    [process.execPath, path.join(root, "scripts/verify-sbr-helper-signature.mjs"), "--mas"],
    [
      "/usr/bin/codesign",
      "--verify",
      "--deep",
      "--strict",
      "-R",
      '=identifier "com.tammy.desktop" and anchor apple generic and certificate leaf[subject.OU] = "ABCDE12345"',
      plan.app,
    ],
    [
      "/usr/bin/productbuild",
      "--component",
      plan.app,
      "/Applications",
      "--sign",
      environment().TAMMY_MACOS_INSTALLER_IDENTITY,
      plan.pkg,
    ],
  ]);
  assert.equal(plan.environment.TAMMY_RELEASE_PROFILE, "mas");
  assert.equal(
    plan.environment.VITE_TAMMY_PRIVACY_POLICY_URL,
    environment().TAMMY_MACOS_PRIVACY_POLICY_URL,
  );
  assert.equal(plan.environment.VITE_TAMMY_SUPPORT_URL, environment().TAMMY_MACOS_SUPPORT_URL);
  const helperPath = path.join(
    root,
    "apps/desktop/resources/sbr-helper/darwin-arm64/tammy-sbr-helper",
  );
  const helperSignIndex = commands.findIndex(
    ([command, ...args]) => command === "/usr/bin/codesign" && args.at(-1) === helperPath,
  );
  const profileIndex = commands.findIndex((command) => command.at(-1) === "--mas-profile");
  const manifestIndex = commands.findIndex(
    ([command, ...args]) => command === "pnpm" && args.join(" ") === "build:manifest",
  );
  const packageIndex = commands.findIndex(
    ([command, ...args]) => command === "pnpm" && args.includes("--platform=mas"),
  );
  assert.ok(helperSignIndex > 0 && helperSignIndex < profileIndex);
  assert.ok(profileIndex < manifestIndex && manifestIndex < packageIndex);
  assert.equal(
    commands
      .slice(profileIndex + 1)
      .filter(
        ([command, ...args]) =>
          command === "/usr/bin/codesign" && args.includes("--sign") && args.at(-1) === helperPath,
      ).length,
    0,
    "the final helper bytes must never be signed after profile generation",
  );
});

test("development signing stops at a locally runnable MAS app", () => {
  const plan = createMacOSStoreBuildPlan(root, environment("development"));
  assert.equal(plan.pkg, undefined);
  assert.equal(plan.commands.length, 13);
});

test("clean MAS execution preserves the final signed helper bytes through package verification", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment("development"));
  let sourceHelper;
  let packagedHelper;
  let profileHelper;
  let finalVerification = false;
  await packageMacOSStore.executeMacOSStoreBuild(plan, {
    commandRunner: async ({ command, args }) => {
      if (args.at(-1) === "--mas-raw") sourceHelper = Buffer.from("raw deterministic helper");
      if (
        command === "/usr/bin/codesign" &&
        args.includes("--sign") &&
        args.at(-1)?.endsWith("tammy-sbr-helper")
      ) {
        assert.deepEqual(sourceHelper, Buffer.from("raw deterministic helper"));
        sourceHelper = Buffer.from("final MAS signed helper");
      }
      if (args.at(-1) === "--mas-profile") profileHelper = Buffer.from(sourceHelper);
      if (command === "pnpm" && args.includes("--platform=mas")) {
        assert.deepEqual(profileHelper, sourceHelper);
        packagedHelper = Buffer.from(sourceHelper);
      }
      if (args.at(-1) === "--mas") {
        assert.deepEqual(packagedHelper, profileHelper);
        finalVerification = true;
      }
      return undefined;
    },
    write: () => {},
  });
  assert.equal(finalVerification, true);
});

function successfulRunner(events, assessment = { stderr: "accepted\nsource=Mac App Store\n" }) {
  return async (commandSpec) => {
    events.push(commandSpec.command);
    if (commandSpec.command === "/usr/sbin/spctl") {
      return { exitCode: 0, signal: null, ...assessment };
    }
    return undefined;
  };
}

test("development execution stops at the app without package validation or package output", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment("development"));
  const events = [];
  const output = [];

  const result = await packageMacOSStore.executeMacOSStoreBuild(plan, {
    commandRunner: successfulRunner(events),
    packageHasher: async () => {
      throw new Error("package hash should not run");
    },
    write: (line) => output.push(line),
  });

  assert.deepEqual(
    events,
    plan.commands.map((step) => step.command),
  );
  assert.deepEqual(result, { app: plan.app });
  assert.deepEqual(output, [`{"app":"${plan.app}"}\n`]);
});

test("outer MAS app seal failure stops before productbuild", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());
  const commands = [];
  await assert.rejects(
    packageMacOSStore.executeMacOSStoreBuild(plan, {
      commandRunner: async (commandSpec) => {
        commands.push(commandSpec);
        if (commandSpec.command === "/usr/bin/codesign" && commandSpec.args.includes("--deep")) {
          throw new Error("invalid outer seal");
        }
        return undefined;
      },
      write: () => {},
    }),
    /MACOS_STORE_COMMAND_FAILED/,
  );
  assert.equal(
    commands.some(({ command }) => command === "/usr/bin/productbuild"),
    false,
  );
});

test("build lock rejects concurrent owners, cleans failures, and never removes a foreign lock", async () => {
  const temporary = await mkdtemp(path.join(tmpdir(), "tammy-macos-package-lock-"));
  const lockPath = path.join(temporary, ".tmp/sbr-build-owner/owner.lock");
  const plan = {
    app: path.join(temporary, "Tammy.app"),
    commands: [{ args: [], command: "build" }],
    environment: {},
    root: temporary,
  };
  let unblock;
  const blocked = new Promise((resolve) => {
    unblock = resolve;
  });
  let started;
  const entered = new Promise((resolve) => {
    started = resolve;
  });
  try {
    const first = packageMacOSStore.executeMacOSStoreBuild(plan, {
      commandRunner: async () => {
        started();
        await blocked;
      },
      write: () => {},
    });
    await entered;
    await assert.rejects(
      packageMacOSStore.executeMacOSStoreBuild(plan, {
        commandRunner: async () => {},
        write: () => {},
      }),
      /SBR_BUILD_LOCKED/,
    );
    unblock();
    await first;
    await assert.rejects(readFile(lockPath), /ENOENT/);

    await assert.rejects(
      packageMacOSStore.executeMacOSStoreBuild(plan, {
        commandRunner: async () => {
          throw new Error("build failed");
        },
        write: () => {},
      }),
      /MACOS_STORE_COMMAND_FAILED/,
    );
    await assert.rejects(readFile(lockPath), /ENOENT/);

    await packageMacOSStore.executeMacOSStoreBuild(plan, {
      commandRunner: async () => {
        await unlink(lockPath);
        await writeFile(lockPath, "foreign-owner\n", { mode: 0o600 });
      },
      write: () => {},
    });
    assert.equal(await readFile(lockPath, "utf8"), "foreign-owner\n");
  } finally {
    await rm(temporary, { force: true, recursive: true });
  }
});

test("distribution execution validates its package in the required order and emits stable JSON", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());
  const events = [];
  const output = [];
  const validationSteps = [];
  const hash = "a".repeat(64);

  const result = await packageMacOSStore.executeMacOSStoreBuild(plan, {
    commandRunner: async (commandSpec, options) => {
      events.push(commandSpec.command);
      if (commandSpec.command.startsWith("/usr/sbin/")) {
        validationSteps.push({ args: commandSpec.args, command: commandSpec.command, options });
      }
      return commandSpec.command === "/usr/sbin/spctl"
        ? { exitCode: 0, signal: null, stderr: "accepted\nsource=Mac App Store\n" }
        : undefined;
    },
    packageHasher: async (pkg) => {
      assert.equal(pkg, plan.pkg);
      events.push("sha256");
      return hash;
    },
    write: (line) => {
      events.push("json");
      output.push(line);
    },
  });

  assert.deepEqual(events, [
    ...plan.commands.map((step) => step.command),
    "/usr/sbin/pkgutil",
    "sha256",
    "/usr/sbin/spctl",
    "json",
  ]);
  const inheritedToken = validationSteps[0].options.environment[SBR_BUILD_LOCK_ENV];
  assert.match(inheritedToken, /^[0-9a-f]{64}$/);
  assert.deepEqual(validationSteps, [
    {
      command: "/usr/sbin/pkgutil",
      args: ["--check-signature", plan.pkg],
      options: {
        captureOutput: true,
        cwd: root,
        environment: { ...plan.environment, [SBR_BUILD_LOCK_ENV]: inheritedToken },
      },
    },
    {
      command: "/usr/sbin/spctl",
      args: ["--assess", "--type", "install", "--verbose=4", plan.pkg],
      options: {
        allowNonZero: true,
        captureOutput: true,
        cwd: root,
        environment: { ...plan.environment, [SBR_BUILD_LOCK_ENV]: inheritedToken },
      },
    },
  ]);
  assert.deepEqual(result, {
    app: plan.app,
    pkg: plan.pkg,
    pkgSha256: hash,
    gatekeeperAssessment: "accepted",
  });
  assert.deepEqual(output, [
    `${JSON.stringify({
      app: plan.app,
      pkg: plan.pkg,
      pkgSha256: hash,
      gatekeeperAssessment: "accepted",
    })}\n`,
  ]);
});

test("distribution execution fails closed when a command runner cannot spawn", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());
  const secret = "DO_NOT_DISCLOSE";

  await assert.rejects(
    () =>
      packageMacOSStore.executeMacOSStoreBuild(plan, {
        commandRunner: async () => {
          throw new Error(secret);
        },
        packageHasher: async () => "a".repeat(64),
        write: () => {},
      }),
    (error) => error.message === "MACOS_STORE_COMMAND_FAILED" && !error.message.includes(secret),
  );
});

test("distribution execution treats an invalid package signature as fatal", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());

  await assert.rejects(
    () =>
      packageMacOSStore.executeMacOSStoreBuild(plan, {
        commandRunner: async (commandSpec) => {
          if (commandSpec.command === "/usr/sbin/pkgutil") throw new Error("nonzero");
          return commandSpec.command === "/usr/sbin/spctl" ? { stderr: "accepted" } : undefined;
        },
        packageHasher: async () => "a".repeat(64),
        write: () => {},
      }),
    /MACOS_STORE_PACKAGE_SIGNATURE_INVALID/,
  );
});

test("distribution execution fails closed when Gatekeeper produces no verdict", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());

  await assert.rejects(
    () =>
      packageMacOSStore.executeMacOSStoreBuild(plan, {
        commandRunner: successfulRunner([], {}),
        packageHasher: async () => "a".repeat(64),
        write: () => {},
      }),
    /MACOS_STORE_GATEKEEPER_OUTPUT_MISSING/,
  );
});

test("distribution execution fails closed for an unclassifiable Gatekeeper verdict", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());
  const secret = "DO_NOT_DISCLOSE";

  await assert.rejects(
    () =>
      packageMacOSStore.executeMacOSStoreBuild(plan, {
        commandRunner: successfulRunner([], { stderr: `ambiguous ${secret}` }),
        packageHasher: async () => "a".repeat(64),
        write: () => {},
      }),
    (error) =>
      error.message === "MACOS_STORE_GATEKEEPER_OUTPUT_UNCLASSIFIABLE" &&
      !error.message.includes(secret),
  );
});

test("distribution execution records an ordinary pre-App-Store Gatekeeper rejection without failing", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());
  const output = [];

  const result = await packageMacOSStore.executeMacOSStoreBuild(plan, {
    commandRunner: successfulRunner([], {
      exitCode: 3,
      stderr: "Tammy.pkg: rejected\nsource=Developer ID\n",
    }),
    packageHasher: async () => "b".repeat(64),
    write: (line) => output.push(line),
  });

  assert.equal(result.gatekeeperAssessment, "rejected");
  assert.match(output[0], /"gatekeeperAssessment":"rejected"/);
});

test("distribution execution fails closed for unexpected Gatekeeper status and verdict pairs", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());
  const invalidAssessments = [
    { exitCode: 1, stderr: "Tammy.pkg: rejected\n" },
    { exitCode: 2, stderr: "Tammy.pkg: rejected\n" },
    { exitCode: 4, stderr: "Tammy.pkg: rejected\n" },
    { exitCode: 3, stderr: "Tammy.pkg: accepted\n" },
    { exitCode: 0, stderr: "Tammy.pkg: rejected\n" },
  ];

  for (const assessment of invalidAssessments) {
    await assert.rejects(
      () =>
        packageMacOSStore.executeMacOSStoreBuild(plan, {
          commandRunner: successfulRunner([], assessment),
          packageHasher: async () => "c".repeat(64),
          write: () => {},
        }),
      /MACOS_STORE_GATEKEEPER_ASSESSMENT_INVALID/,
    );
  }
});

test("distribution execution preserves bounded validation-command output failures", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());

  for (const command of ["/usr/sbin/pkgutil", "/usr/sbin/spctl"]) {
    await assert.rejects(
      () =>
        packageMacOSStore.executeMacOSStoreBuild(plan, {
          commandRunner: async (commandSpec) => {
            if (commandSpec.command === command) {
              throw new Error("MACOS_STORE_COMMAND_OUTPUT_INVALID");
            }
            return commandSpec.command === "/usr/sbin/spctl"
              ? { exitCode: 0, signal: null, stderr: "accepted" }
              : undefined;
          },
          packageHasher: async () => "c".repeat(64),
          write: () => {},
        }),
      /MACOS_STORE_COMMAND_OUTPUT_INVALID/,
    );
  }
});

test("distribution execution fails closed when Gatekeeper is terminated after a partial rejection", async () => {
  const plan = createMacOSStoreBuildPlan(root, environment());

  await assert.rejects(
    () =>
      packageMacOSStore.executeMacOSStoreBuild(plan, {
        commandRunner: successfulRunner([], {
          signal: "SIGTERM",
          stderr: "Tammy.pkg: rejected\n",
        }),
        packageHasher: async () => "c".repeat(64),
        write: () => {},
      }),
    /MACOS_STORE_GATEKEEPER_ASSESSMENT_INVALID/,
  );
});
