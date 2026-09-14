// @vitest-environment node

import { readFile } from "node:fs/promises";
import path from "node:path";
import { describe, expect, test } from "vitest";
import type { ToolingAliasProbe } from "@/app/test/tooling.fixture";
import config from "../../../electron.vite.config";

const desktopRoot = process.cwd();
const aliasProbe: ToolingAliasProbe = { resolved: true };

describe("desktop tooling", () => {
  test("keeps the app entry local and resolves the explicit build aliases", async () => {
    expect(aliasProbe.resolved).toBe(true);
    expect(config.renderer?.base).toBe("./");
    expect(config.renderer?.resolve?.alias).toEqual({
      "@/app": path.join(desktopRoot, "src/app"),
      "@/conveyor": path.join(desktopRoot, "src/conveyor"),
      "@/main": path.join(desktopRoot, "src/main"),
      "@/shared": path.join(desktopRoot, "src/shared"),
      "@/resources": path.join(desktopRoot, "resources"),
    });

    const html = await readFile(path.join(desktopRoot, "src/app/index.html"), "utf8");
    expect(html).toContain("<title>Tammy</title>");
    expect(html).toContain('<div id="root"></div>');
    expect(html).toContain('<script type="module" src="/main.tsx"></script>');
    expect(html).not.toMatch(/<script[^>]+src=["']https?:\/\//u);
    expect(html).not.toMatch(/http-equiv=["']Content-Security-Policy["']/u);
  });

  test("keeps strict pnpm peers local to desktop while hoisting only Forge tooling", async () => {
    const desktopPackage = JSON.parse(
      await readFile(path.join(desktopRoot, "package.json"), "utf8"),
    );
    const workspace = await readFile(path.join(desktopRoot, "../../pnpm-workspace.yaml"), "utf8");

    expect(desktopPackage.dependencies).toMatchObject({
      "@radix-ui/react-compose-refs": "1.1.3",
      scheduler: "0.27.0",
    });
    expect(workspace).toContain('publicHoistPattern: ["@electron-forge/*"]');
    expect(workspace).not.toMatch(/publicHoistPattern:.*(?:react-compose-refs|scheduler)/u);
  });
});
