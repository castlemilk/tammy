import { defineConfig } from "vite";

export default defineConfig({
  build: {
    sourcemap: false,
    minify: true,
    rollupOptions: { external: ["electron"] },
  },
});
