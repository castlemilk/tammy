import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { FreshFactorContextSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  AssertTOTPRequestSchema,
  AssertTOTPResponseSchema,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  ImportMachineCredentialRequestSchema,
  ImportMachineCredentialResponseSchema,
  MachineCredentialState,
  MachineCredentialStatusSchema,
  RemoveMachineCredentialRequestSchema,
  RemoveMachineCredentialResponseSchema,
  ReplaceMachineCredentialRequestSchema,
  ReplaceMachineCredentialResponseSchema,
  UnlockMachineCredentialRequestSchema,
  UnlockMachineCredentialResponseSchema,
} from "@tammy/connect-client/tammy/v1/sbr_pb.js";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode } from "react";
import { expect, it, vi } from "vitest";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { MachineCredentialForm } from "./machine-credential-form";

const workspace = {
  workspaceId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073991",
  userId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
  sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073993",
  organisationDisplayName: "Wattle & Co",
  organisationCanonicalAbn: "11000000560",
};
const handle = "01900f3c-7b2e-7cc4-98c4-dc0c0c073994";
const assertCodec = createProtoMethodCodec({
  input: AssertTOTPRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 8_192,
  output: AssertTOTPResponseSchema,
});
function factorAPI() {
  return vi.fn((frame: Uint8Array) => {
    const request = assertCodec.decodeRequest(frame);
    return Promise.resolve(
      assertCodec.encodeResponse(
        create(AssertTOTPResponseSchema, {
          freshFactor: create(FreshFactorContextSchema, {
            assertionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073995",
            purpose: request.purpose,
            assertedAt: create(TimestampSchema, { seconds: 1n }),
          }),
        }),
      ),
    );
  });
}
const importResponseCodec = createProtoMethodCodec({
  input: ImportMachineCredentialRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: ImportMachineCredentialResponseSchema,
});
const replaceCodec = createProtoMethodCodec({
  input: ReplaceMachineCredentialRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: ReplaceMachineCredentialResponseSchema,
});
const unlockCodec = createProtoMethodCodec({
  input: UnlockMachineCredentialRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: UnlockMachineCredentialResponseSchema,
});
const removeCodec = createProtoMethodCodec({
  input: RemoveMachineCredentialRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: RemoveMachineCredentialResponseSchema,
});
function unused() {
  return vi.fn().mockRejectedValue(new Error("unused"));
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((promiseResolve) => {
    resolve = promiseResolve;
  });
  return { promise, resolve };
}

it("uses the native picker, purpose-bound factor, and invokes import once without rendering path or secrets", async () => {
  const importMachineCredential = vi.fn(() =>
    Promise.resolve(
      importResponseCodec.encodeResponse(
        create(ImportMachineCredentialResponseSchema, {
          credentialStatus: create(MachineCredentialStatusSchema, {
            state: MachineCredentialState.PRESENT,
          }),
        }),
      ),
    ),
  );
  const api = {
    assertTotp: factorAPI(),
    selectMachineCredentialFile: vi.fn().mockResolvedValue({ selected: true as const, handle }),
    importMachineCredential,
    replaceMachineCredential: unused(),
    unlockMachineCredential: unused(),
    removeMachineCredential: unused(),
  };
  const user = userEvent.setup();
  render(
    <StrictMode>
      <MachineCredentialForm
        api={api}
        credentialState={MachineCredentialState.MISSING}
        onChanged={vi.fn()}
        workspace={workspace}
      />
    </StrictMode>,
  );
  expect(document.querySelector('input[type="file"]')).toBeNull();
  await user.click(screen.getByRole("button", { name: "Import credential" }));
  await user.dblClick(screen.getByRole("button", { name: "Choose credential in macOS" }));
  expect(api.selectMachineCredentialFile).toHaveBeenCalledOnce();
  await user.type(screen.getByLabelText("Credential password"), "PRIVATE-PASSWORD");
  await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
  await user.dblClick(screen.getByRole("button", { name: "Continue" }));
  expect(importMachineCredential).toHaveBeenCalledOnce();
  expect(document.body.textContent).not.toContain("PRIVATE-PASSWORD");
  expect(document.body.textContent).not.toContain("/secret/path");
  expect(document.body.textContent).not.toContain(".p12");
});

it("does not mutate after picker cancel or failed factor assertion", async () => {
  const api = {
    assertTotp: vi.fn().mockRejectedValue(new Error("stale totp /secret")),
    selectMachineCredentialFile: vi.fn().mockResolvedValue({ selected: false as const }),
    importMachineCredential: vi.fn(),
    replaceMachineCredential: unused(),
    unlockMachineCredential: unused(),
    removeMachineCredential: unused(),
  };
  const user = userEvent.setup();
  render(
    <MachineCredentialForm
      api={api}
      credentialState={MachineCredentialState.MISSING}
      onChanged={vi.fn()}
      workspace={workspace}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Import credential" }));
  await user.click(screen.getByRole("button", { name: "Choose credential in macOS" }));
  expect(await screen.findByText(/no credential was selected/i)).toBeTruthy();
  expect(api.importMachineCredential).not.toHaveBeenCalled();
  expect(document.body.textContent).not.toContain("/secret");
});

it.each(["expired handle /private/path", "invalid password PRIVATE", "wrong ABN 00000000000"])(
  "treats a dispatched %s failure as unknown without replaying or exposing details",
  async (detail) => {
    const importMachineCredential = vi.fn().mockRejectedValue(new Error(detail));
    const api = {
      assertTotp: factorAPI(),
      selectMachineCredentialFile: vi.fn().mockResolvedValue({ selected: true as const, handle }),
      importMachineCredential,
      replaceMachineCredential: unused(),
      unlockMachineCredential: unused(),
      removeMachineCredential: unused(),
    };
    const user = userEvent.setup();
    render(
      <MachineCredentialForm
        api={api}
        credentialState={MachineCredentialState.MISSING}
        onChanged={vi.fn()}
        workspace={workspace}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Import credential" }));
    await user.click(screen.getByRole("button", { name: "Choose credential in macOS" }));
    await user.type(screen.getByLabelText("Credential password"), "PRIVATE-PASSWORD");
    await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    expect(
      await screen.findByText(/outcome is unknown.*refresh status before trying again/i),
    ).toBeTruthy();
    expect(importMachineCredential).toHaveBeenCalledOnce();
    expect(document.body.textContent).not.toContain(detail);
    expect(document.body.textContent).not.toContain("PRIVATE-PASSWORD");
  },
);

it("names the organisation and ABN in both replace and remove confirmations", async () => {
  const api = {
    assertTotp: factorAPI(),
    selectMachineCredentialFile: vi.fn(),
    importMachineCredential: unused(),
    replaceMachineCredential: unused(),
    unlockMachineCredential: unused(),
    removeMachineCredential: unused(),
  };
  const user = userEvent.setup();
  render(
    <MachineCredentialForm
      api={api}
      credentialState={MachineCredentialState.PRESENT}
      onChanged={vi.fn()}
      workspace={workspace}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Replace credential" }));
  expect(screen.getByText(/Replace the credential for Wattle & Co, ABN 11000000560/i)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  await user.click(screen.getByRole("button", { name: "Remove credential" }));
  expect(screen.getByText(/Remove the credential for Wattle & Co, ABN 11000000560/i)).toBeTruthy();
});

it("replaces through a fresh native selection and removes through separate purpose-bound commands", async () => {
  const assertTotp = factorAPI();
  const replaceMachineCredential = vi.fn(({ command }: { command: Uint8Array }) => {
    expect(replaceCodec.decodeRequest(command).commandContext?.freshFactor?.purpose).toBe(
      "sbr_machine_credential_replace",
    );
    return Promise.resolve(
      replaceCodec.encodeResponse(
        create(ReplaceMachineCredentialResponseSchema, {
          credentialStatus: create(MachineCredentialStatusSchema, {
            state: MachineCredentialState.PRESENT,
          }),
        }),
      ),
    );
  });
  const removeMachineCredential = vi.fn((command: Uint8Array) => {
    expect(removeCodec.decodeRequest(command).commandContext?.freshFactor?.purpose).toBe(
      "sbr_machine_credential_remove",
    );
    return Promise.resolve(
      removeCodec.encodeResponse(
        create(RemoveMachineCredentialResponseSchema, {
          credentialStatus: create(MachineCredentialStatusSchema, {
            state: MachineCredentialState.MISSING,
          }),
        }),
      ),
    );
  });
  const api = {
    assertTotp,
    selectMachineCredentialFile: vi.fn().mockResolvedValue({ selected: true as const, handle }),
    importMachineCredential: unused(),
    replaceMachineCredential,
    unlockMachineCredential: unused(),
    removeMachineCredential,
  };
  const user = userEvent.setup();
  const view = render(
    <MachineCredentialForm
      api={api}
      credentialState={MachineCredentialState.PRESENT}
      onChanged={vi.fn()}
      workspace={workspace}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Replace credential" }));
  await user.click(screen.getByRole("button", { name: "Choose credential in macOS" }));
  await user.click(screen.getByText(/Replace the credential for/i));
  await user.type(screen.getByLabelText("Credential password"), "PRIVATE-PASSWORD");
  await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(replaceMachineCredential).toHaveBeenCalledOnce();
  view.rerender(
    <MachineCredentialForm
      api={api}
      credentialState={MachineCredentialState.PRESENT}
      onChanged={vi.fn()}
      workspace={workspace}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Remove credential" }));
  await user.click(screen.getByText(/Remove the credential for/i));
  await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(removeMachineCredential).toHaveBeenCalledOnce();
});

it("unlocks once with the exact purpose and reports that no network request started", async () => {
  const assertTotp = factorAPI();
  const unlockMachineCredential = vi.fn(
    ({ command }: { command: Uint8Array; password: string }) => {
      expect(unlockCodec.decodeRequest(command).commandContext?.freshFactor?.purpose).toBe(
        "sbr_machine_credential_unlock",
      );
      return Promise.resolve(
        unlockCodec.encodeResponse(
          create(UnlockMachineCredentialResponseSchema, {
            credentialStatus: create(MachineCredentialStatusSchema, {
              state: MachineCredentialState.PRESENT,
            }),
          }),
        ),
      );
    },
  );
  const api = {
    assertTotp,
    selectMachineCredentialFile: vi.fn(),
    importMachineCredential: unused(),
    replaceMachineCredential: unused(),
    unlockMachineCredential,
    removeMachineCredential: unused(),
  };
  const user = userEvent.setup();
  render(
    <MachineCredentialForm
      api={api}
      credentialState={MachineCredentialState.PRESENT}
      onChanged={vi.fn()}
      workspace={workspace}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Unlock for local use" }));
  await user.type(screen.getByLabelText("Credential password"), "PRIVATE-PASSWORD");
  await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
  await user.dblClick(screen.getByRole("button", { name: "Continue" }));
  expect(unlockMachineCredential).toHaveBeenCalledOnce();
  expect(await screen.findByText(/No network request was started/i)).toBeTruthy();
});

it("fences a late mutation response after unmount", async () => {
  const late = deferred<Uint8Array>();
  const changed = vi.fn();
  const api = {
    assertTotp: factorAPI(),
    selectMachineCredentialFile: vi.fn().mockResolvedValue({ selected: true as const, handle }),
    importMachineCredential: vi.fn(() => late.promise),
    replaceMachineCredential: unused(),
    unlockMachineCredential: unused(),
    removeMachineCredential: unused(),
  };
  const user = userEvent.setup();
  const view = render(
    <MachineCredentialForm
      api={api}
      credentialState={MachineCredentialState.MISSING}
      onChanged={changed}
      workspace={workspace}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Import credential" }));
  await user.click(screen.getByRole("button", { name: "Choose credential in macOS" }));
  await user.type(screen.getByLabelText("Credential password"), "PRIVATE-PASSWORD");
  await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  view.unmount();
  await act(async () => {
    late.resolve(
      importResponseCodec.encodeResponse(
        create(ImportMachineCredentialResponseSchema, {
          credentialStatus: create(MachineCredentialStatusSchema, {
            state: MachineCredentialState.PRESENT,
          }),
        }),
      ),
    );
    await late.promise;
  });
  expect(changed).not.toHaveBeenCalled();
  expect(document.body.textContent).not.toContain("PRIVATE-PASSWORD");
});

it("does not dispatch a credential mutation when unmounted during factor assertion", async () => {
  const factor = deferred<Uint8Array>();
  const importMachineCredential = vi.fn();
  const api = {
    assertTotp: vi.fn(() => factor.promise),
    selectMachineCredentialFile: vi.fn().mockResolvedValue({ selected: true as const, handle }),
    importMachineCredential,
    replaceMachineCredential: unused(),
    unlockMachineCredential: unused(),
    removeMachineCredential: unused(),
  };
  const user = userEvent.setup();
  const view = render(
    <MachineCredentialForm
      api={api}
      credentialState={MachineCredentialState.MISSING}
      onChanged={vi.fn()}
      workspace={workspace}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Import credential" }));
  await user.click(screen.getByRole("button", { name: "Choose credential in macOS" }));
  await user.type(screen.getByLabelText("Credential password"), "PRIVATE-PASSWORD");
  await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  view.unmount();
  await act(async () => {
    factor.resolve(
      assertCodec.encodeResponse(
        create(AssertTOTPResponseSchema, {
          freshFactor: create(FreshFactorContextSchema, {
            assertionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073995",
            purpose: "sbr_machine_credential_import",
            assertedAt: create(TimestampSchema, { seconds: 1n }),
          }),
        }),
      ),
    );
    await factor.promise;
  });
  expect(importMachineCredential).not.toHaveBeenCalled();
});

it("fails closed when an import response does not report the exact terminal state", async () => {
  const changed = vi.fn();
  const api = {
    assertTotp: factorAPI(),
    selectMachineCredentialFile: vi.fn().mockResolvedValue({ selected: true as const, handle }),
    importMachineCredential: vi.fn(() =>
      Promise.resolve(
        importResponseCodec.encodeResponse(
          create(ImportMachineCredentialResponseSchema, {
            credentialStatus: create(MachineCredentialStatusSchema, {
              state: MachineCredentialState.MISSING,
            }),
          }),
        ),
      ),
    ),
    replaceMachineCredential: unused(),
    unlockMachineCredential: unused(),
    removeMachineCredential: unused(),
  };
  const user = userEvent.setup();
  render(
    <MachineCredentialForm
      api={api}
      credentialState={MachineCredentialState.MISSING}
      onChanged={changed}
      workspace={workspace}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Import credential" }));
  await user.click(screen.getByRole("button", { name: "Choose credential in macOS" }));
  await user.type(screen.getByLabelText("Credential password"), "PRIVATE");
  await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(await screen.findByText(/outcome is unknown/i)).toBeTruthy();
  expect(changed).not.toHaveBeenCalled();
});
