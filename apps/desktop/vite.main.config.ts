import { defineConfig } from "vite";

export default defineConfig({
  build: {
    lib: {
      entry: "src/main/index.ts",
      fileName: () => "main.js",
      formats: ["es"],
    },
    sourcemap: false,
    minify: true,
    rollupOptions: { external: ["electron"] },
  },
});
