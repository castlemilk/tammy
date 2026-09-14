import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { beforeEach, describe, expect, it, vi } from "vitest";

type CapturedConveyorRegistration = {
  readonly client: unknown;
  readonly getWindow: () => unknown;
};

const electron = vi.hoisted(() => ({
  app: {
    getAppPath: vi.fn(() => "/Applications/Tammy.app/Contents/Resources/app"),
    getPath: vi.fn(() => "/Users/test/Library/Application Support/Tammy"),
    isPackaged: true,
    setPath: vi.fn(),
  },
  browserWindows: [] as Array<{ destroyed: boolean }>,
  dialog: { showOpenDialog: vi.fn() },
}));

const conveyor = vi.hoisted(() => ({
  registerTammyConveyor: vi.fn<(options: CapturedConveyorRegistration) => () => void>(() =>
    vi.fn(),
  ),
}));

vi.mock("../conveyor/registration", () => conveyor);

vi.mock("electron", () => ({
  app: electron.app,
  BrowserWindow: class {
    public destroyed = false;
    public readonly webContents = {};
    public constructor(_options: unknown) {
      electron.browserWindows.push(this);
    }
    public destroy() {
      this.destroyed = true;
    }
    public isDestroyed() {
      return this.destroyed;
    }
    public loadURL() {
      return Promise.resolve();
    }
    public on() {
      return this;
    }
    public once() {
      return this;
    }
    public show() {}
  },
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
    electron.browserWindows.length = 0;
    conveyor.registerTammyConveyor.mockClear();
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

  it("registers Conveyor against only the lifecycle-owned live native window", () => {
    const dependencies = createProductionDependencies([
      "/Applications/Tammy.app/Contents/MacOS/Tammy",
    ]);
    const lifecycleWindow = dependencies.createWindow();
    const getWindow = vi.fn<() => typeof lifecycleWindow | undefined>(() => undefined);
    const client = {} as Parameters<typeof dependencies.registerConveyor>[1];

    dependencies.registerConveyor(getWindow, client);

    const registration = conveyor.registerTammyConveyor.mock.calls[0]?.[0];
    expect(registration?.client).toBe(client);
    expect(registration?.getWindow()).toBeUndefined();
    getWindow.mockReturnValue(lifecycleWindow);
    const nativeWindow = electron.browserWindows[0];
    expect(nativeWindow).toBeDefined();
    if (!nativeWindow) throw new Error("EXPECTED_LIVE_BROWSER_WINDOW");
    expect(registration?.getWindow()).toBe(nativeWindow);
    nativeWindow.destroyed = true;
    expect(registration?.getWindow()).toBeUndefined();
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
