import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "jsdom",
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
