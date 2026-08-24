import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { FreshFactorContextSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  AssertTOTPRequestSchema,
  AssertTOTPResponseSchema,
  Role,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  MachineCredentialState,
  ProductIdState,
  RunSbrReadinessFixtureRequestSchema,
  RunSbrReadinessFixtureResponseSchema,
  SbrEnvironment,
  SbrReadinessFixtureFailure,
  SbrReadinessFixtureOutcome,
  SbrReadinessFixtureResultSchema,
  SbrReadinessSchema,
  SbrReadinessState,
} from "@tammy/connect-client/tammy/v1/sbr_pb.js";
import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode } from "react";
import { describe, expect, it, vi } from "vitest";

import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import { SbrSimulatorPanel } from "./sbr-simulator-panel";

const FIXTURE_ID = "SIM-SBR-READINESS-V1";
const assertCodec = createProtoMethodCodec({
  input: AssertTOTPRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 8_192,
  output: AssertTOTPResponseSchema,
});
const runCodec = createProtoMethodCodec({
  input: RunSbrReadinessFixtureRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: RunSbrReadinessFixtureResponseSchema,
});

const workspace = {
  workspaceId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073991",
  userId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
  sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073993",
  organisationId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073994",
  organisationDisplayName: "Current Live Organisation",
  organisationCanonicalAbn: "53004085616",
  roles: [Role.WORKSPACE_ADMIN],
} satisfies AuthenticatedWorkspace;

const simulatorReadiness = create(SbrReadinessSchema, {
  environment: SbrEnvironment.SIMULATOR,
  state: SbrReadinessState.READY_FOR_SIMULATOR,
  machineCredentialState: MachineCredentialState.PRESENT,
  productIdState: ProductIdState.MISSING,
  credentialFingerprint: "credential:simulator",
  profileFingerprint: "profile:simulator",
});

const invalidSimulatorReadyProjections = [
  {
    name: "Product ID present",
    readiness: create(SbrReadinessSchema, {
      ...simulatorReadiness,
      productIdState: ProductIdState.PRESENT,
    }),
  },
  {
    name: "readiness code present",
    readiness: create(SbrReadinessSchema, {
      ...simulatorReadiness,
      readinessCodes: ["SBR_PRIVATE_INCONSISTENCY"],
    }),
  },
  {
    name: "EVTE scope present",
    readiness: create(SbrReadinessSchema, {
      ...simulatorReadiness,
      evteProductIdentifier: "TAMMY.EVTE",
      evteServiceIdentifier: "BAS.LODGE",
    }),
  },
  {
    name: "oversized fingerprint",
    readiness: create(SbrReadinessSchema, {
      ...simulatorReadiness,
      componentFingerprint: "f".repeat(129),
    }),
  },
] as const;

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((promiseResolve) => {
    resolve = promiseResolve;
  });
  return { promise, resolve };
}

function factorFrame(purpose = "sbr_machine_credential_use") {
  return assertCodec.encodeResponse(
    create(AssertTOTPResponseSchema, {
      freshFactor: create(FreshFactorContextSchema, {
        assertionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073995",
        purpose,
        assertedAt: create(TimestampSchema, { seconds: 1n }),
      }),
    }),
  );
}

function resultFrame(
  failureCase: SbrReadinessFixtureFailure,
  overrides: {
    readonly failureCase?: SbrReadinessFixtureFailure;
    readonly fixtureId?: string;
    readonly outcome?: SbrReadinessFixtureOutcome;
    readonly readiness?: Parameters<typeof create<typeof SbrReadinessSchema>>[1];
    readonly succeeded?: boolean;
  } = {},
) {
  const defaultOutcome =
    {
      [SbrReadinessFixtureFailure.UNSPECIFIED]: SbrReadinessFixtureOutcome.ACCEPTED,
      [SbrReadinessFixtureFailure.NOT_STARTED]: SbrReadinessFixtureOutcome.NOT_STARTED,
      [SbrReadinessFixtureFailure.MAYBE_SENT]: SbrReadinessFixtureOutcome.MAYBE_SENT,
      [SbrReadinessFixtureFailure.MALFORMED_RESPONSE]:
        SbrReadinessFixtureOutcome.MALFORMED_RESPONSE,
      [SbrReadinessFixtureFailure.HELPER_DEATH]: SbrReadinessFixtureOutcome.HELPER_DEATH,
      [SbrReadinessFixtureFailure.TIMEOUT]: SbrReadinessFixtureOutcome.TIMEOUT,
      [SbrReadinessFixtureFailure.UNKNOWN]: SbrReadinessFixtureOutcome.UNKNOWN,
    }[failureCase] ?? SbrReadinessFixtureOutcome.UNKNOWN;
  const outcome = overrides.outcome ?? defaultOutcome;
  return runCodec.encodeResponse(
    create(RunSbrReadinessFixtureResponseSchema, {
      result: create(SbrReadinessFixtureResultSchema, {
        fixtureId: overrides.fixtureId ?? FIXTURE_ID,
        failureCase: overrides.failureCase ?? failureCase,
        succeeded:
          overrides.succeeded ??
          (outcome === SbrReadinessFixtureOutcome.ACCEPTED ||
            (outcome === SbrReadinessFixtureOutcome.EXACT_REPLAY &&
              failureCase === SbrReadinessFixtureFailure.UNSPECIFIED)),
        readiness: create(SbrReadinessSchema, overrides.readiness ?? simulatorReadiness),
        outcome,
      }),
    }),
  );
}

function apiFor(
  run = vi.fn((frame: Uint8Array) => {
    const request = runCodec.decodeRequest(frame);
    return Promise.resolve(resultFrame(request.failureCase));
  }),
) {
  return {
    assertTotp: vi.fn((frame: Uint8Array) => {
      const request = assertCodec.decodeRequest(frame);
      return Promise.resolve(factorFrame(request.purpose));
    }),
    runSbrReadinessFixture: run,
  };
}

async function runFixture(user: ReturnType<typeof userEvent.setup>, diagnosticCase = "ACCEPTED") {
  if (diagnosticCase !== "ACCEPTED") {
    await user.selectOptions(screen.getByLabelText("Test-only diagnostic case"), diagnosticCase);
  }
  await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
  await user.click(screen.getByRole("button", { name: "Run simulator fixture" }));
}

describe("SbrSimulatorPanel", () => {
  it("shows the fixed network-disabled preparation-only fixture identity", () => {
    render(
      <SbrSimulatorPanel
        api={apiFor()}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={workspace}
      />,
    );

    expect(screen.getByText(FIXTURE_ID)).toBeTruthy();
    expect(screen.getByText("Wattle & Co Test Pty Ltd")).toBeTruthy();
    expect(screen.getByText(/ABN 11 000 000 560/)).toBeTruthy();
    expect(screen.getByText("SIM.READINESS.0001")).toBeTruthy();
    expect(screen.getByText(/simulator-only/i)).toBeTruthy();
    expect(screen.getByText(/network-disabled/i)).toBeTruthy();
    expect(screen.getByText(/BAS work stays preparation-only/i)).toBeTruthy();
    expect(screen.getByLabelText("Test-only diagnostic case")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /production|lodge|submit|declare/i })).toBeNull();
  });

  it("asserts the exact purpose and sends only the fixed fixture command boundary", async () => {
    const decoded: ReturnType<typeof runCodec.decodeRequest>[] = [];
    const assertions: ReturnType<typeof assertCodec.decodeRequest>[] = [];
    const api = apiFor(
      vi.fn((frame: Uint8Array) => {
        const request = runCodec.decodeRequest(frame);
        decoded.push(request);
        return Promise.resolve(resultFrame(request.failureCase));
      }),
    );
    vi.mocked(api.assertTotp).mockImplementation((frame: Uint8Array) => {
      const request = assertCodec.decodeRequest(frame);
      assertions.push(request);
      return Promise.resolve(factorFrame(request.purpose));
    });
    const user = userEvent.setup();
    const workspaceWithAccounting = {
      ...workspace,
      currentBasDraftId: "LIVE-BAS-2026-Q4",
      currentAccountingTotal: "9876543.21",
      currentReportingPeriod: "2026-06-30",
    };
    render(
      <SbrSimulatorPanel
        api={api}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={workspaceWithAccounting}
      />,
    );

    await runFixture(user);
    expect(await screen.findByText("ACCEPTED")).toBeTruthy();
    expect(api.assertTotp).toHaveBeenCalledOnce();
    const assertion = assertions[0];
    expect(assertion).toBeDefined();
    expect(assertion?.purpose).toBe("sbr_machine_credential_use");
    expect(assertion?.authentication?.actorUserId).toBe(workspace.userId);
    expect(assertion?.authentication?.sessionId).toBe(workspace.sessionId);
    expect(decoded).toHaveLength(1);
    expect(decoded[0]?.fixtureId).toBe(FIXTURE_ID);
    expect(decoded[0]?.failureCase).toBe(SbrReadinessFixtureFailure.UNSPECIFIED);
    expect(decoded[0]?.commandContext?.authentication?.actorUserId).toBe(workspace.userId);
    expect(decoded[0]?.commandContext?.authentication?.sessionId).toBe(workspace.sessionId);
    expect(decoded[0]?.commandContext?.freshFactor?.purpose).toBe("sbr_machine_credential_use");
    expect(decoded[0]?.commandContext?.idempotencyKey).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
    expect(
      [...(vi.mocked(api.runSbrReadinessFixture).mock.calls[0]?.[0] ?? [])].every(
        (byte) => byte === 0,
      ),
    ).toBe(true);
    const serialized = JSON.stringify(decoded[0], (_key, value) =>
      typeof value === "bigint" ? value.toString() : value,
    );
    for (const forbidden of [
      workspaceWithAccounting.currentBasDraftId,
      workspaceWithAccounting.currentAccountingTotal,
      workspaceWithAccounting.currentReportingPeriod,
      workspace.organisationCanonicalAbn,
      workspace.organisationDisplayName,
    ]) {
      expect(serialized).not.toContain(forbidden);
    }
  });

  it.each([
    ["ACCEPTED", "ACCEPTED"],
    ["NOT_STARTED", "NOT_STARTED"],
    ["MAYBE_SENT", "MAYBE_SENT"],
  ])("renders the deterministic %s result", async (diagnosticCase, expected) => {
    const user = userEvent.setup();
    render(
      <SbrSimulatorPanel
        api={apiFor()}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={workspace}
      />,
    );
    await runFixture(user, diagnosticCase);
    expect(await screen.findByText(expected)).toBeTruthy();
  });

  it.each(["MALFORMED_RESPONSE", "HELPER_DEATH", "TIMEOUT"])(
    "renders and terminal-locks the authoritative %s diagnostic",
    async (diagnosticCase) => {
      const run = vi.fn((frame: Uint8Array) => {
        const request = runCodec.decodeRequest(frame);
        return Promise.resolve(resultFrame(request.failureCase));
      });
      const user = userEvent.setup();
      render(
        <SbrSimulatorPanel
          api={apiFor(run)}
          onRefresh={vi.fn()}
          readiness={simulatorReadiness}
          workspace={workspace}
        />,
      );
      await runFixture(user, diagnosticCase);
      expect(await screen.findByText(diagnosticCase)).toBeTruthy();
      expect(screen.getByRole("button", { name: "Refresh authoritative status" })).toBeTruthy();
      expect(
        (screen.getByRole("button", { name: "Run simulator fixture" }) as HTMLButtonElement)
          .disabled,
      ).toBe(true);
    },
  );

  it("replays the exact operation with the same idempotency key and no new factor", async () => {
    const requests: ReturnType<typeof runCodec.decodeRequest>[] = [];
    const api = apiFor(
      vi.fn((frame: Uint8Array) => {
        const request = runCodec.decodeRequest(frame);
        requests.push(request);
        return Promise.resolve(
          resultFrame(request.failureCase, {
            outcome:
              requests.length === 1
                ? SbrReadinessFixtureOutcome.ACCEPTED
                : SbrReadinessFixtureOutcome.EXACT_REPLAY,
          }),
        );
      }),
    );
    const user = userEvent.setup();
    render(
      <SbrSimulatorPanel
        api={api}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={workspace}
      />,
    );
    await runFixture(user);
    await screen.findByText("ACCEPTED");
    await user.click(screen.getByRole("button", { name: "Replay exact request" }));
    expect(await screen.findByText("EXACT_REPLAY")).toBeTruthy();
    expect(requests).toHaveLength(2);
    expect(requests[1]).toEqual(requests[0]);
    expect(api.assertTotp).toHaveBeenCalledOnce();
  });

  it("accepts a non-successful exact replay only for the original named case", async () => {
    const requests: ReturnType<typeof runCodec.decodeRequest>[] = [];
    const api = apiFor(
      vi.fn((frame: Uint8Array) => {
        const request = runCodec.decodeRequest(frame);
        requests.push(request);
        return Promise.resolve(
          resultFrame(request.failureCase, {
            outcome:
              requests.length === 1
                ? SbrReadinessFixtureOutcome.NOT_STARTED
                : SbrReadinessFixtureOutcome.EXACT_REPLAY,
            succeeded: false,
          }),
        );
      }),
    );
    const user = userEvent.setup();
    render(
      <SbrSimulatorPanel
        api={api}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={workspace}
      />,
    );
    await runFixture(user, "NOT_STARTED");
    await screen.findByText("NOT_STARTED");
    await user.click(screen.getByRole("button", { name: "Replay exact request" }));
    expect(await screen.findByText("EXACT_REPLAY")).toBeTruthy();
    expect(requests[1]).toEqual(requests[0]);
  });

  it("allows authoritative UNKNOWN during replay without claiming an exact result", async () => {
    let calls = 0;
    const run = vi.fn((frame: Uint8Array) => {
      const request = runCodec.decodeRequest(frame);
      calls += 1;
      return Promise.resolve(
        resultFrame(request.failureCase, {
          outcome:
            calls === 1 ? SbrReadinessFixtureOutcome.ACCEPTED : SbrReadinessFixtureOutcome.UNKNOWN,
          succeeded: calls === 1,
        }),
      );
    });
    const user = userEvent.setup();
    render(
      <SbrSimulatorPanel
        api={apiFor(run)}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={workspace}
      />,
    );
    await runFixture(user);
    await screen.findByText("ACCEPTED");
    await user.click(screen.getByRole("button", { name: "Replay exact request" }));
    expect(await screen.findByText("UNKNOWN")).toBeTruthy();
    expect(screen.queryByText("EXACT_REPLAY")).toBeNull();
  });

  it("uses the same key with an altered closed case to expose an idempotency conflict", async () => {
    const keys: string[] = [];
    const cases: SbrReadinessFixtureFailure[] = [];
    const run = vi.fn((frame: Uint8Array) => {
      const request = runCodec.decodeRequest(frame);
      keys.push(request.commandContext?.idempotencyKey ?? "");
      cases.push(request.failureCase);
      if (keys.length === 2) {
        return Promise.resolve(
          resultFrame(request.failureCase, {
            outcome: SbrReadinessFixtureOutcome.IDEMPOTENCY_CONFLICT,
          }),
        );
      }
      return Promise.resolve(resultFrame(request.failureCase));
    });
    const api = apiFor(run);
    const user = userEvent.setup();
    render(
      <SbrSimulatorPanel
        api={api}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={workspace}
      />,
    );
    await runFixture(user);
    await screen.findByText("ACCEPTED");
    await user.selectOptions(screen.getByLabelText("Test-only diagnostic case"), "NOT_STARTED");
    await user.click(screen.getByRole("button", { name: "Check idempotency conflict" }));
    expect(await screen.findByText("IDEMPOTENCY_CONFLICT")).toBeTruthy();
    expect(keys).toHaveLength(2);
    expect(new Set(keys).size).toBe(1);
    expect(cases).toEqual([
      SbrReadinessFixtureFailure.UNSPECIFIED,
      SbrReadinessFixtureFailure.NOT_STARTED,
    ]);
    expect(api.assertTotp).toHaveBeenCalledOnce();
  });

  it("rejects a named terminal outcome returned in conflict mode", async () => {
    let calls = 0;
    const run = vi.fn((frame: Uint8Array) => {
      const request = runCodec.decodeRequest(frame);
      calls += 1;
      return Promise.resolve(
        resultFrame(request.failureCase, {
          outcome:
            calls === 1 ? SbrReadinessFixtureOutcome.ACCEPTED : SbrReadinessFixtureOutcome.TIMEOUT,
          succeeded: calls === 1,
        }),
      );
    });
    const user = userEvent.setup();
    render(
      <SbrSimulatorPanel
        api={apiFor(run)}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={workspace}
      />,
    );
    await runFixture(user);
    await screen.findByText("ACCEPTED");
    await user.selectOptions(screen.getByLabelText("Test-only diagnostic case"), "TIMEOUT");
    await user.click(screen.getByRole("button", { name: "Check idempotency conflict" }));
    expect(await screen.findByText("UNKNOWN")).toBeTruthy();
  });

  it("rejects UNKNOWN in conflict mode without claiming authoritative restart recovery", async () => {
    let calls = 0;
    const run = vi.fn((frame: Uint8Array) => {
      const request = runCodec.decodeRequest(frame);
      calls += 1;
      return Promise.resolve(
        resultFrame(request.failureCase, {
          outcome:
            calls === 1 ? SbrReadinessFixtureOutcome.ACCEPTED : SbrReadinessFixtureOutcome.UNKNOWN,
          succeeded: calls === 1,
        }),
      );
    });
    const user = userEvent.setup();
    render(
      <SbrSimulatorPanel
        api={apiFor(run)}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={workspace}
      />,
    );
    await runFixture(user);
    await screen.findByText("ACCEPTED");
    await user.selectOptions(screen.getByLabelText("Test-only diagnostic case"), "TIMEOUT");
    await user.click(screen.getByRole("button", { name: "Check idempotency conflict" }));
    expect(await screen.findByText("UNKNOWN")).toBeTruthy();
    expect(screen.getByRole("status").textContent).toMatch(/local operation outcome is unknown/i);
    expect(
      screen.queryByText(/Core reports an unknown outcome after restart recovery/i),
    ).toBeNull();
  });

  it("terminal-locks restart-recovered UNKNOWN and refreshes without resending", async () => {
    const onRefresh = vi.fn();
    const run = vi.fn((frame: Uint8Array) => {
      const request = runCodec.decodeRequest(frame);
      return Promise.resolve(
        resultFrame(request.failureCase, { outcome: SbrReadinessFixtureOutcome.UNKNOWN }),
      );
    });
    const user = userEvent.setup();
    render(
      <SbrSimulatorPanel
        api={apiFor(run)}
        onRefresh={onRefresh}
        readiness={simulatorReadiness}
        workspace={workspace}
      />,
    );
    await runFixture(user);
    expect(await screen.findByText("UNKNOWN")).toBeTruthy();
    expect(screen.getAllByText(/restart recovery/i).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/never automatically resent/i)).toHaveLength(2);
    await user.click(screen.getByRole("button", { name: "Refresh authoritative status" }));
    expect(onRefresh).toHaveBeenCalledOnce();
    expect(run).toHaveBeenCalledOnce();
    expect((screen.getByLabelText("Test-only diagnostic case") as HTMLSelectElement).disabled).toBe(
      false,
    );
    expect(screen.queryByRole("button", { name: "Refresh authoritative status" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Replay exact request" })).toBeNull();
  });

  it("uses generic UNKNOWN for a rejected invocation without claiming restart recovery", async () => {
    const run = vi.fn().mockRejectedValue(new Error("private helper detail"));
    const user = userEvent.setup();
    render(
      <SbrSimulatorPanel
        api={apiFor(run)}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={workspace}
      />,
    );
    await runFixture(user, "HELPER_DEATH");
    expect(await screen.findByText("UNKNOWN")).toBeTruthy();
    expect(document.body.textContent).not.toContain("private helper detail");
    expect(document.body.textContent).not.toMatch(/restart recovery/i);
  });

  it("guards double clicks before and after factor assertion", async () => {
    const pendingRun = deferred<Uint8Array>();
    const run = vi.fn(() => pendingRun.promise);
    const api = apiFor(run);
    const user = userEvent.setup();
    render(
      <StrictMode>
        <SbrSimulatorPanel
          api={api}
          onRefresh={vi.fn()}
          readiness={simulatorReadiness}
          workspace={workspace}
        />
      </StrictMode>,
    );
    await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
    await user.dblClick(screen.getByRole("button", { name: "Run simulator fixture" }));
    expect(api.assertTotp).toHaveBeenCalledOnce();
    expect(run).toHaveBeenCalledOnce();
    pendingRun.resolve(resultFrame(SbrReadinessFixtureFailure.UNSPECIFIED));
    expect(await screen.findByText("ACCEPTED")).toBeTruthy();
  });

  it.each([
    ["missing result", () => runCodec.encodeResponse(create(RunSbrReadinessFixtureResponseSchema))],
    [
      "wrong fixture echo",
      () =>
        resultFrame(SbrReadinessFixtureFailure.UNSPECIFIED, {
          fixtureId: "SIM-OTHER",
        }),
    ],
    [
      "wrong case echo",
      () =>
        resultFrame(SbrReadinessFixtureFailure.UNSPECIFIED, {
          failureCase: SbrReadinessFixtureFailure.NOT_STARTED,
        }),
    ],
    [
      "wrong boolean semantics",
      () =>
        resultFrame(SbrReadinessFixtureFailure.UNSPECIFIED, {
          succeeded: false,
        }),
    ],
    [
      "unspecified authoritative outcome",
      () =>
        resultFrame(SbrReadinessFixtureFailure.UNSPECIFIED, {
          outcome: SbrReadinessFixtureOutcome.UNSPECIFIED,
          succeeded: false,
        }),
    ],
    [
      "unexpected initial conflict outcome",
      () =>
        resultFrame(SbrReadinessFixtureFailure.UNSPECIFIED, {
          outcome: SbrReadinessFixtureOutcome.IDEMPOTENCY_CONFLICT,
          succeeded: false,
        }),
    ],
    [
      "invalid readiness",
      () =>
        resultFrame(SbrReadinessFixtureFailure.UNSPECIFIED, {
          readiness: {
            environment: SbrEnvironment.EVTE,
            state: SbrReadinessState.READY_FOR_EVTE_PRE_CONFORMANCE,
            machineCredentialState: MachineCredentialState.PRESENT,
            productIdState: ProductIdState.PRESENT,
            evteProductIdentifier: "TAMMY.EVTE",
            evteServiceIdentifier: "BAS.LODGE",
          },
        }),
    ],
  ])("rejects a %s response and requires a refresh", async (_name, response) => {
    const user = userEvent.setup();
    render(
      <SbrSimulatorPanel
        api={apiFor(vi.fn(async () => response()))}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={workspace}
      />,
    );
    await runFixture(user);
    expect(await screen.findByText("UNKNOWN")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Refresh authoritative status" })).toBeTruthy();
  });

  it.each([
    {
      name: "TIMEOUT request with accepted success",
      diagnosticCase: "TIMEOUT",
      outcome: SbrReadinessFixtureOutcome.ACCEPTED,
      succeeded: true,
    },
    {
      name: "HELPER_DEATH request with accepted success",
      diagnosticCase: "HELPER_DEATH",
      outcome: SbrReadinessFixtureOutcome.ACCEPTED,
      succeeded: true,
    },
    {
      name: "MALFORMED request with accepted success",
      diagnosticCase: "MALFORMED_RESPONSE",
      outcome: SbrReadinessFixtureOutcome.ACCEPTED,
      succeeded: true,
    },
    {
      name: "TIMEOUT request with helper-death outcome",
      diagnosticCase: "TIMEOUT",
      outcome: SbrReadinessFixtureOutcome.HELPER_DEATH,
      succeeded: false,
    },
    {
      name: "accepted request with timeout outcome",
      diagnosticCase: "ACCEPTED",
      outcome: SbrReadinessFixtureOutcome.TIMEOUT,
      succeeded: false,
    },
  ])("rejects incompatible $name fields", async ({ diagnosticCase, outcome, succeeded }) => {
    const user = userEvent.setup();
    const run = vi.fn((frame: Uint8Array) => {
      const request = runCodec.decodeRequest(frame);
      return Promise.resolve(resultFrame(request.failureCase, { outcome, succeeded }));
    });
    render(
      <SbrSimulatorPanel
        api={apiFor(run)}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={workspace}
      />,
    );
    await runFixture(user, diagnosticCase);
    expect(await screen.findByText("UNKNOWN")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Refresh authoritative status" })).toBeTruthy();
  });

  it("does not dispatch after unmount while factor assertion is pending", async () => {
    const assertion = deferred<Uint8Array>();
    const api = apiFor();
    vi.mocked(api.assertTotp).mockReturnValue(assertion.promise);
    const user = userEvent.setup();
    const view = render(
      <SbrSimulatorPanel
        api={api}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={workspace}
      />,
    );
    await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
    await user.click(screen.getByRole("button", { name: "Run simulator fixture" }));
    view.unmount();
    await act(async () => assertion.resolve(factorFrame()));
    expect(api.runSbrReadinessFixture).not.toHaveBeenCalled();
  });

  it("does not dispatch for a stale principal after factor assertion", async () => {
    const assertion = deferred<Uint8Array>();
    const api = apiFor();
    vi.mocked(api.assertTotp).mockReturnValue(assertion.promise);
    const user = userEvent.setup();
    const view = render(
      <SbrSimulatorPanel
        api={api}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={workspace}
      />,
    );
    await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
    await user.click(screen.getByRole("button", { name: "Run simulator fixture" }));
    view.rerender(
      <SbrSimulatorPanel
        api={api}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={{ ...workspace, userId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073996" }}
      />,
    );
    await act(async () => assertion.resolve(factorFrame()));
    expect(api.runSbrReadinessFixture).not.toHaveBeenCalled();
  });

  it.each([
    {
      name: "EVTE transition",
      readiness: create(SbrReadinessSchema, {
        environment: SbrEnvironment.EVTE,
        state: SbrReadinessState.READY_FOR_EVTE_PRE_CONFORMANCE,
        machineCredentialState: MachineCredentialState.PRESENT,
        productIdState: ProductIdState.PRESENT,
        evteProductIdentifier: "TAMMY.EVTE",
        evteServiceIdentifier: "BAS.LODGE",
      }),
      workspace,
      factorEnabled: true,
    },
    {
      name: "role loss",
      readiness: simulatorReadiness,
      workspace: { ...workspace, roles: [Role.BUSINESS_LODGER] },
      factorEnabled: true,
    },
    {
      name: "readiness loss",
      readiness: create(SbrReadinessSchema, {
        environment: SbrEnvironment.SIMULATOR,
        state: SbrReadinessState.UNAVAILABLE,
        machineCredentialState: MachineCredentialState.MISSING,
        productIdState: ProductIdState.MISSING,
      }),
      workspace,
      factorEnabled: true,
    },
    {
      name: "credential readiness loss",
      readiness: create(SbrReadinessSchema, {
        environment: SbrEnvironment.SIMULATOR,
        state: SbrReadinessState.READY_FOR_SIMULATOR,
        machineCredentialState: MachineCredentialState.INACCESSIBLE,
        productIdState: ProductIdState.MISSING,
      }),
      workspace,
      factorEnabled: true,
    },
    {
      name: "factor disable",
      readiness: simulatorReadiness,
      workspace,
      factorEnabled: false,
    },
    ...invalidSimulatorReadyProjections.map(({ name, readiness }) => ({
      name,
      readiness,
      workspace,
      factorEnabled: true,
    })),
  ])(
    "does not dispatch when $name invalidates the action gate during factor assertion",
    async (next) => {
      const assertion = deferred<Uint8Array>();
      const api = apiFor();
      vi.mocked(api.assertTotp).mockReturnValue(assertion.promise);
      const user = userEvent.setup();
      const view = render(
        <SbrSimulatorPanel
          api={api}
          onRefresh={vi.fn()}
          readiness={simulatorReadiness}
          workspace={workspace}
        />,
      );
      await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
      await user.click(screen.getByRole("button", { name: "Run simulator fixture" }));
      view.rerender(
        <SbrSimulatorPanel
          api={api}
          factorEnabled={next.factorEnabled}
          onRefresh={vi.fn()}
          readiness={next.readiness}
          workspace={next.workspace}
        />,
      );
      await act(async () => assertion.resolve(factorFrame()));
      expect(api.runSbrReadinessFixture).not.toHaveBeenCalled();
    },
  );

  it.each(invalidSimulatorReadyProjections)(
    "keeps invalid simulator READY projection status-only: $name",
    ({ readiness }) => {
      const api = apiFor();
      render(
        <SbrSimulatorPanel
          api={api}
          onRefresh={vi.fn()}
          readiness={readiness}
          workspace={workspace}
        />,
      );
      expect(screen.getByText(/simulator is not ready/i)).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Run simulator fixture" })).toBeNull();
      expect(api.runSbrReadinessFixture).not.toHaveBeenCalled();
    },
  );

  it("ignores a stale operation response after the session changes", async () => {
    const operation = deferred<Uint8Array>();
    const api = apiFor(vi.fn(() => operation.promise));
    const user = userEvent.setup();
    const view = render(
      <SbrSimulatorPanel
        api={api}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={workspace}
      />,
    );
    await runFixture(user);
    view.rerender(
      <SbrSimulatorPanel
        api={api}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={{ ...workspace, sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073997" }}
      />,
    );
    await act(async () => operation.resolve(resultFrame(SbrReadinessFixtureFailure.UNSPECIFIED)));
    expect(screen.queryByText("ACCEPTED")).toBeNull();
  });

  it("does not let a stale operation completion release the new principal's in-flight guard", async () => {
    const oldOperation = deferred<Uint8Array>();
    const newAssertion = deferred<Uint8Array>();
    const run = vi.fn(() => oldOperation.promise);
    const api = apiFor(run);
    vi.mocked(api.assertTotp)
      .mockResolvedValueOnce(factorFrame())
      .mockReturnValueOnce(newAssertion.promise);
    const user = userEvent.setup();
    const view = render(
      <SbrSimulatorPanel
        api={api}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={workspace}
      />,
    );
    await runFixture(user);
    view.rerender(
      <SbrSimulatorPanel
        api={api}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={{ ...workspace, sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073997" }}
      />,
    );
    await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
    await user.click(screen.getByRole("button", { name: "Run simulator fixture" }));
    await act(async () =>
      oldOperation.resolve(resultFrame(SbrReadinessFixtureFailure.UNSPECIFIED)),
    );
    fireEvent.submit(
      screen.getByLabelText("Fresh six-digit code").closest("form") as HTMLFormElement,
    );
    expect(api.assertTotp).toHaveBeenCalledTimes(2);
    await act(async () => newAssertion.resolve(factorFrame()));
  });

  it.each([
    {
      name: "EVTE",
      readiness: create(SbrReadinessSchema, {
        environment: SbrEnvironment.EVTE,
        state: SbrReadinessState.READY_FOR_EVTE_PRE_CONFORMANCE,
        machineCredentialState: MachineCredentialState.PRESENT,
        productIdState: ProductIdState.PRESENT,
        evteProductIdentifier: "TAMMY.EVTE",
        evteServiceIdentifier: "BAS.LODGE",
      }),
      workspace,
      expected: /EVTE status only/i,
    },
    {
      name: "simulator not ready",
      readiness: create(SbrReadinessSchema, {
        environment: SbrEnvironment.SIMULATOR,
        state: SbrReadinessState.UNAVAILABLE,
        machineCredentialState: MachineCredentialState.MISSING,
        productIdState: ProductIdState.MISSING,
      }),
      workspace,
      expected: /simulator is not ready/i,
    },
    {
      name: "wrong role",
      readiness: simulatorReadiness,
      workspace: { ...workspace, roles: [Role.BUSINESS_LODGER] },
      expected: /workspace administrator role/i,
    },
  ])(
    "keeps $name status-only with no component operation",
    ({ readiness, workspace, expected }) => {
      const api = apiFor();
      render(
        <SbrSimulatorPanel
          api={api}
          onRefresh={vi.fn()}
          readiness={readiness}
          workspace={workspace}
        />,
      );
      expect(screen.getByText(expected)).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Run simulator fixture" })).toBeNull();
      expect(api.runSbrReadinessFixture).not.toHaveBeenCalled();
    },
  );

  it("keeps one live region mounted across operation states", async () => {
    const user = userEvent.setup();
    render(
      <SbrSimulatorPanel
        api={apiFor()}
        onRefresh={vi.fn()}
        readiness={simulatorReadiness}
        workspace={workspace}
      />,
    );
    const status = screen.getByRole("status");
    await runFixture(user);
    await screen.findByText("ACCEPTED");
    expect(screen.getByRole("status")).toBe(status);
    expect(status.textContent).toContain("ACCEPTED");
  });
});
