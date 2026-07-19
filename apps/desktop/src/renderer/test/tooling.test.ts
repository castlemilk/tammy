import { readFile } from "node:fs/promises";
import path from "node:path";
import { loadConfigFromFile, type UserConfig } from "vite";
import { describe, expect, test } from "vitest";
import type { ToolingAliasProbe } from "@/renderer/test/tooling.fixture";

const desktopRoot = process.cwd();
const aliasProbe: ToolingAliasProbe = { resolved: true };

describe("desktop tooling", () => {
  test("keeps the renderer entry local and resolves source aliases", async () => {
    const loadedConfig = await loadConfigFromFile(
      { command: "build", mode: "test" },
      path.join(desktopRoot, "vite.renderer.config.ts"),
    );
    const config = loadedConfig?.config as UserConfig;

    expect(aliasProbe.resolved).toBe(true);
    expect(config.base).toBe("./");
    expect(config.resolve?.alias).toEqual({
      "@": path.join(desktopRoot, "src"),
    });

    const html = await readFile(path.join(desktopRoot, "index.html"), "utf8");
    const document = new DOMParser().parseFromString(html, "text/html");
    const scripts = Array.from(document.querySelectorAll("script"));

    expect(document.title).toBe("Tammy");
    expect(document.querySelector("#root")).not.toBeNull();
    expect(document.querySelector("script[type='module']")?.getAttribute("src")).toBe(
      "/src/renderer/main.tsx",
    );
    expect(scripts.every((script) => !/^https?:\/\//u.test(script.getAttribute("src") ?? ""))).toBe(
      true,
    );
    expect(document.querySelector("meta[http-equiv='Content-Security-Policy']")).toBeNull();
  });
});
