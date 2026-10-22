import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
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
