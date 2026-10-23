import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import { WorkspaceEntryLayout } from "./workspace-entry-layout";

it("shows the current recovery step and keeps the content heading in the form panel", () => {
  render(
    <WorkspaceEntryLayout
      description="Keep this code safe."
      onPrivacy={vi.fn()}
      stage="recovery"
      title="Save your recovery code"
    >
      <p>Recovery details</p>
    </WorkspaceEntryLayout>,
  );

  expect(screen.getByRole("heading", { name: "Save your recovery code", level: 1 })).toBeTruthy();
  expect(screen.getByRole("list", { name: "Workspace setup steps" })).toBeTruthy();
  expect(screen.getByText("Save recovery code").closest("li")?.getAttribute("aria-current")).toBe(
    "step",
  );
  expect(screen.getByRole("button", { name: "Privacy and support" })).toBeTruthy();
});

it("keeps unlock focused on opening the saved workspace instead of showing setup progress", () => {
  render(
    <WorkspaceEntryLayout
      description="Your data stays on this device."
      stage="unlock"
      title="Unlock your workspace"
    >
      <p>Unlock fields</p>
    </WorkspaceEntryLayout>,
  );

  expect(screen.getByRole("heading", { name: "Unlock your workspace", level: 1 })).toBeTruthy();
  expect(screen.queryByRole("list", { name: "Workspace setup steps" })).toBeNull();
  expect(screen.getByText("Unlock fields")).toBeTruthy();
});
