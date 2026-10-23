import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { HorizontalScrollArea } from "./horizontal-scroll-area";

it("keeps read-only tabular content in a named keyboard focus target", () => {
  render(
    <HorizontalScrollArea label="Invoice amounts">
      <table>
        <tbody>
          <tr>
            <td>$1,320.00</td>
          </tr>
        </tbody>
      </table>
    </HorizontalScrollArea>,
  );
  const region = screen.getByRole("region", { name: "Invoice amounts" });
  region.focus();
  expect(document.activeElement).toBe(region);
  expect(region.contains(screen.getByRole("table"))).toBe(true);
});

it("moves overflowing content with unmodified horizontal keys on the region itself", () => {
  render(<HorizontalScrollArea label="Document amounts">Retained data</HorizontalScrollArea>);
  const region = screen.getByRole("region", { name: "Document amounts" });
  Object.defineProperties(region, {
    clientWidth: { value: 200 },
    scrollWidth: { value: 650 },
    scrollBy: {
      value: vi.fn((options: ScrollToOptions) => {
        region.scrollLeft += options.left ?? 0;
      }),
    },
  });
  region.focus();
  expect(fireEvent.keyDown(region, { key: "ArrowRight" })).toBe(false);
  expect(region.scrollLeft).toBeGreaterThan(0);
  expect(fireEvent.keyDown(region, { key: "ArrowLeft" })).toBe(false);
  expect(region.scrollLeft).toBe(0);
});

it("preserves child controls, modified keys, vertical navigation and non-overflowing content", () => {
  render(
    <HorizontalScrollArea label="Document amounts">
      <input aria-label="Note" />
    </HorizontalScrollArea>,
  );
  const region = screen.getByRole("region", { name: "Document amounts" });
  const scrollBy = vi.fn();
  Object.defineProperties(region, {
    clientWidth: { value: 200 },
    scrollWidth: { value: 650, configurable: true },
    scrollBy: { value: scrollBy },
  });
  expect(fireEvent.keyDown(screen.getByLabelText("Note"), { key: "ArrowRight" })).toBe(true);
  for (const modifier of ["altKey", "ctrlKey", "metaKey", "shiftKey"])
    expect(fireEvent.keyDown(region, { key: "ArrowRight", [modifier]: true })).toBe(true);
  expect(fireEvent.keyDown(region, { key: "ArrowDown" })).toBe(true);
  Object.defineProperty(region, "scrollWidth", { value: 200 });
  expect(fireEvent.keyDown(region, { key: "ArrowRight" })).toBe(true);
  expect(scrollBy).not.toHaveBeenCalled();
});
