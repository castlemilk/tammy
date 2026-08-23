import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { SystemDiagnostics, TammyDesktopAPI } from "../shared/desktop-api";
import { App } from "./app";

const diagnostics: SystemDiagnostics = {
  apiVersion: "tammy.v1",
  coreVersion: "0.1.0",
  runtimeMode: "offline",
  networkRequired: false,
};

const rendererStyles = readFileSync(resolve(process.cwd(), "src/renderer/styles.css"), "utf8");

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve;
    reject = promiseReject;
  });

  return { promise, reject, resolve };
}

function installDesktopAPI(getSystemDiagnostics: TammyDesktopAPI["getSystemDiagnostics"]) {
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
      removeSbrProductId: vi.fn(),
      runSbrReadinessFixture: vi.fn(),
      selectMachineCredentialFile: vi.fn(async () => ({ selected: false as const })),
      importMachineCredential: vi.fn(),
      replaceMachineCredential: vi.fn(),
      unlockMachineCredential: vi.fn(),
      importSbrProductId: vi.fn(),
      getSystemDiagnostics,
    } satisfies TammyDesktopAPI),
  });
}

afterEach(() => {
  Reflect.deleteProperty(window, "tammy");
  window.history.replaceState(null, "", "/");
  window.sessionStorage.clear();
  window.localStorage.clear();
});

describe("App", () => {
  it("shows privacy and support information before workspace setup", () => {
    installDesktopAPI(vi.fn().mockResolvedValue(diagnostics));
    window.sessionStorage.clear();
    window.history.replaceState(null, "", "/privacy");

    render(<App />);

    expect(screen.getByRole("heading", { level: 1, name: "Privacy and support" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Back to Tammy" })).toBeTruthy();
  });

  it("announces the local engine while diagnostics are loading", () => {
    const pending = deferred<SystemDiagnostics>();
    installDesktopAPI(vi.fn(() => pending.promise));

    render(<App />);

    const status = screen.getByRole("status");
    expect(status.getAttribute("aria-live")).toBe("polite");
    expect(status.textContent).toContain("Starting local engine");
  });

  it("shows the offline-ready accounting shell without unavailable modules", async () => {
    installDesktopAPI(vi.fn().mockResolvedValue(diagnostics));

    render(<App />);

    await screen.findByText(/Local engine ready/);
    const status = screen.getByRole("status");
    expect(status.getAttribute("data-startup-transition")).toBe("starting-to-ready");
    expect(status.textContent).toContain("Offline");
    expect(status.textContent).toContain("No cloud required");

    expect(screen.queryByText("Workspace setup comes next")).toBeNull();
    const navigation = screen.getByRole("navigation", { name: "Primary" });
    expect(within(navigation).getByRole("link", { name: "Overview" })).toBeTruthy();

    expect(within(navigation).getByRole("link", { name: "Documents" })).toBeTruthy();
    expect(within(navigation).getByRole("link", { name: "Banking" })).toBeTruthy();
    expect(within(navigation).getByRole("link", { name: "GST & BAS" })).toBeTruthy();
    expect(within(navigation).queryByRole("link", { name: "General ledger" })).toBeNull();
    for (const futureModule of ["Submissions", "Lodge BAS"]) {
      expect(screen.queryByText(futureModule)).toBeNull();
    }
  });

  it("canonicalizes a rejected authenticated deep link to Overview", async () => {
    installDesktopAPI(vi.fn().mockResolvedValue(diagnostics));
    window.history.replaceState(null, "", "/accounting/general-ledger");

    render(<App />);

    expect(screen.getByRole("heading", { level: 1, name: "Overview" })).toBeTruthy();
    await waitFor(() => expect(window.location.pathname).toBe("/overview"));
  });

  it("canonicalizes rejected popstate routes while preserving valid history paths", async () => {
    installDesktopAPI(vi.fn().mockResolvedValue(diagnostics));
    window.history.replaceState(null, "", "/overview");
    render(<App />);

    act(() => {
      window.history.pushState(null, "", "/documents");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(await screen.findByRole("heading", { level: 1, name: "Documents" })).toBeTruthy();
    expect(window.location.pathname).toBe("/documents");

    act(() => {
      window.history.pushState(null, "", "/accounting/general-ledger");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(await screen.findByRole("heading", { level: 1, name: "Overview" })).toBeTruthy();
    await waitFor(() => expect(window.location.pathname).toBe("/overview"));
  });

  it("keeps valid 128-character version values intact in wrap-safe cells", async () => {
    const longApiVersion = "a".repeat(128);
    const longCoreVersion = "9".repeat(128);
    installDesktopAPI(
      vi.fn().mockResolvedValue({
        ...diagnostics,
        apiVersion: longApiVersion,
        coreVersion: longCoreVersion,
      } satisfies SystemDiagnostics),
    );

    render(<App />);
    const user = userEvent.setup();
    await user.click(screen.getByRole("link", { name: "Settings" }));

    for (const version of [longApiVersion, longCoreVersion]) {
      const value = await screen.findByText(version);
      expect(value.textContent).toBe(version);
      expect(value.classList.contains("min-w-0")).toBe(true);
      expect(value.classList.contains("[overflow-wrap:anywhere]")).toBe(true);
      expect(value.parentElement?.classList.contains("min-w-0")).toBe(true);
    }
  });

  it("keeps failure copy safe and retries through the typed desktop method", async () => {
    const retry = deferred<SystemDiagnostics>();
    const getSystemDiagnostics = vi
      .fn<TammyDesktopAPI["getSystemDiagnostics"]>()
      .mockRejectedValueOnce(
        new Error("capability=secret-token certificatePin=sha256:raw 127.0.0.1:45000 readiness"),
      )
      .mockImplementationOnce(() => retry.promise);
    installDesktopAPI(getSystemDiagnostics);
    const user = userEvent.setup();

    render(<App />);

    await screen.findByText(/Local engine unavailable/, { selector: "div" });
    let status = screen.getByRole("status");
    expect(document.body.textContent).not.toContain("secret-token");
    expect(document.body.textContent).not.toContain("certificatePin");
    expect(document.body.textContent).not.toContain("127.0.0.1");
    expect(document.body.textContent).not.toContain("readiness");

    const retryButton = screen.getByRole("button", { name: "Retry local engine" });
    await user.click(retryButton);
    expect(getSystemDiagnostics).toHaveBeenCalledTimes(2);
    status = screen.getByRole("status");
    expect(status.textContent).toContain("Starting local engine");

    retry.resolve(diagnostics);
    await screen.findByText(/Local engine ready/);
  });

  it("uses semantic landmarks, heading order, and keyboard focus order", async () => {
    installDesktopAPI(vi.fn().mockRejectedValue(new Error("unavailable")));
    const user = userEvent.setup();

    render(<App />);

    expect(screen.getByRole("navigation", { name: "Primary" })).toBeTruthy();
    expect(screen.getByRole("main")).toBeTruthy();
    expect(screen.getByRole("heading", { level: 1, name: "Overview" })).toBeTruthy();
    expect(screen.getByRole("heading", { level: 2, name: "Documents" })).toBeTruthy();

    await screen.findByRole("button", { name: "Retry local engine" });
    await user.tab();
    expect(document.activeElement).toBe(screen.getByRole("link", { name: "Overview" }));
    await user.tab();
    expect(document.activeElement).toBe(screen.getByRole("link", { name: "Documents" }));
    await user.tab();
    expect(document.activeElement).toBe(screen.getByRole("link", { name: "Banking" }));
  });
});

describe("renderer semantic styles", () => {
  it("defines the separator background utility against the border token", () => {
    expect(rendererStyles).toMatch(/\.bg-border\s*{\s*background-color:\s*var\(--border\);\s*}/);
  });
});
