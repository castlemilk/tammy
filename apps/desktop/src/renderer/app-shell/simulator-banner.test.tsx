import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { SimulatorBanner } from "./simulator-banner";

describe("SimulatorBanner", () => {
  it("shows a persistent no-lodgment warning for simulator authority", () => {
    render(<SimulatorBanner scenario="sbr-simulator" />);
    expect(screen.getByRole("status").textContent).toBe(
      "SIMULATOR — NOT FOR ATO LODGMENT",
    );
  });

  it("does not label ordinary accounting as a simulator", () => {
    const { container } = render(<SimulatorBanner scenario="accounting" />);
    expect(container.innerHTML).toBe("");
  });
});
