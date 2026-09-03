import { builtinModules } from "node:module";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "electron-vite";

const desktopRoot = dirname(fileURLToPath(import.meta.url));

export const isElectronRuntimeExternal = (id: string): boolean =>
  id === "electron" || id.startsWith("node:") || builtinModules.includes(id);

const aliases = {
  "@/app": resolve(desktopRoot, "src/app"),
  "@/conveyor": resolve(desktopRoot, "src/conveyor"),
  "@/main": resolve(desktopRoot, "src/main"),
  "@/shared": resolve(desktopRoot, "src/shared"),
  "@/resources": resolve(desktopRoot, "resources"),
};

export default defineConfig({
  main: {
    build: {
      externalizeDeps: false,
      outDir: resolve(desktopRoot, "out/main"),
      lib: {
        entry: resolve(desktopRoot, "src/main/index.ts"),
        fileName: () => "index.js",
        formats: ["es"],
      },
      rollupOptions: { external: isElectronRuntimeExternal },
      sourcemap: false,
    },
    resolve: { alias: aliases },
  },
  preload: {
    build: {
      externalizeDeps: false,
      outDir: resolve(desktopRoot, "out/preload"),
      lib: {
        entry: resolve(desktopRoot, "src/preload/index.ts"),
        fileName: () => "index.cjs",
        formats: ["cjs"],
      },
      rollupOptions: { external: isElectronRuntimeExternal },
      sourcemap: false,
    },
    resolve: { alias: aliases },
  },
  renderer: {
    root: resolve(desktopRoot, "src/app"),
    base: "./",
    build: {
      outDir: resolve(desktopRoot, "out/renderer"),
      rollupOptions: { input: resolve(desktopRoot, "src/app/index.html") },
      sourcemap: false,
    },
    resolve: { alias: aliases },
    plugins: [tailwindcss(), react()],
  },
});
