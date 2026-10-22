// @vitest-environment node

import path from "node:path";
import { fileURLToPath } from "node:url";
import { beforeEach, expect, it, vi } from "vitest";

const { execFileMock, spawnMock } = vi.hoisted(() => ({
  execFileMock: vi.fn(),
  spawnMock: vi.fn(),
}));

vi.mock("node:child_process", async (importOriginal) => {
  const actual = (await importOriginal()) as typeof import("node:child_process");
  return { ...actual, execFile: execFileMock, spawn: spawnMock };
});

import { buildPurchaseOpeningFixture } from "../../tests/e2e/support/purchase-opening-fixture";
import { buildPurchaseEvidenceInspector } from "../../tests/e2e/support/purchase-persisted-evidence";

const root = fileURLToPath(new URL("../../../../", import.meta.url));

beforeEach(() => {
  execFileMock.mockReset();
  execFileMock.mockImplementation((...args: unknown[]) => {
    const callback = args.at(-1);
    if (typeof callback === "function") callback(null, "", "");
    return undefined;
  });
  spawnMock.mockReset();
});

it.each([
  [
    "purchase evidence inspector",
    buildPurchaseEvidenceInspector,
    "build-purchase-e2e-inspector.mjs",
  ],
  [
    "purchase opening fixture",
    buildPurchaseOpeningFixture,
    "build-purchase-e2e-opening-fixture.mjs",
  ],
])("builds the %s with the pinned Node runtime", async (_name, build, script) => {
  await build();

  expect(execFileMock).toHaveBeenCalledTimes(1);
  const [executable, args, options] = execFileMock.mock.calls[0] as [
    string,
    string[],
    { cwd: string; maxBuffer: number },
  ];
  expect(executable).toBe(process.execPath);
  expect(args).toEqual([path.join(root, "scripts", script)]);
  expect(options).toEqual({ cwd: root, maxBuffer: 1024 * 1024 });
  expect([executable, ...args].join(" ")).not.toMatch(/(?:^|\s)(?:rtk|mise)(?:\s|$)/);
  expect(spawnMock).not.toHaveBeenCalled();
});
