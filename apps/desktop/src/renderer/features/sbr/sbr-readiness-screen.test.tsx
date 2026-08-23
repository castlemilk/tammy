import { create } from "@bufbuild/protobuf";
import { AuthenticationContextSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { FactorState, Role } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  GetSbrReadinessRequestSchema,
  GetSbrReadinessResponseSchema,
  MachineCredentialState,
  ProductIdState,
  SbrEnvironment,
  SbrReadinessSchema,
  SbrReadinessState,
} from "@tammy/connect-client/tammy/v1/sbr_pb.js";
import { act, render, screen } from "@testing-library/react";
import { StrictMode } from "react";
import { describe, expect, it, vi } from "vitest";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import { SbrReadinessScreen } from "./sbr-readiness-screen";

const codec = createProtoMethodCodec({
  input: GetSbrReadinessRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetSbrReadinessResponseSchema,
});

const workspace = {
  sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073993",
  userId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
  workspaceId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073991",
  organisationId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073994",
  organisationDisplayName: "Wattle & Co",
  organisationCanonicalAbn: "11000000560",
  roles: [Role.WORKSPACE_ADMIN],
  userFactorState: FactorState.ENABLED,
} satisfies AuthenticatedWorkspace & { readonly userFactorState: FactorState };

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((promiseResolve) => {
    resolve = promiseResolve;
  });
  return { promise, resolve };
}

function responseFrame(readiness: Parameters<typeof create<typeof SbrReadinessSchema>>[1]) {
  return codec.encodeResponse(
    create(GetSbrReadinessResponseSchema, {
      readiness: create(SbrReadinessSchema, readiness),
    }),
  );
}

function apiFor(
  readiness: NonNullable<Parameters<typeof create<typeof SbrReadinessSchema>>[1]>,
  defaultEvteScope = true,
): Pick<TammyDesktopAPI, "getCurrentUser" | "getSbrReadiness"> {
  const projected =
    readiness.environment === SbrEnvironment.EVTE && defaultEvteScope
      ? {
          evteProductIdentifier: "TAMMY.EVTE",
          evteServiceIdentifier: "BAS.LODGE",
          ...readiness,
        }
      : readiness;
  return {
    getCurrentUser: vi.fn(),
    getSbrReadiness: vi.fn(async (frame: Uint8Array) => {
      const request = codec.decodeRequest(frame);
      expect(request.authentication).toEqual(
        create(AuthenticationContextSchema, {
          actorUserId: workspace.userId,
          sessionId: workspace.sessionId,
        }),
      );
      return codec.encodeResponse(
        create(GetSbrReadinessResponseSchema, {
          readiness: create(SbrReadinessSchema, projected),
        }),
      );
    }),
  };
}

describe("SbrReadinessScreen", () => {
  it("keeps a persistent polite status region from loading through unavailable", async () => {
    const getSbrReadiness = vi.fn().mockRejectedValue(new Error("secret credential path"));
    render(
      <SbrReadinessScreen
        api={{ getCurrentUser: vi.fn(), getSbrReadiness }}
        workspace={workspace}
      />,
    );

    const status = screen.getByRole("status");
    expect(status.getAttribute("aria-live")).toBe("polite");
    expect(status.textContent).toContain("Checking SBR readiness");
    expect(await screen.findByText("Readiness unavailable")).toBeTruthy();
    expect(screen.getByRole("status")).toBe(status);
    expect(document.body.textContent).not.toContain("secret credential path");
  });

  it.each([
    {
      name: "simulator ready",
      readiness: {
        environment: SbrEnvironment.SIMULATOR,
        state: SbrReadinessState.READY_FOR_SIMULATOR,
        machineCredentialState: MachineCredentialState.PRESENT,
        productIdState: ProductIdState.MISSING,
        credentialFingerprint: "cred:7f91",
        profileFingerprint: "profile:3a12",
        componentFingerprint: "",
      },
      expected: ["Ready for simulator", "synthetic", "network-disabled"],
    },
    {
      name: "EVTE pre-conformance",
      readiness: {
        environment: SbrEnvironment.EVTE,
        state: SbrReadinessState.READY_FOR_EVTE_PRE_CONFORMANCE,
        machineCredentialState: MachineCredentialState.PRESENT,
        productIdState: ProductIdState.PRESENT,
      },
      expected: ["Ready for EVTE pre-conformance", "non-production"],
    },
    {
      name: "EVTE post-conformance",
      readiness: {
        environment: SbrEnvironment.EVTE,
        state: SbrReadinessState.READY_FOR_EVTE_POST_CONFORMANCE,
        machineCredentialState: MachineCredentialState.PRESENT,
        productIdState: ProductIdState.PRESENT,
      },
      expected: ["Ready for EVTE post-conformance", "non-production"],
    },
  ])("renders $name without claiming lodgment", async ({ readiness, expected }) => {
    render(<SbrReadinessScreen api={apiFor(readiness)} workspace={workspace} />);

    for (const copy of expected) {
      expect((await screen.findAllByText(new RegExp(copy, "i"))).length).toBeGreaterThan(0);
    }
    expect(screen.getByText(/BAS remains preparation-only/i)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /lodge|submit/i })).toBeNull();
  });

  it.each([
    [MachineCredentialState.MISSING, "Missing"],
    [MachineCredentialState.INACCESSIBLE, "Inaccessible"],
    [MachineCredentialState.INCOMPATIBLE, "Incompatible"],
    [MachineCredentialState.REVOKED, "Revoked"],
    [MachineCredentialState.EXPIRED, "Expired"],
    [MachineCredentialState.ABN_MISMATCH, "Organisation mismatch"],
  ])("renders credential state %s", async (machineCredentialState, label) => {
    render(
      <SbrReadinessScreen
        api={apiFor({
          environment: SbrEnvironment.EVTE,
          state: SbrReadinessState.UNAVAILABLE,
          machineCredentialState,
          productIdState: ProductIdState.PRESENT,
        })}
        workspace={workspace}
      />,
    );
    expect(await screen.findByText(label, { selector: "dd" })).toBeTruthy();
  });

  it.each([
    [ProductIdState.MISSING, "Missing"],
    [ProductIdState.INACCESSIBLE, "Inaccessible"],
  ])("renders Product ID state %s without its value", async (productIdState, label) => {
    render(
      <SbrReadinessScreen
        api={apiFor({
          environment: SbrEnvironment.EVTE,
          state: SbrReadinessState.UNAVAILABLE,
          machineCredentialState: MachineCredentialState.PRESENT,
          productIdState,
        })}
        workspace={workspace}
      />,
    );
    expect(await screen.findByText(label, { selector: "dd" })).toBeTruthy();
    expect(document.body.textContent).not.toContain("PRODUCT-ID-SECRET");
  });

  it("translates stale registration codes and never echoes unknown diagnostic text", async () => {
    render(
      <SbrReadinessScreen
        api={apiFor({
          environment: SbrEnvironment.EVTE,
          state: SbrReadinessState.UNAVAILABLE,
          machineCredentialState: MachineCredentialState.PRESENT,
          productIdState: ProductIdState.PRESENT,
          evteProductIdentifier: "TAMMY.EVTE",
          evteServiceIdentifier: "BAS.LODGE",
          readinessCodes: [
            "SBR_REGISTRATION_MANIFEST_EXPIRED",
            "SBR_ENDPOINT_PROFILE_EXPIRED",
            "SECRET_VALUE_FROM_SERVER",
          ],
        })}
        workspace={workspace}
      />,
    );
    expect(await screen.findByText(/registration evidence has expired/i)).toBeTruthy();
    expect(screen.getByText(/endpoint profile has expired/i)).toBeTruthy();
    expect(document.body.textContent).not.toContain("SECRET_VALUE_FROM_SERVER");
  });

  it("drops an unknown readiness code without rejecting an otherwise valid response", async () => {
    render(
      <SbrReadinessScreen
        api={apiFor({
          environment: SbrEnvironment.SIMULATOR,
          state: SbrReadinessState.READY_FOR_SIMULATOR,
          machineCredentialState: MachineCredentialState.PRESENT,
          productIdState: ProductIdState.MISSING,
          readinessCodes: ["unknown private diagnostic"],
        })}
        workspace={workspace}
      />,
    );

    expect(await screen.findByText("Ready for simulator")).toBeTruthy();
    expect(document.body.textContent).not.toContain("unknown private diagnostic");
  });

  it("runs doctor through the same RPC exactly once under StrictMode", async () => {
    const api = apiFor({
      environment: SbrEnvironment.SIMULATOR,
      state: SbrReadinessState.READY_FOR_SIMULATOR,
      machineCredentialState: MachineCredentialState.PRESENT,
      productIdState: ProductIdState.MISSING,
    });
    render(
      <StrictMode>
        <SbrReadinessScreen api={api} doctorMode workspace={workspace} />
      </StrictMode>,
    );
    expect(await screen.findByText("Ready for simulator")).toBeTruthy();
    expect(api.getSbrReadiness).toHaveBeenCalledOnce();
    expect(screen.queryByRole("button", { name: /doctor/i })).toBeNull();
  });

  it.each([ProductIdState.MISSING, ProductIdState.PRESENT, ProductIdState.INACCESSIBLE])(
    "accepts simulator readiness with core Product ID projection %s",
    async (productIdState) => {
      render(
        <SbrReadinessScreen
          api={apiFor({
            environment: SbrEnvironment.SIMULATOR,
            state: SbrReadinessState.READY_FOR_SIMULATOR,
            machineCredentialState: MachineCredentialState.PRESENT,
            productIdState,
          })}
          workspace={workspace}
        />,
      );

      expect(await screen.findByText("Ready for simulator")).toBeTruthy();
    },
  );

  it("ignores a stale readiness response after the authenticated principal changes", async () => {
    const first = deferred<Uint8Array>();
    const second = deferred<Uint8Array>();
    const getSbrReadiness = vi
      .fn()
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const nextWorkspace = {
      ...workspace,
      userId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073995",
      sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073996",
    };
    const getCurrentUser = vi.fn();
    const view = render(
      <SbrReadinessScreen api={{ getCurrentUser, getSbrReadiness }} workspace={workspace} />,
    );
    view.rerender(
      <SbrReadinessScreen api={{ getCurrentUser, getSbrReadiness }} workspace={nextWorkspace} />,
    );

    await act(async () => {
      second.resolve(
        responseFrame({
          environment: SbrEnvironment.EVTE,
          state: SbrReadinessState.READY_FOR_EVTE_PRE_CONFORMANCE,
          machineCredentialState: MachineCredentialState.PRESENT,
          productIdState: ProductIdState.PRESENT,
          evteProductIdentifier: "TAMMY.EVTE",
          evteServiceIdentifier: "BAS.LODGE",
        }),
      );
    });
    expect(
      await screen.findByRole("heading", { name: "Ready for EVTE pre-conformance" }),
    ).toBeTruthy();

    await act(async () => {
      first.resolve(
        responseFrame({
          environment: SbrEnvironment.SIMULATOR,
          state: SbrReadinessState.READY_FOR_SIMULATOR,
          machineCredentialState: MachineCredentialState.PRESENT,
          productIdState: ProductIdState.MISSING,
        }),
      );
    });
    expect(screen.queryByRole("heading", { name: "Ready for simulator" })).toBeNull();
  });

  it("hides credential mutation controls from users without the administrator role", async () => {
    const preparer = { ...workspace, roles: [Role.BUSINESS_PREPARER] };
    render(
      <SbrReadinessScreen
        api={apiFor({
          environment: SbrEnvironment.EVTE,
          state: SbrReadinessState.UNAVAILABLE,
          machineCredentialState: MachineCredentialState.PRESENT,
          productIdState: ProductIdState.MISSING,
        })}
        workspace={preparer}
      />,
    );
    await screen.findByText("Readiness unavailable");
    for (const action of [
      /import machine credential/i,
      /replace machine credential/i,
      /remove machine credential/i,
    ]) {
      expect(screen.queryByRole("button", { name: action })).toBeNull();
    }
  });

  it("shows credential controls only to administrators with an enabled factor", async () => {
    render(
      <SbrReadinessScreen
        api={apiFor({
          environment: SbrEnvironment.EVTE,
          state: SbrReadinessState.UNAVAILABLE,
          machineCredentialState: MachineCredentialState.PRESENT,
          productIdState: ProductIdState.MISSING,
        })}
        workspace={workspace}
      />,
    );
    const replace = await screen.findByRole("button", { name: "Replace credential" });
    const remove = screen.getByRole("button", { name: "Remove credential" });
    expect(replace.hasAttribute("disabled")).toBe(false);
    expect(remove.hasAttribute("disabled")).toBe(false);
    expect(screen.getByText(/protected storage on this Mac/i)).toBeTruthy();
  });

  it("shows TOTP setup instead of high-risk actions when the administrator has no enabled factor", async () => {
    render(
      <SbrReadinessScreen
        api={apiFor({
          environment: SbrEnvironment.SIMULATOR,
          state: SbrReadinessState.UNAVAILABLE,
          machineCredentialState: MachineCredentialState.MISSING,
          productIdState: ProductIdState.MISSING,
        })}
        workspace={{ ...workspace, userFactorState: FactorState.DISABLED }}
      />,
    );
    expect(await screen.findByRole("heading", { name: "Set up a security code" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Import credential" })).toBeNull();
  });

  it("resets local factor gating when the authoritative principal projection changes", async () => {
    const api = apiFor({
      environment: SbrEnvironment.SIMULATOR,
      state: SbrReadinessState.UNAVAILABLE,
      machineCredentialState: MachineCredentialState.MISSING,
      productIdState: ProductIdState.MISSING,
    });
    const view = render(<SbrReadinessScreen api={api} workspace={workspace} />);
    expect(await screen.findByRole("button", { name: "Import credential" })).toBeTruthy();
    view.rerender(
      <SbrReadinessScreen
        api={api}
        workspace={{ ...workspace, userFactorState: FactorState.PENDING_CONFIRMATION }}
      />,
    );
    expect(await screen.findByRole("button", { name: "Restart TOTP setup" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Import credential" })).toBeNull();
    expect(api.getSbrReadiness).toHaveBeenCalledOnce();
  });

  it("shows Product ID administration only for exact signed EVTE product and service scope", async () => {
    const base = {
      environment: SbrEnvironment.EVTE,
      state: SbrReadinessState.UNAVAILABLE,
      machineCredentialState: MachineCredentialState.PRESENT,
      productIdState: ProductIdState.MISSING,
    };
    const view = render(<SbrReadinessScreen api={apiFor(base, false)} workspace={workspace} />);
    expect(
      await screen.findByText(/could not inspect the authenticated local SBR state/i),
    ).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "EVTE Product ID" })).toBeNull();
    view.unmount();
    render(
      <SbrReadinessScreen
        api={apiFor({
          ...base,
          evteProductIdentifier: "TAMMY.EVTE",
          evteServiceIdentifier: "BAS.LODGE",
        })}
        workspace={workspace}
      />,
    );
    expect(await screen.findByRole("heading", { name: "EVTE Product ID" })).toBeTruthy();
  });

  it("fails closed when simulator readiness carries EVTE scope metadata", async () => {
    render(
      <SbrReadinessScreen
        api={apiFor({
          environment: SbrEnvironment.SIMULATOR,
          state: SbrReadinessState.UNAVAILABLE,
          machineCredentialState: MachineCredentialState.MISSING,
          productIdState: ProductIdState.MISSING,
          evteProductIdentifier: "TAMMY.EVTE",
          evteServiceIdentifier: "BAS.LODGE",
        })}
        workspace={workspace}
      />,
    );
    expect(
      await screen.findByText(/could not inspect the authenticated local SBR state/i),
    ).toBeTruthy();
  });

  it.each([
    {
      name: "unknown environment",
      environment: 999 as SbrEnvironment,
      state: SbrReadinessState.READY_FOR_SIMULATOR,
      machineCredentialState: MachineCredentialState.PRESENT,
      productIdState: ProductIdState.MISSING,
    },
    {
      name: "unspecified environment",
      environment: SbrEnvironment.UNSPECIFIED,
      state: SbrReadinessState.UNAVAILABLE,
      machineCredentialState: MachineCredentialState.MISSING,
      productIdState: ProductIdState.MISSING,
    },
    {
      name: "unknown readiness state",
      environment: SbrEnvironment.SIMULATOR,
      state: 999 as SbrReadinessState,
      machineCredentialState: MachineCredentialState.PRESENT,
      productIdState: ProductIdState.MISSING,
    },
    {
      name: "unknown credential state",
      environment: SbrEnvironment.SIMULATOR,
      state: SbrReadinessState.READY_FOR_SIMULATOR,
      machineCredentialState: 999 as MachineCredentialState,
      productIdState: ProductIdState.MISSING,
    },
    {
      name: "unknown Product ID state",
      environment: SbrEnvironment.SIMULATOR,
      state: SbrReadinessState.READY_FOR_SIMULATOR,
      machineCredentialState: MachineCredentialState.PRESENT,
      productIdState: 999 as ProductIdState,
    },
    {
      name: "unspecified Product ID state",
      environment: SbrEnvironment.SIMULATOR,
      state: SbrReadinessState.READY_FOR_SIMULATOR,
      machineCredentialState: MachineCredentialState.PRESENT,
      productIdState: ProductIdState.UNSPECIFIED,
    },
    {
      name: "EVTE stage under simulator",
      environment: SbrEnvironment.SIMULATOR,
      state: SbrReadinessState.READY_FOR_EVTE_PRE_CONFORMANCE,
      machineCredentialState: MachineCredentialState.PRESENT,
      productIdState: ProductIdState.PRESENT,
    },
    {
      name: "simulator stage under EVTE",
      environment: SbrEnvironment.EVTE,
      state: SbrReadinessState.READY_FOR_SIMULATOR,
      machineCredentialState: MachineCredentialState.PRESENT,
      productIdState: ProductIdState.MISSING,
    },
    {
      name: "EVTE ready without Product ID",
      environment: SbrEnvironment.EVTE,
      state: SbrReadinessState.READY_FOR_EVTE_PRE_CONFORMANCE,
      machineCredentialState: MachineCredentialState.PRESENT,
      productIdState: ProductIdState.MISSING,
    },
    {
      name: "ready stage without usable credential",
      environment: SbrEnvironment.EVTE,
      state: SbrReadinessState.READY_FOR_EVTE_POST_CONFORMANCE,
      machineCredentialState: MachineCredentialState.MISSING,
      productIdState: ProductIdState.PRESENT,
    },
  ])("fails closed for $name", async (readiness) => {
    render(<SbrReadinessScreen api={apiFor(readiness)} workspace={workspace} />);

    expect(
      await screen.findByText(/could not inspect the authenticated local SBR state/i),
    ).toBeTruthy();
    expect(screen.queryByText("Credential administration")).toBeNull();
  });

  it("renders the core reimport-required remediation without exposing internal state", async () => {
    render(
      <SbrReadinessScreen
        api={apiFor({
          environment: SbrEnvironment.SIMULATOR,
          state: SbrReadinessState.UNAVAILABLE,
          machineCredentialState: MachineCredentialState.PRESENT,
          productIdState: ProductIdState.MISSING,
          readinessCodes: ["SBR_CREDENTIAL_REIMPORT_REQUIRED"],
          credentialFingerprint: "reimport-safe-fingerprint",
        })}
        workspace={workspace}
      />,
    );

    expect(await screen.findByText(/reimport.*machine credential/i)).toBeTruthy();
    expect(screen.getByText("reimport-safe-fingerprint")).toBeTruthy();
  });

  it("wraps the maximum-length organisation display name", async () => {
    const longName = "W".repeat(256);
    render(
      <SbrReadinessScreen
        api={apiFor({
          environment: SbrEnvironment.SIMULATOR,
          state: SbrReadinessState.READY_FOR_SIMULATOR,
          machineCredentialState: MachineCredentialState.PRESENT,
          productIdState: ProductIdState.MISSING,
        })}
        workspace={{ ...workspace, organisationDisplayName: longName }}
      />,
    );

    const identity = await screen.findByTestId("sbr-organisation-identity");
    expect(identity.classList.contains("min-w-0")).toBe(true);
    expect(identity.classList.contains("[overflow-wrap:anywhere]")).toBe(true);
  });

  it("integrates simulator controls only for a business lodger on authenticated simulator readiness", async () => {
    const api = {
      ...apiFor({
        environment: SbrEnvironment.SIMULATOR,
        state: SbrReadinessState.READY_FOR_SIMULATOR,
        machineCredentialState: MachineCredentialState.PRESENT,
        productIdState: ProductIdState.MISSING,
      }),
      assertTotp: vi.fn(),
      runSbrReadinessFixture: vi.fn(),
    };
    render(
      <SbrReadinessScreen api={api} workspace={{ ...workspace, roles: [Role.BUSINESS_LODGER] }} />,
    );

    expect(await screen.findByRole("button", { name: "Run simulator fixture" })).toBeTruthy();
    expect(screen.getByText("SIM-SBR-READINESS-V1")).toBeTruthy();
    expect(api.runSbrReadinessFixture).not.toHaveBeenCalled();
  });

  it("keeps EVTE simulator evidence status-only without a component operation", async () => {
    const api = {
      ...apiFor({
        environment: SbrEnvironment.EVTE,
        state: SbrReadinessState.READY_FOR_EVTE_PRE_CONFORMANCE,
        machineCredentialState: MachineCredentialState.PRESENT,
        productIdState: ProductIdState.PRESENT,
      }),
      assertTotp: vi.fn(),
      runSbrReadinessFixture: vi.fn(),
    };
    render(
      <SbrReadinessScreen api={api} workspace={{ ...workspace, roles: [Role.BUSINESS_LODGER] }} />,
    );

    expect(await screen.findByText(/EVTE status only/i)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Run simulator fixture" })).toBeNull();
    expect(api.runSbrReadinessFixture).not.toHaveBeenCalled();
  });
});
