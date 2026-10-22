import path from "node:path";

import { describe, expect, it } from "vitest";

import { createCoreLaunchArguments } from "./core-launch";

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
});
