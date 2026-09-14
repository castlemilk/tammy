import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it } from "vitest";

import type { SystemDiagnostics } from "../../shared/conveyor-contract";
import { App as ActualApp } from "../app";
import { createConveyorFixture, renderWithConveyor as render } from "../test/conveyor-fixtures";

const diagnostics: SystemDiagnostics = {
  apiVersion: "tammy.v1",
  coreVersion: "0.1.0",
  networkRequired: false,
  runtimeMode: "offline",
};

let fixture: ReturnType<typeof createConveyorFixture> | undefined;

function App() {
  if (!fixture) throw new Error("installConveyorFixture() must run before rendering App");
  return <ActualApp conveyor={fixture.conveyor} />;
}

function installConveyorFixture() {
  window.sessionStorage.setItem(
    "tammy.session.active",
    JSON.stringify({
      workspaceId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073991",
      userId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
      sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073993",
      organisationId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073994",
      organisationDisplayName: "Tammy Business",
      organisationCanonicalAbn: "51824753556",
      roles: [1],
    }),
  );
  fixture = createConveyorFixture();
  fixture.procedures.system.getSystemDiagnostics.mockResolvedValue(diagnostics);
}

afterEach(() => {
  window.history.replaceState(null, "", "/");
  window.sessionStorage.clear();
  window.localStorage.clear();
});

it("renders the walkthrough app shell", async () => {
  installConveyorFixture();
  const user = userEvent.setup();

  render(<App />);

  expect(await screen.findByText("Local data")).toBeTruthy();
  expect(screen.getByText("Tammy Business")).toBeTruthy();

  const navigation = screen.getByRole("navigation", { name: "Primary" });
  expect(
    within(navigation)
      .getAllByRole("link")
      .map((link) => link.textContent?.trim()),
  ).toEqual([
    "Overview",
    "Documents",
    "Banking",
    "Chart of accounts",
    "Journals",
    "Trial balance",
    "GST & BAS",
    "Audit trail",
    "Settings",
  ]);
  expect(within(navigation).queryByRole("link", { name: "General ledger" })).toBeNull();

  expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
  expect(screen.getByRole("heading", { level: 1, name: "Overview" })).toBeTruthy();
  expect(screen.getByRole("heading", { level: 2, name: "Documents" })).toBeTruthy();
  expect(screen.getByRole("heading", { level: 2, name: "Banking" })).toBeTruthy();
  expect(screen.getByRole("heading", { level: 2, name: "GST & BAS" })).toBeTruthy();
  expect(screen.queryByText("Workspace setup comes next")).toBeNull();

  await user.click(within(navigation).getByRole("link", { name: "Settings" }));
  expect(within(navigation).getByRole("link", { name: "Organisation" })).toBeTruthy();
  expect(within(navigation).getByRole("link", { name: "SBR readiness" })).toBeTruthy();
  await user.click(within(navigation).getByRole("link", { name: "SBR readiness" }));
  expect(await screen.findByText("Workspace details unavailable")).toBeTruthy();
  expect(
    within(navigation).getByRole("link", { name: "SBR readiness" }).getAttribute("aria-current"),
  ).toBe("page");
  expect(
    within(navigation)
      .getAllByRole("link")
      .filter((link) => link.getAttribute("aria-current") === "page"),
  ).toHaveLength(1);
  expect(within(navigation).getByRole("link", { name: "Settings" }).className).toContain(
    "bg-forest",
  );

  await user.click(within(navigation).getByRole("link", { name: "Journals" }));
  expect(screen.getByRole("heading", { level: 1, name: "Journals" })).toBeTruthy();
  expect(await screen.findByText("Journals unavailable")).toBeTruthy();
});
