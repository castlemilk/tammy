import { beforeEach, describe, expect, it, vi } from "vitest";

const electron = vi.hoisted(() => ({
  app: {
    getPath: vi.fn(() => "/Users/test/Library/Application Support/Tammy"),
    isPackaged: true,
    setPath: vi.fn(),
  },
}));

vi.mock("electron", () => ({
  app: electron.app,
  BrowserWindow: class {},
  ipcMain: {},
  net: {},
  protocol: {},
  session: {},
  shell: {},
}));

import { createProductionDependencies } from "./index-production";

describe("createProductionDependencies local scenario ownership", () => {
  beforeEach(() => {
    electron.app.setPath.mockClear();
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
});
