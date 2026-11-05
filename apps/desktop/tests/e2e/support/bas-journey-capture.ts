import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { expect, type Page } from "@playwright/test";

export const basCapturePhase = process.env.TAMMY_BAS_CAPTURE;
if (
  basCapturePhase !== undefined &&
  !["before", "before-ready", "after", "after-compact"].includes(basCapturePhase)
)
  throw new Error("BAS_CAPTURE_PHASE_INVALID");

export async function captureBas(page: Page, name: string, focused = false) {
  if (!basCapturePhase) return;
  const directory = path.resolve(
    import.meta.dirname,
    "../../../../../output/playwright/soft-precision-bas-2026-10-24",
    basCapturePhase,
  );
  await mkdir(directory, { recursive: true });
  if (!focused)
    await page
      .getByRole("heading", { level: 1 })
      .first()
      .evaluate((heading) => {
        for (let node = heading.parentElement; node; node = node.parentElement)
          node.scrollTo({ top: 0, left: 0, behavior: "instant" });
        document.scrollingElement?.scrollTo({ top: 0, left: 0, behavior: "instant" });
      });
  await writeFile(
    path.join(directory, `${name}.png`),
    await page.screenshot({ animations: "disabled" }),
    { flag: "wx" },
  );
}

export async function reviewBasAtNarrowWidths(page: Page) {
  await page.emulateMedia({ reducedMotion: "reduce" });
  for (const width of [900, 390, 320]) {
    await page.setViewportSize({ width, height: 1000 });
    const review = page.getByRole("button", { name: "Review", exact: true });
    await review.focus();
    await page.keyboard.press("Shift+Tab");
    await page.keyboard.press("Tab");
    await expect(review).toBeFocused();
    await expect(review).toHaveCSS("outline-style", "solid");
    await captureBas(page, `05-review-nav-focus-${width}`, true);
    if (!basCapturePhase?.startsWith("before")) {
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      );
      const scroll = page.getByRole("region", {
        name: "BAS calculation labels table",
        exact: true,
      });
      await scroll.focus();
      await page.keyboard.press("Shift+Tab");
      await page.keyboard.press("Tab");
      await expect(scroll).toBeFocused();
      await expect(scroll).toHaveCSS("outline-offset", "-2px");
      await scroll.evaluate((element) => {
        element.scrollLeft = 0;
      });
      await page.keyboard.press("ArrowRight");
      await expect.poll(() => scroll.evaluate((element) => element.scrollLeft)).toBeGreaterThan(0);
      for (let step = 0; step < 16; step += 1) {
        if (
          await scroll.evaluate(
            (element) => element.scrollLeft + element.clientWidth >= element.scrollWidth - 1,
          )
        )
          break;
        await page.keyboard.press("ArrowRight");
      }
      expect(
        await scroll.evaluate(
          (element) => element.scrollLeft + element.clientWidth >= element.scrollWidth - 1,
        ),
      ).toBe(true);
      await scroll.evaluate((element) =>
        element.scrollIntoView({ block: "start", behavior: "instant" }),
      );
      await captureBas(page, `06-calculation-keyboard-${width}`, true);
    }
  }
  await page.setViewportSize({ width: 1440, height: 1100 });
}
