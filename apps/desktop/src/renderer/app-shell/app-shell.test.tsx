import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";

import type { SystemDiagnostics, TammyDesktopAPI } from "../../shared/desktop-api";
import { App } from "../app";

const diagnostics: SystemDiagnostics = {
  apiVersion: "tammy.v1",
  coreVersion: "0.1.0",
  networkRequired: false,
  runtimeMode: "offline",
};

function installDesktopAPI() {
  window.sessionStorage.setItem("tammy.session.active", "test-session");
  Object.defineProperty(window, "tammy", {
    configurable: true,
    value: Object.freeze({
      createWorkspace: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      confirmRecovery: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      unlockWorkspace: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      signIn: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      createOrganisation: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      createAccount: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      listAccounts: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      postManualJournal: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      listJournals: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      getJournal: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      getTrialBalance: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      ingestDocument: vi.fn(async () => { throw new Error("unavailable"); }),
      listDocuments: vi.fn(async () => { throw new Error("unavailable"); }),
      getDocument: vi.fn(async () => { throw new Error("unavailable"); }),
      saveDocumentReview: vi.fn(async () => { throw new Error("unavailable"); }),
      getAttentionSummary: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      getSystemDiagnostics: vi.fn<TammyDesktopAPI["getSystemDiagnostics"]>().mockResolvedValue(diagnostics),
    } satisfies TammyDesktopAPI),
  });
}

afterEach(() => {
  Reflect.deleteProperty(window, "tammy");
  window.history.replaceState(null, "", "/");
  window.sessionStorage.clear();
  window.localStorage.clear();
});

it("renders the walkthrough app shell", async () => {
  installDesktopAPI();
  const user = userEvent.setup();

  render(<App />);

  expect(await screen.findByText("Local data")).toBeTruthy();
  expect(screen.getByText("Tammy Business")).toBeTruthy();

  const navigation = screen.getByRole("navigation", { name: "Primary" });
  expect(within(navigation).getAllByRole("link").map((link) => link.textContent?.trim())).toEqual([
    "Overview",
    "Documents",
    "Chart of accounts",
    "Journals",
    "General ledger",
    "Trial balance",
    "Audit trail",
    "Settings",
  ]);

  expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
  expect(screen.getByRole("heading", { level: 1, name: "Overview" })).toBeTruthy();
  expect(screen.getByRole("heading", { level: 2, name: "Documents" })).toBeTruthy();
  expect(screen.getByRole("heading", { level: 2, name: "Banking" })).toBeTruthy();
  expect(screen.getByRole("heading", { level: 2, name: "GST & BAS" })).toBeTruthy();
  expect(screen.queryByText("Workspace setup comes next")).toBeNull();

  await user.click(within(navigation).getByRole("link", { name: "Journals" }));
  expect(screen.getByRole("heading", { level: 1, name: "Journals" })).toBeTruthy();
  expect(await screen.findByText("Journals unavailable")).toBeTruthy();
});
