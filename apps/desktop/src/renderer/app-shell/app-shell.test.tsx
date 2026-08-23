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
      importBankStatement: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      listBankStatementLines: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      matchBankStatementLine: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      completeBankReconciliation: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      getBankingSummary: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      ingestDocument: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      listDocuments: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      getDocument: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      saveDocumentReview: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      createBasDraft: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      getCurrentBasDraft: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      getReportingCapability: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      getAttentionSummary: vi.fn(async () => {
        throw new Error("unavailable");
      }),
      getCurrentUser: vi.fn(),
      enrolTotp: vi.fn(),
      confirmTotp: vi.fn(),
      assertTotp: vi.fn(),
      getOrganisation: vi.fn(),
      recordEntityVerification: vi.fn(),
      getSbrReadiness: vi.fn(),
      getMachineCredentialStatus: vi.fn(),
      removeMachineCredential: vi.fn(),
      runSbrReadinessFixture: vi.fn(),
      selectMachineCredentialFile: vi.fn(async () => ({ selected: false as const })),
      importMachineCredential: vi.fn(),
      replaceMachineCredential: vi.fn(),
      unlockMachineCredential: vi.fn(),
      getSystemDiagnostics: vi
        .fn<TammyDesktopAPI["getSystemDiagnostics"]>()
        .mockResolvedValue(diagnostics),
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
