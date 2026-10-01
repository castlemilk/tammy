import { createCaller } from "electron-conveyor/main";
import { expect, test, vi } from "vitest";

vi.mock("electron", () => ({ app: { isPackaged: true } }));

import manifest from "../shared/conveyor-manifest.json";
import { createTammyRouter } from "./router";

test("the router composes exactly the fixed eight-module manifest and cannot dispatch an unknown method", async () => {
  const client = new Proxy({}, { get: () => async (request: unknown) => request });
  const router = createTammyRouter();
  const caller = createCaller(router, {
    ctx: { client: client as never, fileIntake: {} as never },
  });

  expect(router.manifest).toEqual(manifest);
  expect(Object.keys(router.modules)).toEqual(Object.keys(manifest));
  await expect(
    (caller.workspace as unknown as { missing(): Promise<unknown> }).missing(),
  ).rejects.toMatchObject({ code: "UNKNOWN_PROCEDURE" });
  expect(() =>
    (
      caller as unknown as { missing: { getSystemDiagnostics(): Promise<unknown> } }
    ).missing.getSystemDiagnostics(),
  ).toThrow(/Unknown module/);
  await expect(
    (
      caller.workspace as unknown as { getSystemDiagnostics(): Promise<unknown> }
    ).getSystemDiagnostics(),
  ).rejects.toMatchObject({ code: "UNKNOWN_PROCEDURE" });
});
