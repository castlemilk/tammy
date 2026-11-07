import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { CashFlowForecast } from "../components/cash-flow-forecast";

afterEach(cleanup);
const sample = () => screen.getByRole("button", { name: "Use sample figures" });
const confirmation = () => screen.queryByRole("group", { name: "Replace your forecast with sample figures?" });
const opening = () => screen.getByLabelText("Available opening cash (AUD)") as HTMLInputElement;
function enterMovement() {
  fireEvent.click(screen.getByRole("button", { name: "Add first movement" }));
  fireEvent.change(screen.getByLabelText("Description"), { target: { value: "Customer balance" } });
  fireEvent.change(screen.getByLabelText("Amount (AUD)"), { target: { value: "250.19" } });
}

describe("Keeping a forecast when exploring sample figures", () => {
  it("protects an edited opening balance even with no movements and lets Escape keep it", () => {
    render(<CashFlowForecast />);
    fireEvent.change(opening(), { target: { value: "9750.37" } });
    fireEvent.click(sample());
    expect(confirmation()).toBeTruthy();
    expect(opening().value).toBe("9750.37");
    const keep = screen.getByRole("button", { name: "Keep my forecast" });
    expect(document.activeElement).toBe(keep);
    fireEvent.keyDown(keep, { key: "Escape" });
    expect(confirmation()).toBeNull();
    expect(opening().value).toBe("9750.37");
    expect(document.activeElement).toBe(sample());
    expect(screen.queryByLabelText("Description")).toBeNull();
  });

  it("keeps entered movements unchanged when replacement is declined", () => {
    render(<CashFlowForecast />);
    enterMovement();
    fireEvent.click(sample());
    expect(screen.getByText(/1 entered movement/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Keep my forecast" }));
    expect(confirmation()).toBeNull();
    expect(screen.getByDisplayValue("Customer balance")).toBeTruthy();
    expect(screen.getByDisplayValue("250.19")).toBeTruthy();
    expect(document.activeElement).toBe(sample());
  });

  it("replaces figures only after confirmation and preserves the chosen start date", () => {
    const { container } = render(<CashFlowForecast />);
    fireEvent.change(screen.getByLabelText("First week begins"), { target: { value: "2026-11-07" } });
    fireEvent.change(opening(), { target: { value: "9750.37" } });
    enterMovement();
    fireEvent.click(sample());
    expect(screen.getByDisplayValue("Customer balance")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Replace with sample" }));
    expect(confirmation()).toBeNull();
    expect(opening().value).toBe("12000.00");
    expect((screen.getByLabelText("First week begins") as HTMLInputElement).value).toBe("2026-11-07");
    expect(screen.getAllByLabelText("Description")).toHaveLength(4);
    expect(screen.queryByDisplayValue("Customer balance")).toBeNull();
    expect(screen.getByDisplayValue("Customer payment")).toBeTruthy();
    expect(container.querySelectorAll("tbody tr")).toHaveLength(12);
    expect(screen.getByText(/Illustrative scenario loaded/)).toBeTruthy();
    expect(document.activeElement).toBe(sample());
  });

  it("dismisses a pending replacement when a movement is edited", () => {
    render(<CashFlowForecast />);
    enterMovement();
    fireEvent.click(sample());
    fireEvent.change(screen.getByLabelText("Amount (AUD)"), { target: { value: "500.39" } });
    expect(confirmation()).toBeNull();
    expect(screen.getByDisplayValue("500.39")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Replace with sample" })).toBeNull();
    fireEvent.click(sample());
    expect(confirmation()).toBeTruthy();
    expect(screen.getByDisplayValue("500.39")).toBeTruthy();
  });

  it("cannot load a sample without a valid forecast date", () => {
    render(<CashFlowForecast />);
    fireEvent.change(screen.getByLabelText("First week begins"), { target: { value: "" } });
    expect((sample() as HTMLButtonElement).disabled).toBe(true);
    expect(confirmation()).toBeNull();
  });
});
