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

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((promiseResolve) => {
    resolve = promiseResolve;
  });
  return { promise, resolve };
}

it("shows provisioning material once, confirms it, and removes it permanently", async () => {
  const secret = "otpauth://totp/Tammy?secret=ONLYONCE";
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
  };
  const user = userEvent.setup();
  const view = render(<TotpSetup api={api} onEnabled={vi.fn()} workspace={workspace} />);
  await user.type(screen.getByLabelText("Current administrator password"), "PRIVATE-PASSWORD");
  await user.click(screen.getByRole("button", { name: "Begin TOTP setup" }));
  expect((await screen.findByRole("alert")).textContent).toMatch(/check your password/i);
  expect(document.body.textContent).not.toContain("PRIVATE-PASSWORD");
  view.unmount();
});

it("ignores an enrolment response that arrives after unmount", async () => {
  const late = deferred<Uint8Array>();
  const api = { enrolTotp: vi.fn(() => late.promise), confirmTotp: vi.fn() };
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
            utf8: new TextEncoder().encode("LATE-PRIVATE-SECRET"),
          }),
        }),
      ),
    );
    await late.promise;
  });
  expect(document.body.textContent).not.toContain("LATE-PRIVATE-SECRET");
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
  };
  const user = userEvent.setup();
  render(<TotpSetup api={api} onEnabled={vi.fn()} workspace={workspace} />);
  await user.type(screen.getByLabelText("Current administrator password"), "PRIVATE-PASSWORD");
  await user.click(screen.getByRole("button", { name: "Begin TOTP setup" }));
  expect(await screen.findByRole("alert")).toBeTruthy();
  expect(document.body.textContent).not.toContain(secret);
});
