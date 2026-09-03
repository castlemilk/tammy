import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { OneTimeSecretOutputSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  ConfirmTOTPRequestSchema,
  ConfirmTOTPResponseSchema,
  EnrolTOTPRequestSchema,
  EnrolTOTPResponseSchema,
  FactorSchema,
  FactorState,
  GetCurrentUserRequestSchema,
  GetCurrentUserResponseSchema,
  UserSchema,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode } from "react";
import { expect, it, vi } from "vitest";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { TotpSetup } from "./totp-setup";

const workspace = {
  workspaceId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073991",
  userId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
  sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073993",
  userFactorState: FactorState.DISABLED,
};
const factorId = "01900f3c-7b2e-7cc4-98c4-dc0c0c073994";
const enrolCodec = createProtoMethodCodec({
  input: EnrolTOTPRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 8_192,
  output: EnrolTOTPResponseSchema,
});
const confirmCodec = createProtoMethodCodec({
  input: ConfirmTOTPRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 8_192,
  output: ConfirmTOTPResponseSchema,
});
const currentUserCodec = createProtoMethodCodec({
  input: GetCurrentUserRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 8_192,
  output: GetCurrentUserResponseSchema,
});
const currentUser = (state: FactorState, userId = workspace.userId) =>
  currentUserCodec.encodeResponse(
    create(GetCurrentUserResponseSchema, {
      user: create(UserSchema, { id: userId, factorState: state }),
    }),
  );

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((promiseResolve) => {
    resolve = promiseResolve;
  });
  return { promise, resolve };
}

it("shows provisioning material once, confirms it, and removes it permanently", async () => {
  const secret = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP";
  let enrolResponseFrame: Uint8Array | undefined;
  const api = {
    enrolTotp: vi.fn((frame: Uint8Array) => {
      expect(new TextDecoder().decode(enrolCodec.decodeRequest(frame).currentPassword?.utf8)).toBe(
        "administrator-password",
      );
      enrolResponseFrame = enrolCodec.encodeResponse(
        create(EnrolTOTPResponseSchema, {
          factor: create(FactorSchema, {
            id: factorId,
            userId: workspace.userId,
            version: 1n,
            state: FactorState.PENDING_CONFIRMATION,
            createdAt: create(TimestampSchema, { seconds: 1n }),
          }),
          provisioningSecret: create(OneTimeSecretOutputSchema, {
            utf8: new TextEncoder().encode(secret),
          }),
        }),
      );
      return Promise.resolve(enrolResponseFrame);
    }),
    confirmTotp: vi.fn((frame: Uint8Array) => {
      expect(confirmCodec.decodeRequest(frame).code?.value).toBe("123456");
      return Promise.resolve(
        confirmCodec.encodeResponse(
          create(ConfirmTOTPResponseSchema, {
            factor: create(FactorSchema, {
              id: factorId,
              userId: workspace.userId,
              version: 2n,
              state: FactorState.ENABLED,
              createdAt: create(TimestampSchema, { seconds: 1n }),
            }),
          }),
        ),
      );
    }),
    getCurrentUser: vi.fn(),
  };
  const enabled = vi.fn();
  const user = userEvent.setup();
  render(
    <StrictMode>
      <TotpSetup api={api} onEnabled={enabled} workspace={workspace} />
    </StrictMode>,
  );
  await user.type(
    screen.getByLabelText("Current administrator password"),
    "administrator-password",
  );
  await user.dblClick(screen.getByRole("button", { name: "Begin TOTP setup" }));
  expect(api.enrolTotp).toHaveBeenCalledOnce();
  expect(await screen.findByText(secret)).toBeTruthy();
  expect(enrolResponseFrame?.every((value) => value === 0)).toBe(true);
  await user.type(screen.getByLabelText("Six-digit code"), "123456");
  await user.dblClick(screen.getByRole("button", { name: "Confirm security code" }));
  expect(api.confirmTotp).toHaveBeenCalledOnce();
  expect(enabled).toHaveBeenCalledOnce();
  expect(screen.queryByText(secret)).toBeNull();
});

it("drops provisioning material on unmount and never renders a rejected password", async () => {
  const api = {
    enrolTotp: vi.fn().mockRejectedValue(new Error("PRIVATE-PASSWORD")),
    confirmTotp: vi.fn(),
    getCurrentUser: vi.fn(() => Promise.resolve(currentUser(FactorState.DISABLED))),
  };
  const user = userEvent.setup();
  const view = render(<TotpSetup api={api} onEnabled={vi.fn()} workspace={workspace} />);
  await user.type(screen.getByLabelText("Current administrator password"), "PRIVATE-PASSWORD");
  await user.click(screen.getByRole("button", { name: "Begin TOTP setup" }));
  expect((await screen.findByRole("alert")).textContent).toMatch(/begin setup again/i);
  expect(document.body.textContent).not.toContain("PRIVATE-PASSWORD");
  view.unmount();
});

it("keeps one live error region mounted from first paint", async () => {
  const api = {
    enrolTotp: vi.fn().mockRejectedValue(new Error("timeout")),
    confirmTotp: vi.fn(),
    getCurrentUser: vi.fn().mockRejectedValue(new Error("core restarted")),
  };
  const user = userEvent.setup();
  render(<TotpSetup api={api} onEnabled={vi.fn()} workspace={workspace} />);
  const liveRegion = screen.getByRole("alert");
  expect(liveRegion.textContent).toBe("");

  await user.type(screen.getByLabelText("Current administrator password"), "password");
  await user.click(screen.getByRole("button", { name: "Begin TOTP setup" }));

  expect(screen.getByRole("alert")).toBe(liveRegion);
  expect(liveRegion.textContent).toMatch(/unavailable/i);
});

it("ignores an enrolment response that arrives after unmount", async () => {
  const late = deferred<Uint8Array>();
  const api = {
    enrolTotp: vi.fn(() => late.promise),
    confirmTotp: vi.fn(),
    getCurrentUser: vi.fn(),
  };
  const user = userEvent.setup();
  const view = render(<TotpSetup api={api} onEnabled={vi.fn()} workspace={workspace} />);
  await user.type(screen.getByLabelText("Current administrator password"), "PRIVATE-PASSWORD");
  await user.click(screen.getByRole("button", { name: "Begin TOTP setup" }));
  view.unmount();
  await act(async () => {
    late.resolve(
      enrolCodec.encodeResponse(
        create(EnrolTOTPResponseSchema, {
          factor: create(FactorSchema, {
            id: factorId,
            userId: workspace.userId,
            version: 1n,
            state: FactorState.PENDING_CONFIRMATION,
            createdAt: create(TimestampSchema, { seconds: 1n }),
          }),
          provisioningSecret: create(OneTimeSecretOutputSchema, {
            utf8: new TextEncoder().encode("JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"),
          }),
        }),
      ),
    );
    await late.promise;
  });
  expect(document.body.textContent).not.toContain("LATE-PRIVATE-SECRET");
});

it("does not install enrolment material returned for a previous principal", async () => {
  const secret = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP";
  const late = deferred<Uint8Array>();
  const api = {
    enrolTotp: vi.fn(() => late.promise),
    confirmTotp: vi.fn(),
    getCurrentUser: vi.fn(),
  };
  const user = userEvent.setup();
  const enabled = vi.fn();
  const view = render(<TotpSetup api={api} onEnabled={enabled} workspace={workspace} />);
  await user.type(screen.getByLabelText("Current administrator password"), "password");
  await user.click(screen.getByRole("button", { name: "Begin TOTP setup" }));

  view.rerender(
    <TotpSetup
      api={api}
      onEnabled={enabled}
      workspace={{
        ...workspace,
        sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073995",
        userId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073996",
      }}
    />,
  );
  const responseFrame = enrolCodec.encodeResponse(
    create(EnrolTOTPResponseSchema, {
      factor: create(FactorSchema, {
        id: factorId,
        userId: workspace.userId,
        version: 1n,
        state: FactorState.PENDING_CONFIRMATION,
        createdAt: create(TimestampSchema, { seconds: 1n }),
      }),
      provisioningSecret: create(OneTimeSecretOutputSchema, {
        utf8: new TextEncoder().encode(secret),
      }),
    }),
  );
  await act(async () => {
    late.resolve(responseFrame);
    await late.promise;
  });

  expect(screen.queryByTestId("totp-provisioning-material")).toBeNull();
  expect(document.body.textContent).not.toContain(secret);
  expect(responseFrame.every((value) => value === 0)).toBe(true);
  expect(enabled).not.toHaveBeenCalled();
});

it("does not enable a renewed session from a previous session confirmation", async () => {
  const secret = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP";
  const late = deferred<Uint8Array>();
  let confirmRequestFrame: Uint8Array | undefined;
  const api = {
    enrolTotp: vi.fn(() =>
      Promise.resolve(
        enrolCodec.encodeResponse(
          create(EnrolTOTPResponseSchema, {
            factor: create(FactorSchema, {
              id: factorId,
              userId: workspace.userId,
              version: 1n,
              state: FactorState.PENDING_CONFIRMATION,
              createdAt: create(TimestampSchema, { seconds: 1n }),
            }),
            provisioningSecret: create(OneTimeSecretOutputSchema, {
              utf8: new TextEncoder().encode(secret),
            }),
          }),
        ),
      ),
    ),
    confirmTotp: vi.fn((frame: Uint8Array) => {
      confirmRequestFrame = frame;
      return late.promise;
    }),
    getCurrentUser: vi.fn(),
  };
  const user = userEvent.setup();
  const enabled = vi.fn();
  const view = render(<TotpSetup api={api} onEnabled={enabled} workspace={workspace} />);
  await user.type(screen.getByLabelText("Current administrator password"), "password");
  await user.click(screen.getByRole("button", { name: "Begin TOTP setup" }));
  await screen.findByText(secret);
  await user.type(screen.getByLabelText("Six-digit code"), "123456");
  await user.click(screen.getByRole("button", { name: "Confirm security code" }));

  view.rerender(
    <TotpSetup
      api={api}
      onEnabled={enabled}
      workspace={{
        ...workspace,
        sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073995",
      }}
    />,
  );
  await act(async () => {
    late.resolve(
      confirmCodec.encodeResponse(
        create(ConfirmTOTPResponseSchema, {
          factor: create(FactorSchema, {
            id: factorId,
            userId: workspace.userId,
            version: 2n,
            state: FactorState.ENABLED,
            createdAt: create(TimestampSchema, { seconds: 1n }),
          }),
        }),
      ),
    );
    await late.promise;
  });

  expect(enabled).not.toHaveBeenCalled();
  expect(confirmRequestFrame?.every((value) => value === 0)).toBe(true);
  expect(screen.getByRole("button", { name: "Begin TOTP setup" })).toBeTruthy();
  expect(document.body.textContent).not.toContain(secret);
});

it("does not apply a nested security refresh returned for a previous principal", async () => {
  const lateRefresh = deferred<Uint8Array>();
  const api = {
    enrolTotp: vi.fn().mockRejectedValue(new Error("timeout")),
    confirmTotp: vi.fn(),
    getCurrentUser: vi.fn(() => lateRefresh.promise),
  };
  const user = userEvent.setup();
  const enabled = vi.fn();
  const view = render(<TotpSetup api={api} onEnabled={enabled} workspace={workspace} />);
  await user.type(screen.getByLabelText("Current administrator password"), "password");
  await user.click(screen.getByRole("button", { name: "Begin TOTP setup" }));
  expect(api.getCurrentUser).toHaveBeenCalledOnce();

  const nextWorkspace = {
    ...workspace,
    sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073995",
    userId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073996",
  };
  view.rerender(<TotpSetup api={api} onEnabled={enabled} workspace={nextWorkspace} />);
  await act(async () => {
    lateRefresh.resolve(currentUser(FactorState.ENABLED, workspace.userId));
    await lateRefresh.promise;
  });

  expect(enabled).not.toHaveBeenCalled();
  expect(screen.getByRole("button", { name: "Begin TOTP setup" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Refresh security status" })).toBeNull();
});

it("rejects malformed factor metadata without displaying returned provisioning material", async () => {
  const secret = "MALFORMED-PRIVATE-SECRET";
  const api = {
    enrolTotp: vi.fn(() =>
      Promise.resolve(
        enrolCodec.encodeResponse(
          create(EnrolTOTPResponseSchema, {
            factor: create(FactorSchema, {
              id: factorId,
              userId: workspace.userId,
              version: 0n,
              state: FactorState.PENDING_CONFIRMATION,
            }),
            provisioningSecret: create(OneTimeSecretOutputSchema, {
              utf8: new TextEncoder().encode(secret),
            }),
          }),
        ),
      ),
    ),
    confirmTotp: vi.fn(),
    getCurrentUser: vi.fn(() => Promise.resolve(currentUser(FactorState.DISABLED))),
  };
  const user = userEvent.setup();
  render(<TotpSetup api={api} onEnabled={vi.fn()} workspace={workspace} />);
  await user.type(screen.getByLabelText("Current administrator password"), "PRIVATE-PASSWORD");
  await user.click(screen.getByRole("button", { name: "Begin TOTP setup" }));
  expect(await screen.findByRole("alert")).toBeTruthy();
  expect(document.body.textContent).not.toContain(secret);
});

it("restarts an authoritative pending setup and requests atomic seed rotation", async () => {
  const pendingWorkspace = { ...workspace, userFactorState: FactorState.PENDING_CONFIRMATION };
  const api = {
    enrolTotp: vi.fn((frame: Uint8Array) => {
      expect(enrolCodec.decodeRequest(frame).restartPending).toBe(true);
      return Promise.resolve(
        enrolCodec.encodeResponse(
          create(EnrolTOTPResponseSchema, {
            factor: create(FactorSchema, {
              id: factorId,
              userId: workspace.userId,
              version: 1n,
              state: FactorState.PENDING_CONFIRMATION,
              createdAt: create(TimestampSchema, { seconds: 1n }),
            }),
            provisioningSecret: create(OneTimeSecretOutputSchema, {
              utf8: new TextEncoder().encode("JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"),
            }),
          }),
        ),
      );
    }),
    confirmTotp: vi.fn(),
    getCurrentUser: vi.fn(),
  };
  const user = userEvent.setup();
  render(<TotpSetup api={api} onEnabled={vi.fn()} workspace={pendingWorkspace} />);
  await user.type(screen.getByLabelText("Current administrator password"), "password");
  await user.click(screen.getByRole("button", { name: "Restart TOTP setup" }));
  expect(await screen.findByTestId("totp-provisioning-material")).toBeTruthy();
  expect(api.enrolTotp).toHaveBeenCalledOnce();
});

it("locks an ambiguous enrolment until an authoritative refresh succeeds", async () => {
  const api = {
    enrolTotp: vi.fn().mockRejectedValue(new Error("timeout")),
    confirmTotp: vi.fn(),
    getCurrentUser: vi.fn().mockRejectedValue(new Error("core restarted")),
  };
  const user = userEvent.setup();
  render(<TotpSetup api={api} onEnabled={vi.fn()} workspace={workspace} />);
  await user.type(screen.getByLabelText("Current administrator password"), "password");
  await user.click(screen.getByRole("button", { name: "Begin TOTP setup" }));
  expect(await screen.findByRole("button", { name: "Refresh security status" })).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Begin TOTP setup" }));
  expect(api.enrolTotp).toHaveBeenCalledOnce();
  api.getCurrentUser.mockResolvedValueOnce(currentUser(FactorState.PENDING_CONFIRMATION));
  await user.click(screen.getByRole("button", { name: "Refresh security status" }));
  expect(await screen.findByRole("button", { name: "Restart TOTP setup" })).toBeTruthy();
});

it("rejects non-base32 provisioning shapes without ever displaying them", async () => {
  const malformed = "JBSWY3DP\nHPK3PXPJBSWY3DPEHPK3PX";
  const api = {
    enrolTotp: vi.fn(() =>
      Promise.resolve(
        enrolCodec.encodeResponse(
          create(EnrolTOTPResponseSchema, {
            factor: create(FactorSchema, {
              id: factorId,
              userId: workspace.userId,
              version: 1n,
              state: FactorState.PENDING_CONFIRMATION,
              createdAt: create(TimestampSchema, { seconds: 1n }),
            }),
            provisioningSecret: create(OneTimeSecretOutputSchema, {
              utf8: new TextEncoder().encode(malformed),
            }),
          }),
        ),
      ),
    ),
    confirmTotp: vi.fn(),
    getCurrentUser: vi.fn(() => Promise.resolve(currentUser(FactorState.DISABLED))),
  };
  const user = userEvent.setup();
  render(<TotpSetup api={api} onEnabled={vi.fn()} workspace={workspace} />);
  await user.type(screen.getByLabelText("Current administrator password"), "password");
  await user.click(screen.getByRole("button", { name: "Begin TOTP setup" }));
  await screen.findByRole("alert");
  expect(document.body.textContent).not.toContain(malformed);
});

it("refreshes authoritative factor state after an ambiguous confirmation", async () => {
  const secret = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP";
  const api = {
    enrolTotp: vi.fn(() =>
      Promise.resolve(
        enrolCodec.encodeResponse(
          create(EnrolTOTPResponseSchema, {
            factor: create(FactorSchema, {
              id: factorId,
              userId: workspace.userId,
              version: 1n,
              state: FactorState.PENDING_CONFIRMATION,
              createdAt: create(TimestampSchema, { seconds: 1n }),
            }),
            provisioningSecret: create(OneTimeSecretOutputSchema, {
              utf8: new TextEncoder().encode(secret),
            }),
          }),
        ),
      ),
    ),
    confirmTotp: vi.fn().mockRejectedValue(new Error("timeout after commit")),
    getCurrentUser: vi.fn(() => Promise.resolve(currentUser(FactorState.ENABLED))),
  };
  const enabled = vi.fn();
  const user = userEvent.setup();
  render(<TotpSetup api={api} onEnabled={enabled} workspace={workspace} />);
  await user.type(screen.getByLabelText("Current administrator password"), "password");
  await user.click(screen.getByRole("button", { name: "Begin TOTP setup" }));
  await screen.findByText(secret);
  await user.type(screen.getByLabelText("Six-digit code"), "123456");
  await user.click(screen.getByRole("button", { name: "Confirm security code" }));
  expect(enabled).toHaveBeenCalledOnce();
  expect(api.getCurrentUser).toHaveBeenCalledOnce();
  expect(document.body.textContent).not.toContain(secret);
});

it("does not redisplay provisioning material after navigation", async () => {
  const secret = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP";
  const api = {
    enrolTotp: vi.fn(() =>
      Promise.resolve(
        enrolCodec.encodeResponse(
          create(EnrolTOTPResponseSchema, {
            factor: create(FactorSchema, {
              id: factorId,
              userId: workspace.userId,
              version: 1n,
              state: FactorState.PENDING_CONFIRMATION,
              createdAt: create(TimestampSchema, { seconds: 1n }),
            }),
            provisioningSecret: create(OneTimeSecretOutputSchema, {
              utf8: new TextEncoder().encode(secret),
            }),
          }),
        ),
      ),
    ),
    confirmTotp: vi.fn(),
    getCurrentUser: vi.fn(),
  };
  const user = userEvent.setup();
  const view = render(<TotpSetup api={api} onEnabled={vi.fn()} workspace={workspace} />);
  await user.type(screen.getByLabelText("Current administrator password"), "password");
  await user.click(screen.getByRole("button", { name: "Begin TOTP setup" }));
  await screen.findByText(secret);
  view.unmount();
  render(
    <TotpSetup
      api={api}
      onEnabled={vi.fn()}
      workspace={{ ...workspace, userFactorState: FactorState.PENDING_CONFIRMATION }}
    />,
  );
  expect(screen.getByRole("button", { name: "Restart TOTP setup" })).toBeTruthy();
  expect(document.body.textContent).not.toContain(secret);
});
