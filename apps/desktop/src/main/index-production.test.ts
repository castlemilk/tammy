import { beforeEach, describe, expect, it, vi } from "vitest";

const electron = vi.hoisted(() => ({
  app: {
    getPath: vi.fn(() => "/Users/test/Library/Application Support/Tammy"),
    isPackaged: true,
    setPath: vi.fn(),
  },
  dialog: { showOpenDialog: vi.fn() },
}));

vi.mock("electron", () => ({
  app: electron.app,
  BrowserWindow: class {},
  dialog: electron.dialog,
  ipcMain: {},
  net: {},
  protocol: {},
  session: {},
  shell: {},
}));

import { createProductionDependencies, resolveSbrFileReleaseKind } from "./index-production";
import type { createSbrFileIntake } from "./sbr-file-intake";

describe("createProductionDependencies local scenario ownership", () => {
  beforeEach(() => {
    electron.app.setPath.mockClear();
    electron.dialog.showOpenDialog.mockReset();
  });

  it("sets the validated accounting-fresh user-data root before production dependencies start", () => {
    createProductionDependencies([
      "/Applications/Tammy.app/Contents/MacOS/Tammy",
      "--user-data-dir=/private/tmp/tammy-accounting-fresh-fixed",
    ]);

    expect(electron.app.setPath).toHaveBeenCalledTimes(1);
    expect(electron.app.setPath).toHaveBeenCalledWith(
      "userData",
      "/private/tmp/tammy-accounting-fresh-fixed",
    );
  });

  it("preserves the existing production launch when no scenario argument exists", () => {
    createProductionDependencies(["/Applications/Tammy.app/Contents/MacOS/Tammy"]);
    expect(electron.app.setPath).not.toHaveBeenCalled();
  });

  it("fails before dependency creation for invalid local scenario arguments", () => {
    expect(() =>
      createProductionDependencies([
        "/Applications/Tammy.app/Contents/MacOS/Tammy",
        "--user-data-dir=relative",
      ]),
    ).toThrow("LOCAL_USER_DATA_ARGUMENT_INVALID");
    expect(electron.app.setPath).not.toHaveBeenCalled();
  });

  it.each([
    [false, false, "development"],
    [false, true, "development"],
    [true, false, "ordinary-package"],
    [true, true, "mas"],
  ] as const)(
    "resolves packaged=%s mas=%s to exact release kind %s",
    (isPackaged, isMas, expected) => {
      expect(resolveSbrFileReleaseKind({ isMas, isPackaged })).toBe(expected);
    },
  );

  it("composes the native dialog, exact release kind, and credential-intake cleanup boundary", async () => {
    const clear = vi.fn();
    const createFileIntake = vi.fn((_options: Parameters<typeof createSbrFileIntake>[0]) => ({
      clear,
      consumeMachineCredentialFile: vi.fn(),
      selectMachineCredentialFile: vi.fn(),
    }));
    electron.dialog.showOpenDialog.mockResolvedValue({ canceled: true, filePaths: [] });
    const dependencies = createProductionDependencies(
      ["/Applications/Tammy.app/Contents/MacOS/Tammy"],
      {
        createFileIntake,
        isMas: false,
      },
    );

    expect(createFileIntake).toHaveBeenCalledOnce();
    expect(dependencies.releaseKind).toBe("ordinary-package");
    const options = createFileIntake.mock.calls[0]?.[0];
    expect(options?.releaseKind).toBe("ordinary-package");
    await options?.showOpenDialog({ properties: ["openFile"], securityScopedBookmarks: false });
    expect(electron.dialog.showOpenDialog).toHaveBeenCalledExactlyOnceWith({
      properties: ["openFile"],
      securityScopedBookmarks: false,
    });

    dependencies.cleanupSensitiveState();

    expect(clear).toHaveBeenCalledOnce();
  });
});
