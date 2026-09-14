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
  OrganisationVerificationSummarySchema,
  RecordEntityVerificationRequestSchema,
  RecordEntityVerificationResponseSchema,
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
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { SystemDiagnostics } from "../shared/conveyor-contract";
import { createProtoMethodCodec } from "../shared/proto-ipc";
import { App as ActualApp } from "./app";
import {
  type ConveyorFixtureProcedures,
  createConveyorFixture,
  renderWithConveyor as render,
} from "./test/conveyor-fixtures";

const diagnostics: SystemDiagnostics = {
  apiVersion: "tammy.v1",
  coreVersion: "0.1.0",
  runtimeMode: "offline",
  networkRequired: false,
};

const rendererStyles = readFileSync(resolve(process.cwd(), "src/app/styles.css"), "utf8");
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
const recordVerificationCodec = createProtoMethodCodec({
  input: RecordEntityVerificationRequestSchema,
  maximumRequestBytes: Math.floor(1.1 * 1024 * 1024),
  maximumResponseBytes: 32_768,
  output: RecordEntityVerificationResponseSchema,
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

let fixture: ReturnType<typeof createConveyorFixture> | undefined;

function App() {
  if (!fixture) throw new Error("installConveyorFixture() must run before rendering App");
  return <ActualApp conveyor={fixture.conveyor} />;
}

function procedures(): ConveyorFixtureProcedures {
  if (!fixture) throw new Error("installConveyorFixture() must run before configuring procedures");
  return fixture.procedures;
}

function installConveyorFixture(getSystemDiagnostics: () => Promise<SystemDiagnostics>) {
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
  fixture.procedures.system.getSystemDiagnostics.mockImplementation(getSystemDiagnostics);
  fixture.procedures.sbr.selectMachineCredentialFile.mockResolvedValue({ selected: false });
}

function mockAuthoritativeSettings(
  roles: readonly Role[],
  organisationDisplayName = "Authoritative Tammy Business",
) {
  procedures().organisation.getCurrentUser.mockImplementation(async (frame) => {
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
  procedures().organisation.getOrganisation.mockImplementation(async (frame) => {
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
  procedures().sbr.getSbrReadiness.mockImplementation(async (frame) => {
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
  fixture?.queryClient.clear();
  fixture = undefined;
  Reflect.deleteProperty(window, "tammyLaunchScenario");
  window.history.replaceState(null, "", "/");
  window.sessionStorage.clear();
  window.localStorage.clear();
});

describe("App", () => {
  it("keeps the simulator warning visible and opens authenticated doctor readiness", async () => {
    installConveyorFixture(vi.fn().mockResolvedValue(diagnostics));
    mockAuthoritativeSettings([Role.WORKSPACE_ADMIN]);
    mockUnavailableReadiness();
    Object.defineProperty(window, "tammyLaunchScenario", {
      configurable: true,
      value: "sbr-doctor",
    });

    render(<App />);

    expect(screen.getByText("SIMULATOR — NOT FOR ATO LODGMENT")).toBeTruthy();
    expect(await screen.findByRole("heading", { level: 1, name: "SBR readiness" })).toBeTruthy();
    expect(window.location.pathname).toBe("/settings/sbr");
    expect(window.location.search).toBe("?doctor=1");
  });

  it("shows privacy and support information before workspace setup", () => {
    installConveyorFixture(vi.fn().mockResolvedValue(diagnostics));
    window.sessionStorage.clear();
    window.history.replaceState(null, "", "/privacy");

    render(<App />);

    expect(screen.getByRole("heading", { level: 1, name: "Privacy and support" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Back to Tammy" })).toBeTruthy();
  });

  it("announces the local engine while diagnostics are loading", () => {
    const pending = deferred<SystemDiagnostics>();
    installConveyorFixture(vi.fn(() => pending.promise));

    render(<App />);

    const status = screen.getByRole("status");
    expect(status.getAttribute("aria-live")).toBe("polite");
    expect(status.textContent).toContain("Starting local engine");
  });

  it("shows the offline-ready accounting shell without unavailable modules", async () => {
    installConveyorFixture(vi.fn().mockResolvedValue(diagnostics));

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
    installConveyorFixture(vi.fn().mockResolvedValue(diagnostics));
    window.history.replaceState(null, "", "/accounting/general-ledger");

    render(<App />);

    expect(screen.getByRole("heading", { level: 1, name: "Overview" })).toBeTruthy();
    await waitFor(() => expect(window.location.pathname).toBe("/overview"));
  });

  it("runs the authenticated doctor route once without exposing another capability", async () => {
    installConveyorFixture(vi.fn().mockResolvedValue(diagnostics));
    mockAuthoritativeSettings([Role.WORKSPACE_ADMIN]);
    procedures().sbr.getSbrReadiness.mockRejectedValue(new Error("unavailable"));
    window.history.replaceState(null, "", "/settings/sbr?doctor=1");

    render(<App />);

    expect(await screen.findByRole("heading", { level: 1, name: "SBR readiness" })).toBeTruthy();
    await screen.findByText("Readiness unavailable");
    expect(procedures().sbr.getSbrReadiness).toHaveBeenCalledOnce();
    expect(window.location.search).toBe("?doctor=1");
    expect(screen.queryByRole("button", { name: /doctor/i })).toBeNull();
  });

  it("refreshes cached roles and organisation before rendering privileged SBR settings", async () => {
    installConveyorFixture(vi.fn().mockResolvedValue(diagnostics));
    mockAuthoritativeSettings([Role.BUSINESS_PREPARER]);
    mockUnavailableReadiness();
    window.history.replaceState(null, "", "/settings/sbr");

    render(
      <StrictMode>
        <App />
      </StrictMode>,
    );

    expect(await screen.findByText("Authoritative Tammy Business · ABN 51824753556")).toBeTruthy();
    expect(procedures().organisation.getCurrentUser).toHaveBeenCalledOnce();
    expect(procedures().organisation.getOrganisation).toHaveBeenCalledOnce();
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
    installConveyorFixture(vi.fn().mockResolvedValue(diagnostics));
    procedures().organisation.getCurrentUser.mockRejectedValue(new Error("unavailable"));
    procedures().organisation.getOrganisation.mockRejectedValue(new Error("unavailable"));
    window.history.replaceState(null, "", "/settings/organisation");

    render(<App />);

    expect(await screen.findByText("Workspace details unavailable")).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "Organisation" })).toBeNull();
    expect(document.body.textContent).not.toContain("51824753556");
  });

  it("fails closed when retained verification expiry metadata is malformed", async () => {
    installConveyorFixture(vi.fn().mockResolvedValue(diagnostics));
    mockAuthoritativeSettings([Role.WORKSPACE_ADMIN]);
    procedures().organisation.getOrganisation.mockImplementation(async (frame) => {
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
          currentVerification: create(OrganisationVerificationSummarySchema, {
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
    installConveyorFixture(vi.fn().mockResolvedValue(diagnostics));
    const displayName = "W".repeat(256);
    mockAuthoritativeSettings([Role.WORKSPACE_ADMIN], displayName);
    window.history.replaceState(null, "", "/settings/organisation");

    render(<App />);

    const value = await screen.findByText(displayName);
    expect(value.className).toContain("min-w-0");
    expect(value.className).toContain("[overflow-wrap:anywhere]");
  });

  it("renders independent verification on the authenticated organisation route", async () => {
    installConveyorFixture(vi.fn().mockResolvedValue(diagnostics));
    mockAuthoritativeSettings([Role.WORKSPACE_ADMIN]);
    window.history.replaceState(null, "", "/settings/organisation");
    render(<App />);
    expect(
      await screen.findByRole("heading", { name: "Independent entity verification" }),
    ).toBeTruthy();
    expect(screen.getByLabelText("Independent evidence")).toBeTruthy();
  });

  it("keeps successful verification visible while refreshing authoritative settings", async () => {
    installConveyorFixture(vi.fn().mockResolvedValue(diagnostics));
    mockAuthoritativeSettings([Role.WORKSPACE_ADMIN]);
    procedures().organisation.recordEntityVerification.mockImplementation(async (frame) => {
      const request = recordVerificationCodec.decodeRequest(frame);
      return recordVerificationCodec.encodeResponse(
        create(RecordEntityVerificationResponseSchema, {
          organisation: create(OrganisationSchema, {
            id: request.organisationId,
            version: 2n,
            verificationState: OrganisationVerificationState.VERIFIED,
          }),
          verification: create(EntityVerificationSchema, {
            id: "01900f3c-7b2e-7cc4-98c4-dc0c0c073995",
            organisationId: request.organisationId,
            state: OrganisationVerificationState.VERIFIED,
            expiresAt: create(TimestampSchema, { seconds: 1_800_000_000n }),
          }),
        }),
      );
    });
    window.history.replaceState(null, "", "/settings/organisation");
    const user = userEvent.setup();
    render(<App />);
    await screen.findByRole("heading", { name: "Independent entity verification" });

    const refreshedUser = deferred<Uint8Array>();
    const refreshedOrganisation = deferred<Uint8Array>();
    procedures().organisation.getCurrentUser.mockReturnValueOnce(refreshedUser.promise);
    procedures().organisation.getOrganisation.mockReturnValueOnce(refreshedOrganisation.promise);
    await user.upload(
      screen.getByLabelText("Independent evidence"),
      new File([new Uint8Array([1])], "evidence.pdf", { type: "application/pdf" }),
    );
    await user.click(screen.getByRole("button", { name: "Record verification" }));
    await waitFor(() => expect(procedures().organisation.getOrganisation).toHaveBeenCalledTimes(2));
    expect(screen.getByText("Entity verification evidence recorded.")).toBeTruthy();
  });

  it("ignores an authoritative settings refresh that resolves after unmount", async () => {
    installConveyorFixture(vi.fn().mockResolvedValue(diagnostics));
    const currentUser = deferred<Uint8Array>();
    const organisation = deferred<Uint8Array>();
    procedures().organisation.getCurrentUser.mockReturnValue(currentUser.promise);
    procedures().organisation.getOrganisation.mockReturnValue(organisation.promise);
    window.history.replaceState(null, "", "/settings/organisation");

    const view = render(<App />);
    await waitFor(() => expect(procedures().organisation.getCurrentUser).toHaveBeenCalledOnce());
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
    installConveyorFixture(vi.fn().mockResolvedValue(diagnostics));
    window.history.replaceState(null, "", "/settings/sbr?doctor=1#fragment");

    render(<App />);

    expect(await screen.findByRole("heading", { level: 1, name: "Overview" })).toBeTruthy();
    expect(window.location.pathname).toBe("/overview");
    expect(window.location.search).toBe("");
    expect(window.location.hash).toBe("");
    expect(procedures().organisation.getCurrentUser).not.toHaveBeenCalled();
    expect(procedures().sbr.getSbrReadiness).not.toHaveBeenCalled();
  });

  it("rejects a tampered retained role projection instead of authenticating it", () => {
    installConveyorFixture(vi.fn().mockResolvedValue(diagnostics));
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
    installConveyorFixture(vi.fn().mockResolvedValue(diagnostics));
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
    installConveyorFixture(
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
      .fn<() => Promise<SystemDiagnostics>>()
      .mockRejectedValueOnce(
        new Error("capability=secret-token certificatePin=sha256:raw 127.0.0.1:45000 readiness"),
      )
      .mockImplementationOnce(() => retry.promise);
    installConveyorFixture(getSystemDiagnostics);
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
    installConveyorFixture(vi.fn().mockRejectedValue(new Error("unavailable")));
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
