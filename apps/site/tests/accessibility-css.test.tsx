import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const css = readFileSync("app/globals.css", "utf8");

describe("public site accessibility CSS", () => {
  it("keeps the wordmark home link at least 44px tall", () => {
    expect(css).toMatch(/\.wordmark\s*\{[^}]*min-height:\s*44px/);
  });
});
