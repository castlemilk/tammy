import { create } from "@bufbuild/protobuf";
import { AuthenticationContextSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { Role } from "@tammy/connect-client/tammy/v1/identity_pb.js";
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
} satisfies AuthenticatedWorkspace;

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
  readiness: Parameters<typeof create<typeof SbrReadinessSchema>>[1],
): Pick<TammyDesktopAPI, "getSbrReadiness"> {
  return {
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
          readiness: create(SbrReadinessSchema, readiness),
        }),
      );
    }),
  };
}

describe("SbrReadinessScreen", () => {
  it("keeps a persistent polite status region from loading through unavailable", async () => {
    const getSbrReadiness = vi.fn().mockRejectedValue(new Error("secret credential path"));
    render(<SbrReadinessScreen api={{ getSbrReadiness }} workspace={workspace} />);

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
        productIdState: ProductIdState.UNSPECIFIED,
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

  it("runs doctor through the same RPC exactly once under StrictMode", async () => {
    const api = apiFor({
      environment: SbrEnvironment.SIMULATOR,
      state: SbrReadinessState.READY_FOR_SIMULATOR,
      machineCredentialState: MachineCredentialState.PRESENT,
      productIdState: ProductIdState.UNSPECIFIED,
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
    const view = render(<SbrReadinessScreen api={{ getSbrReadiness }} workspace={workspace} />);
    view.rerender(<SbrReadinessScreen api={{ getSbrReadiness }} workspace={nextWorkspace} />);

    await act(async () => {
      second.resolve(
        responseFrame({
          environment: SbrEnvironment.EVTE,
          state: SbrReadinessState.READY_FOR_EVTE_PRE_CONFORMANCE,
          machineCredentialState: MachineCredentialState.PRESENT,
          productIdState: ProductIdState.PRESENT,
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
          productIdState: ProductIdState.UNSPECIFIED,
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

  it("shows described but inactive credential controls only to administrators", async () => {
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
    const replace = await screen.findByRole("button", { name: "Replace machine credential" });
    const remove = screen.getByRole("button", { name: "Remove machine credential" });
    expect(replace.hasAttribute("disabled")).toBe(true);
    expect(remove.hasAttribute("disabled")).toBe(true);
    expect(replace.getAttribute("aria-describedby")).toBe("sbr-security-actions-description");
    expect(screen.getByText(/local core authorizes every change/i)).toBeTruthy();
  });
});
