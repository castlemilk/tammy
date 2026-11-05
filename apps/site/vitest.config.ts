import mdx from "@mdx-js/rollup";
import remarkGfm from "remark-gfm";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [{ enforce: "pre", ...mdx({ remarkPlugins: [remarkGfm] }) }],
  test: {
    environment: "jsdom",
    include: ["tests/**/*.test.tsx"],
  },
});
