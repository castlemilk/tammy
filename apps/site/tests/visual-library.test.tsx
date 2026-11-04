import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { afterEach, describe, expect, it } from "vitest";
import ToolsPage from "../app/tools/page";
import BlogPage from "../app/blog/page";
import { matchTools, publicTools, toolCategories } from "../lib/tool-library";

afterEach(cleanup);

describe("Tammy practical tools directory", () => {
  it("lists every real tool once with unique, descriptive categories and valid local routes", () => {
    expect(publicTools).toHaveLength(7);
    expect(new Set(publicTools.map((tool) => tool.href)).size).toBe(publicTools.length);
    expect(publicTools.filter((tool) => tool.featured)).toHaveLength(1);
    for (const tool of publicTools) {
      expect(tool.href).toMatch(/^\/tools\/[a-z-]+$/);
      expect(tool.name.length).toBeGreaterThan(7);
      expect(tool.summary.length).toBeGreaterThan(30);
      expect(toolCategories.some((category) => category.id === tool.category)).toBe(true);
      expect(readFileSync("public/assets/" + tool.icon + ".svg", "utf8")).toMatch(/<svg/);
    }
  });

  it("matches by task, key term and category without inventing new results", () => {
    expect(matchTools("", "all")).toHaveLength(7);
    expect(matchTools("GST", "gst")).toHaveLength(2);
    expect(matchTools("markup", "all").map((tool) => tool.href)).toEqual(["/tools/profit-margin-calculator"]);
    expect(matchTools("cash", "invoicing")).toHaveLength(0);
    expect(matchTools("  NET 30  ", "invoicing").map((tool) => tool.href))
      .toEqual(["/tools/invoice-due-date-calculator"]);
  });

  it("filters tool cards accessibly and preserves navigable links", () => {
    render(<ToolsPage />);
    expect(screen.getByRole("status").textContent).toContain("7 tools");
    expect(screen.getByRole("link", { name: /Invoice generator/i })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Tax & GST/i }));
    expect(screen.getByRole("status").textContent).toContain("2 tools");
    expect(screen.getByRole("link", { name: /ABN checker/i })).toBeTruthy();
    expect(screen.queryByRole("link", { name: /Invoice generator/i })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /All tools/i }));
    const search = screen.getByRole("searchbox", { name: "Find a tool" });
    fireEvent.change(search, { target: { value: "contribution" } });
    expect(screen.getByRole("status").textContent).toContain("1 tool");
    expect(screen.getByRole("link", { name: /Break-even calculator/i })).toBeTruthy();
  });

  it("provides an honest empty state and working reset", () => {
    render(<ToolsPage />);
    const search = screen.getByRole("searchbox", { name: "Find a tool" });
    fireEvent.change(search, { target: { value: "no-such-service-xyz" } });
    expect(screen.getByText("No matching tools.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Show all tools" }));
    expect(screen.getByRole("status").textContent).toContain("7 tools");
    expect(screen.getByRole("link", { name: /GST calculator/i })).toBeTruthy();
  });

  it("adds real scalable illustration assets rather than baking in bogus interface copy", () => {
    for (const file of [
      "tammy-ledger-helix.svg",
      "tammy-invoice-fold.svg",
      "tammy-margin-orbit.svg",
    ]) {
      const svg = readFileSync("public/assets/" + file, "utf8");
      expect(svg).toMatch(/<svg\s/);
      expect(svg).toContain("</svg>");
      expect(svg).not.toMatch(/get started free|auto-lodge|bank sync|guaranteed return/i);
      expect(svg.length).toBeLessThan(12_000);
    }
    const { container } = render(<BlogPage />);
    expect(container.querySelector(".blog-feature-watermark")?.getAttribute("src"))
      .toBe("/assets/tammy-margin-orbit.svg");
  });
});
