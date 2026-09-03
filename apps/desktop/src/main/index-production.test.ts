import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { beforeEach, describe, expect, it, vi } from "vitest";

const electron = vi.hoisted(() => ({
  app: {
    getAppPath: vi.fn(() => "/Applications/Tammy.app/Contents/Resources/app"),
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
    electron.app.getAppPath.mockClear();
    electron.app.isPackaged = true;
    electron.app.setPath.mockClear();
    electron.dialog.showOpenDialog.mockReset();
    delete process.env.ELECTRON_RENDERER_URL;
  });

  it("sets the validated accounting-fresh user-data root before production dependencies start", () => {
    createProductionDependencies([
      "/Applications/Tammy.app/Contents/MacOS/Tammy",
      "--user-data-dir=/private/tmp/tammy-accounting-fresh-fixed",
      "--tammy-launch-scenario=accounting-fresh",
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

describe("createProductionDependencies electron-vite runtime paths", () => {
  beforeEach(() => {
    electron.app.isPackaged = false;
    delete process.env.ELECTRON_RENDERER_URL;
  });

  it("uses the one canonical ELECTRON_RENDERER_URL origin in development", () => {
    process.env.ELECTRON_RENDERER_URL = "http://127.0.0.1:43123";

    const dependencies = createProductionDependencies(["tammy"]);

    expect(dependencies.applicationUrl).toBe("http://127.0.0.1:43123/");
  });

  it.each([
    undefined,
    "http://user@127.0.0.1:43123",
    "http://example.com:43123",
    "http://127.0.0.1:43123/app",
    "http://127.0.0.1:43123/?query=value",
    "http://127.0.0.1:43123/#fragment",
  ])("fails closed for an invalid development renderer URL: %s", (url) => {
    if (url === undefined) delete process.env.ELECTRON_RENDERER_URL;
    else process.env.ELECTRON_RENDERER_URL = url;

    expect(() => createProductionDependencies(["tammy"])).toThrow("INVALID_VITE_ORIGIN");
  });

  it("resolves the sandbox preload from out/main to out/preload/index.cjs", () => {
    const source = readFileSync(
      path.join(path.dirname(fileURLToPath(import.meta.url)), "index-production.ts"),
      "utf8",
    );

    expect(source).toContain('path.resolve(buildDirectory, "../preload/index.cjs")');
    expect(source).toContain('path.resolve(buildDirectory, "../../resources")');
    expect(source).toContain('path.join(app.getAppPath(), "out/renderer")');
    expect(source).not.toContain("MAIN_WINDOW_VITE_");
  });
});
