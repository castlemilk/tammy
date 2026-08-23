import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import {
  FactorState,
  GetCurrentUserRequestSchema,
  GetCurrentUserResponseSchema,
  Role,
  UserSchema,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  EntityVerificationSchema,
  GetOrganisationRequestSchema,
  GetOrganisationResponseSchema,
  OrganisationSchema,
  OrganisationVerificationState,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import {
  GetSbrReadinessRequestSchema,
  GetSbrReadinessResponseSchema,
  MachineCredentialState,
  ProductIdState,
  SbrEnvironment,
  SbrReadinessSchema,
  SbrReadinessState,
} from "@tammy/connect-client/tammy/v1/sbr_pb.js";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { SystemDiagnostics, TammyDesktopAPI } from "../shared/desktop-api";
import { createProtoMethodCodec } from "../shared/proto-ipc";
import { App } from "./app";

const diagnostics: SystemDiagnostics = {
  apiVersion: "tammy.v1",
  coreVersion: "0.1.0",
  runtimeMode: "offline",
  networkRequired: false,
};

const rendererStyles = readFileSync(resolve(process.cwd(), "src/renderer/styles.css"), "utf8");
const currentUserCodec = createProtoMethodCodec({
  input: GetCurrentUserRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetCurrentUserResponseSchema,
});
const organisationCodec = createProtoMethodCodec({
  input: GetOrganisationRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetOrganisationResponseSchema,
});
const readinessCodec = createProtoMethodCodec({
  input: GetSbrReadinessRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetSbrReadinessResponseSchema,
});

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

function mockAuthoritativeSettings(
  roles: readonly Role[],
  organisationDisplayName = "Authoritative Tammy Business",
) {
  vi.mocked(window.tammy.getCurrentUser).mockImplementation(async (frame) => {
    const request = currentUserCodec.decodeRequest(frame);
    expect(request.authentication).toMatchObject({
      actorUserId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
      sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073993",
    });
    return currentUserCodec.encodeResponse(
      create(GetCurrentUserResponseSchema, {
        user: create(UserSchema, {
          id: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
          username: "tammy-user",
          displayName: "Tammy User",
          roles: [...roles],
          factorState: FactorState.ENABLED,
        }),
      }),
    );
  });
  vi.mocked(window.tammy.getOrganisation).mockImplementation(async (frame) => {
    const request = organisationCodec.decodeRequest(frame);
    expect(request.organisationId).toBe("01900f3c-7b2e-7cc4-98c4-dc0c0c073994");
    return organisationCodec.encodeResponse(
      create(GetOrganisationResponseSchema, {
        organisation: create(OrganisationSchema, {
          id: request.organisationId,
          displayName: organisationDisplayName,
          legalName: "Authoritative legal name",
          abn: "51824753556",
          entityType: "company",
          version: 1n,
          verificationState: OrganisationVerificationState.UNVERIFIED,
        }),
      }),
    );
  });
}

function mockUnavailableReadiness() {
  vi.mocked(window.tammy.getSbrReadiness).mockImplementation(async (frame) => {
    readinessCodec.decodeRequest(frame);
    return readinessCodec.encodeResponse(
      create(GetSbrReadinessResponseSchema, {
        readiness: create(SbrReadinessSchema, {
          environment: SbrEnvironment.EVTE,
          state: SbrReadinessState.UNAVAILABLE,
          machineCredentialState: MachineCredentialState.PRESENT,
          productIdState: ProductIdState.MISSING,
        }),
      }),
    );
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

  it("runs the authenticated doctor route once without exposing another capability", async () => {
    installDesktopAPI(vi.fn().mockResolvedValue(diagnostics));
    mockAuthoritativeSettings([Role.WORKSPACE_ADMIN]);
    vi.mocked(window.tammy.getSbrReadiness).mockRejectedValue(new Error("unavailable"));
    window.history.replaceState(null, "", "/settings/sbr?doctor=1");

    render(<App />);

    expect(await screen.findByRole("heading", { level: 1, name: "SBR readiness" })).toBeTruthy();
    await screen.findByText("Readiness unavailable");
    expect(window.tammy.getSbrReadiness).toHaveBeenCalledOnce();
    expect(window.location.search).toBe("?doctor=1");
    expect(screen.queryByRole("button", { name: /doctor/i })).toBeNull();
  });

  it("refreshes cached roles and organisation before rendering privileged SBR settings", async () => {
    installDesktopAPI(vi.fn().mockResolvedValue(diagnostics));
    mockAuthoritativeSettings([Role.BUSINESS_PREPARER]);
    mockUnavailableReadiness();
    window.history.replaceState(null, "", "/settings/sbr");

    render(
      <StrictMode>
        <App />
      </StrictMode>,
    );

    expect(await screen.findByText("Authoritative Tammy Business · ABN 51824753556")).toBeTruthy();
    expect(window.tammy.getCurrentUser).toHaveBeenCalledOnce();
    expect(window.tammy.getOrganisation).toHaveBeenCalledOnce();
    expect(screen.queryByText("Tammy Business · ABN 51824753556")).toBeNull();
    expect(screen.queryByRole("button", { name: /machine credential/i })).toBeNull();
    expect(JSON.parse(window.sessionStorage.getItem("tammy.session.active") ?? "{}")).toMatchObject(
      {
        organisationDisplayName: "Authoritative Tammy Business",
        roles: [Role.BUSINESS_PREPARER],
      },
    );
  });

  it("fails closed when authoritative settings projections cannot be refreshed", async () => {
    installDesktopAPI(vi.fn().mockResolvedValue(diagnostics));
    vi.mocked(window.tammy.getCurrentUser).mockRejectedValue(new Error("unavailable"));
    vi.mocked(window.tammy.getOrganisation).mockRejectedValue(new Error("unavailable"));
    window.history.replaceState(null, "", "/settings/organisation");

    render(<App />);

    expect(await screen.findByText("Workspace details unavailable")).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "Organisation" })).toBeNull();
    expect(document.body.textContent).not.toContain("51824753556");
  });

  it("fails closed when retained verification expiry metadata is malformed", async () => {
    installDesktopAPI(vi.fn().mockResolvedValue(diagnostics));
    mockAuthoritativeSettings([Role.WORKSPACE_ADMIN]);
    vi.mocked(window.tammy.getOrganisation).mockImplementation(async (frame) => {
      const request = organisationCodec.decodeRequest(frame);
      return organisationCodec.encodeResponse(
        create(GetOrganisationResponseSchema, {
          organisation: create(OrganisationSchema, {
            id: request.organisationId,
            displayName: "Authoritative Tammy Business",
            legalName: "Authoritative legal name",
            abn: "51824753556",
            entityType: "company",
            version: 1n,
            verificationState: OrganisationVerificationState.EXPIRED,
          }),
          currentVerification: create(EntityVerificationSchema, {
            id: "01900f3c-7b2e-7cc4-98c4-dc0c0c073995",
            organisationId: request.organisationId,
            state: OrganisationVerificationState.VERIFIED,
            expiresAt: create(TimestampSchema, { seconds: 1n, nanos: 1_000_000_000 }),
          }),
        }),
      );
    });
    window.history.replaceState(null, "", "/settings/organisation");
    render(<App />);
    expect(await screen.findByText("Workspace details unavailable")).toBeTruthy();
    expect(document.body.textContent).not.toContain("Invalid Date");
  });

  it("wraps an authoritative 256-character organisation display name", async () => {
    installDesktopAPI(vi.fn().mockResolvedValue(diagnostics));
    const displayName = "W".repeat(256);
    mockAuthoritativeSettings([Role.WORKSPACE_ADMIN], displayName);
    window.history.replaceState(null, "", "/settings/organisation");

    render(<App />);

    const value = await screen.findByText(displayName);
    expect(value.className).toContain("min-w-0");
    expect(value.className).toContain("[overflow-wrap:anywhere]");
  });

  it("renders independent verification on the authenticated organisation route", async () => {
    installDesktopAPI(vi.fn().mockResolvedValue(diagnostics));
    mockAuthoritativeSettings([Role.WORKSPACE_ADMIN]);
    window.history.replaceState(null, "", "/settings/organisation");
    render(<App />);
    expect(
      await screen.findByRole("heading", { name: "Independent entity verification" }),
    ).toBeTruthy();
    expect(screen.getByLabelText("Independent evidence")).toBeTruthy();
  });

  it("ignores an authoritative settings refresh that resolves after unmount", async () => {
    installDesktopAPI(vi.fn().mockResolvedValue(diagnostics));
    const currentUser = deferred<Uint8Array>();
    const organisation = deferred<Uint8Array>();
    vi.mocked(window.tammy.getCurrentUser).mockReturnValue(currentUser.promise);
    vi.mocked(window.tammy.getOrganisation).mockReturnValue(organisation.promise);
    window.history.replaceState(null, "", "/settings/organisation");

    const view = render(<App />);
    await waitFor(() => expect(window.tammy.getCurrentUser).toHaveBeenCalledOnce());
    view.unmount();

    await act(async () => {
      currentUser.resolve(
        currentUserCodec.encodeResponse(
          create(GetCurrentUserResponseSchema, {
            user: create(UserSchema, {
              id: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
              roles: [Role.BUSINESS_PREPARER],
            }),
          }),
        ),
      );
      organisation.resolve(
        organisationCodec.encodeResponse(
          create(GetOrganisationResponseSchema, {
            organisation: create(OrganisationSchema, {
              id: "01900f3c-7b2e-7cc4-98c4-dc0c0c073994",
              displayName: "Late authority response",
              abn: "51824753556",
            }),
          }),
        ),
      );
      await Promise.all([currentUser.promise, organisation.promise]);
    });

    expect(JSON.parse(window.sessionStorage.getItem("tammy.session.active") ?? "{}")).toMatchObject(
      {
        organisationDisplayName: "Tammy Business",
        roles: [Role.WORKSPACE_ADMIN],
      },
    );
  });

  it("rejects a doctor fragment at the app boundary before any settings RPC", async () => {
    installDesktopAPI(vi.fn().mockResolvedValue(diagnostics));
    window.history.replaceState(null, "", "/settings/sbr?doctor=1#fragment");

    render(<App />);

    expect(await screen.findByRole("heading", { level: 1, name: "Overview" })).toBeTruthy();
    expect(window.location.pathname).toBe("/overview");
    expect(window.location.search).toBe("");
    expect(window.location.hash).toBe("");
    expect(window.tammy.getCurrentUser).not.toHaveBeenCalled();
    expect(window.tammy.getSbrReadiness).not.toHaveBeenCalled();
  });

  it("rejects a tampered retained role projection instead of authenticating it", () => {
    installDesktopAPI(vi.fn().mockResolvedValue(diagnostics));
    window.sessionStorage.setItem(
      "tammy.session.active",
      JSON.stringify({
        workspaceId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073991",
        userId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
        sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073993",
        organisationId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073994",
        organisationDisplayName: "Tammy Business",
        organisationCanonicalAbn: "51824753556",
        roles: ["workspace_admin"],
      }),
    );

    render(<App />);

    expect(screen.getByRole("heading", { name: "Create your local workspace" })).toBeTruthy();
    expect(screen.queryByRole("navigation", { name: "Primary" })).toBeNull();
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
