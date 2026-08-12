import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import * as packageMacOSStore from "./package-macos-store.mjs";

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
  assert.equal(plan.app, path.join(root, "apps/desktop/out/Tammy-mas-arm64/Tammy.app"));
  assert.match(plan.pkg ?? "", /Tammy-0\.1\.0-build\.42\.pkg$/);
  assert.deepEqual(
    plan.commands.map(({ command, args }) => [command, ...args]),
    [
      [process.execPath, path.join(root, "scripts/check-macos-store.mjs"), "--release"],
      ["pnpm", "core:build"],
      ["pnpm", "build:manifest"],
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
      [process.execPath, path.join(root, "scripts/write-build-manifest.mjs"), "--rehash-core"],
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
      [
        "/usr/bin/productbuild",
        "--component",
        plan.app,
        "/Applications",
        "--sign",
        environment().TAMMY_MACOS_INSTALLER_IDENTITY,
        plan.pkg,
      ],
    ],
  );
  assert.equal(plan.environment.TAMMY_RELEASE_PROFILE, "mas");
  assert.equal(
    plan.environment.VITE_TAMMY_PRIVACY_POLICY_URL,
    environment().TAMMY_MACOS_PRIVACY_POLICY_URL,
  );
  assert.equal(plan.environment.VITE_TAMMY_SUPPORT_URL, environment().TAMMY_MACOS_SUPPORT_URL);
});

test("development signing stops at a locally runnable MAS app", () => {
  const plan = createMacOSStoreBuildPlan(root, environment("development"));
  assert.equal(plan.pkg, undefined);
  assert.equal(plan.commands.length, 8);
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
  assert.deepEqual(validationSteps, [
    {
      command: "/usr/sbin/pkgutil",
      args: ["--check-signature", plan.pkg],
      options: { captureOutput: true, cwd: root, environment: plan.environment },
    },
    {
      command: "/usr/sbin/spctl",
      args: ["--assess", "--type", "install", "--verbose=4", plan.pkg],
      options: {
        allowNonZero: true,
        captureOutput: true,
        cwd: root,
        environment: plan.environment,
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
