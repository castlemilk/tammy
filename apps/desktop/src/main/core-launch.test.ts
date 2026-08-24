import path from "node:path";

import { describe, expect, it } from "vitest";

import { createCoreLaunchArguments, parseLocalLaunchArguments } from "./core-launch";

describe("createCoreLaunchArguments", () => {
  it("keeps packaged launches on the durable local-core path", () => {
    expect(
      createCoreLaunchArguments({
        isPackaged: true,
        userDataPath: "/Users/test/Library/Application Support/Tammy",
      }),
    ).toEqual([
      "--data-root",
      path.join("/Users/test/Library/Application Support/Tammy", "local-core"),
    ]);
  });

  it("uses an isolated process-local root and explicit memory anchors in development", () => {
    expect(
      createCoreLaunchArguments({
        isPackaged: false,
        userDataPath: "/Users/test/Library/Application Support/Tammy",
      }),
    ).toEqual([
      "--data-root",
      path.join("/Users/test/Library/Application Support/Tammy", "local-core-development"),
      "--development-memory-anchors",
    ]);
  });

  it("passes the exact owned SBR profile to the local core", () => {
    expect(
      createCoreLaunchArguments({
        isPackaged: true,
        sbrProfilePath:
          "/Applications/Tammy.app/Contents/Resources/sbr/simulator/sbr-profile-v1.json",
        userDataPath: "/Users/test/Library/Application Support/Tammy",
      }),
    ).toEqual([
      "--data-root",
      path.join("/Users/test/Library/Application Support/Tammy", "local-core"),
      "--sbr-profile=/Applications/Tammy.app/Contents/Resources/sbr/simulator/sbr-profile-v1.json",
    ]);
  });

  it("rejects a non-absolute SBR profile before core launch", () => {
    expect(() =>
      createCoreLaunchArguments({
        isPackaged: true,
        sbrProfilePath: "resources/sbr-profile-v1.json",
        userDataPath: "/Users/test/Library/Application Support/Tammy",
      }),
    ).toThrow("SBR_PROFILE_PATH_INVALID");
  });
});

describe("parseLocalLaunchArguments", () => {
  it("preserves the normal production launch when no local scenario argument exists", () => {
    expect(parseLocalLaunchArguments(["/Applications/Tammy.app/Contents/MacOS/Tammy"])).toEqual({});
  });

  it("consumes one absolute accounting-fresh user-data directory", () => {
    expect(
      parseLocalLaunchArguments([
        "/Applications/Tammy.app/Contents/MacOS/Tammy",
        "--user-data-dir=/private/tmp/tammy-accounting-fresh-123",
      ]),
    ).toEqual({ userDataPath: "/private/tmp/tammy-accounting-fresh-123" });
  });

  it.each([
    ["duplicate", ["--user-data-dir=/private/tmp/one", "--user-data-dir=/private/tmp/two"]],
    ["relative", ["--user-data-dir=relative/path"]],
    ["empty", ["--user-data-dir="]],
    ["split form", ["--user-data-dir", "/private/tmp/one"]],
  ])("rejects a %s user-data argument", (_name, arguments_) => {
    expect(() => parseLocalLaunchArguments(arguments_)).toThrow("LOCAL_USER_DATA_ARGUMENT_INVALID");
  });

  it.each([
    "--sbr-profile=/private/tmp/profile.json",
    "--tammy-scenario=accounting-fresh",
    "--tammy-unknown=value",
  ])("rejects unknown local scenario switch %s", (argument) => {
    expect(() => parseLocalLaunchArguments([argument])).toThrow(
      `LOCAL_SCENARIO_SWITCH_UNSUPPORTED:${argument.split("=")[0]}`,
    );
  });
});
