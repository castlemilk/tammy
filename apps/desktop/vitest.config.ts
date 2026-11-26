import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "jsdom",
    // Give each test window its own jsdom storage.
    execArgv: process.allowedNodeEnvironmentFlags.has("--no-experimental-webstorage")
      ? ["--no-experimental-webstorage"]
      : [],
    server: {
      deps: {
        inline: ["electron-conveyor"],
      },
    },
    globals: false,
    restoreMocks: true,
    clearMocks: true,
    setupFiles: ["src/app/test/setup.ts"],
    include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
  },
});
