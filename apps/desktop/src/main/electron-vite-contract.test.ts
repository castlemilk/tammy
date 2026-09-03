// @vitest-environment node

import { builtinModules } from "node:module";
import { resolve } from "node:path";
import config, { isElectronRuntimeExternal } from "../../electron.vite.config";
import type { LibraryOptions } from "vite";
import { describe, expect, test } from "vitest";

const desktopRoot = process.cwd();

function libraryFileName(library: false | LibraryOptions | undefined) {
  const fileName = library && library.fileName;
  return typeof fileName === "function" ? fileName("es", "index") : fileName;
}

function pluginNames(plugins: unknown): string[] {
  if (!Array.isArray(plugins)) {
    return [];
  }

  return plugins
    .flatMap((plugin) => (Array.isArray(plugin) ? plugin : [plugin]))
    .flatMap((plugin) =>
      typeof plugin === "object" && plugin !== null && "name" in plugin && typeof plugin.name === "string"
        ? [plugin.name]
        : [],
    );
}

describe("electron-vite configuration", () => {
  test("uses the fixed Tammy build contract", () => {
    expect(config.main?.build?.outDir).toBe(resolve(desktopRoot, "out/main"));
    expect(config.preload?.build?.outDir).toBe(resolve(desktopRoot, "out/preload"));
    expect(config.renderer?.build?.outDir).toBe(resolve(desktopRoot, "out/renderer"));
    expect(config.renderer?.root).toBe(resolve(desktopRoot, "src/app"));
    expect(config.renderer?.build?.rollupOptions?.input).toBe(
      resolve(desktopRoot, "src/app/index.html"),
    );
    expect(libraryFileName(config.main?.build?.lib)).toBe("index.js");
    expect(libraryFileName(config.preload?.build?.lib)).toBe("index.cjs");
    expect(config.main?.build?.lib && config.main.build.lib.formats).toEqual(["es"]);
    expect(config.preload?.build?.lib && config.preload.build.lib.formats).toEqual(["cjs"]);
    expect(Object.keys(config.main?.resolve?.alias ?? {})).toEqual([
      "@/app",
      "@/conveyor",
      "@/main",
      "@/shared",
      "@/resources",
    ]);
    expect(isElectronRuntimeExternal("electron")).toBe(true);
    expect(isElectronRuntimeExternal("node:path")).toBe(true);
    expect(isElectronRuntimeExternal(builtinModules[0] ?? "fs")).toBe(true);
    expect(isElectronRuntimeExternal("@tammy/connect-client")).toBe(false);
    expect(isElectronRuntimeExternal("electron-conveyor")).toBe(false);
    expect(config.main?.build?.externalizeDeps).toBe(false);
    expect(config.preload?.build?.externalizeDeps).toBe(false);
    expect(config.renderer?.base).toBe("./");
    expect(pluginNames(config.renderer?.plugins)).toEqual(
      expect.arrayContaining(["@tailwindcss/vite:scan", "vite:react-babel"]),
    );
    expect(config.main?.build?.sourcemap).toBe(false);
    expect(config.preload?.build?.sourcemap).toBe(false);
    expect(config.renderer?.build?.sourcemap).toBe(false);
    expect(JSON.stringify(config)).not.toContain("ELECTRON_DISABLE_SANDBOX");
  });
});
