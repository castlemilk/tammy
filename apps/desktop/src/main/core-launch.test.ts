import path from "node:path";

import { describe, expect, it } from "vitest";

import { createCoreLaunchArguments } from "./core-launch";

describe("createCoreLaunchArguments", () => {
  it("keeps packaged launches on the durable local-core path", () => {
    expect(
      createCoreLaunchArguments({
        isPackaged: true,
        processId: 42,
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
        processId: 42,
        userDataPath: "/Users/test/Library/Application Support/Tammy",
      }),
    ).toEqual([
      "--data-root",
      path.join("/Users/test/Library/Application Support/Tammy", "local-core-development-42"),
      "--development-memory-anchors",
    ]);
  });
});
