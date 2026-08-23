import { create } from "@bufbuild/protobuf";
import {
  ImportMachineCredentialRequestSchema,
  ImportMachineCredentialResponseSchema,
  MachineCredentialState,
  RemoveMachineCredentialRequestSchema,
  RemoveMachineCredentialResponseSchema,
  ReplaceMachineCredentialRequestSchema,
  ReplaceMachineCredentialResponseSchema,
  UnlockMachineCredentialRequestSchema,
  UnlockMachineCredentialResponseSchema,
} from "@tammy/connect-client/tammy/v1/sbr_pb.js";
import { KeyRound, LoaderCircle, Trash2 } from "lucide-react";
import { type FormEvent, useEffect, useRef, useState } from "react";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { Button } from "../../components/ui/button";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import {
  assertFreshFactor,
  commandContext,
  fieldClassName,
  SBR_PURPOSE,
  unknownOutcomeCopy,
} from "./sbr-form-support";

const importCodec = createProtoMethodCodec({
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

type Action = "import" | "remove" | "replace" | "unlock";
type CredentialAPI = Pick<
  TammyDesktopAPI,
  | "assertTotp"
  | "importMachineCredential"
  | "removeMachineCredential"
  | "replaceMachineCredential"
  | "selectMachineCredentialFile"
  | "unlockMachineCredential"
>;

export function MachineCredentialForm({
  api,
  credentialState,
  onChanged,
  workspace,
}: {
  readonly api: CredentialAPI;
  readonly credentialState: MachineCredentialState;
  readonly onChanged: () => void;
  readonly workspace: AuthenticatedWorkspace &
    Required<Pick<AuthenticatedWorkspace, "organisationCanonicalAbn" | "organisationDisplayName">>;
}) {
  const missing = credentialState === MachineCredentialState.MISSING;
  const [action, setAction] = useState<Action>();
  const [selected, setSelected] = useState(false);
  const handle = useRef<string | undefined>(undefined);
  const [password, setPassword] = useState("");
  const [totp, setTotp] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<string>();
  const pickerInFlight = useRef(false);
  const mutationInFlight = useRef(false);
  const mounted = useRef(true);

  const clearTransient = () => {
    handle.current = undefined;
    setSelected(false);
    setPassword("");
    setTotp("");
    setConfirmed(false);
  };
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      handle.current = undefined;
    };
  }, []);
  const chooseAction = (next: Action) => {
    if (pickerInFlight.current || mutationInFlight.current) return;
    clearTransient();
    setNotice(undefined);
    setAction((current) => (current === next ? undefined : next));
  };

  const select = async () => {
    if (pickerInFlight.current || mutationInFlight.current || handle.current) return;
    pickerInFlight.current = true;
    setBusy(true);
    setNotice(undefined);
    clearTransient();
    try {
      const selection = await api.selectMachineCredentialFile();
      if (!mounted.current) return;
      if (!selection.selected) {
        setNotice("No credential was selected.");
        return;
      }
      handle.current = selection.handle;
      setSelected(true);
    } catch {
      if (mounted.current) setNotice("The credential picker was unavailable. No file was read.");
    } finally {
      pickerInFlight.current = false;
      if (mounted.current) setBusy(false);
    }
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!action || mutationInFlight.current || pickerInFlight.current || !/^\d{6}$/.test(totp))
      return;
    if ((action === "import" || action === "replace") && !handle.current) return;
    if ((action === "remove" || action === "replace") && !confirmed) return;
    mutationInFlight.current = true;
    setBusy(true);
    setNotice(undefined);
    const operationHandle = handle.current;
    let mutationStarted = false;
    try {
      const purpose =
        action === "import"
          ? SBR_PURPOSE.importCredential
          : action === "replace"
            ? SBR_PURPOSE.replaceCredential
            : action === "unlock"
              ? SBR_PURPOSE.unlockCredential
              : SBR_PURPOSE.removeCredential;
      const fresh = await assertFreshFactor(api, workspace, totp, purpose);
      if (!mounted.current) return;
      const context = commandContext(workspace, fresh);
      let responseState: MachineCredentialState | undefined;
      if (action === "import" && operationHandle) {
        const command = importCodec.encodeRequest(
          create(ImportMachineCredentialRequestSchema, { commandContext: context }),
        );
        try {
          const pending = api.importMachineCredential({
            command,
            handle: operationHandle,
            password,
          });
          mutationStarted = true;
          clearTransient();
          responseState = importCodec.decodeResponse(await pending).credentialStatus?.state;
        } finally {
          command.fill(0);
        }
      } else if (action === "replace" && operationHandle) {
        const command = replaceCodec.encodeRequest(
          create(ReplaceMachineCredentialRequestSchema, { commandContext: context }),
        );
        try {
          const pending = api.replaceMachineCredential({
            command,
            handle: operationHandle,
            password,
          });
          mutationStarted = true;
          clearTransient();
          responseState = replaceCodec.decodeResponse(await pending).credentialStatus?.state;
        } finally {
          command.fill(0);
        }
      } else if (action === "unlock") {
        const command = unlockCodec.encodeRequest(
          create(UnlockMachineCredentialRequestSchema, { commandContext: context }),
        );
        try {
          const pending = api.unlockMachineCredential({ command, password });
          mutationStarted = true;
          clearTransient();
          responseState = unlockCodec.decodeResponse(await pending).credentialStatus?.state;
        } finally {
          command.fill(0);
        }
      } else {
        const command = removeCodec.encodeRequest(
          create(RemoveMachineCredentialRequestSchema, { commandContext: context }),
        );
        try {
          const pending = api.removeMachineCredential(command);
          mutationStarted = true;
          clearTransient();
          responseState = removeCodec.decodeResponse(await pending).credentialStatus?.state;
        } finally {
          command.fill(0);
        }
      }
      const expectedState =
        action === "remove" ? MachineCredentialState.MISSING : MachineCredentialState.PRESENT;
      if (responseState !== expectedState) throw new Error("invalid response");
      if (!mounted.current) return;
      setAction(undefined);
      setNotice(
        action === "unlock"
          ? "Credential unlocked for local use. No network request was started."
          : "Credential status updated.",
      );
      onChanged();
    } catch {
      if (mounted.current) {
        clearTransient();
        setNotice(
          mutationStarted
            ? unknownOutcomeCopy
            : "Authorization failed. No credential operation was started.",
        );
      }
    } finally {
      mutationInFlight.current = false;
      if (mounted.current) setBusy(false);
    }
  };

  return (
    <section aria-labelledby="machine-credential-heading" className="border-t border-border pt-4">
      <div className="flex items-center gap-2">
        <KeyRound aria-hidden="true" className="size-3.5 text-forest" />
        <h2 id="machine-credential-heading" className="text-[12px] font-semibold">
          RAM machine credential
        </h2>
      </div>
      <p className="mt-1 text-[10px] leading-5 text-muted-foreground">
        The credential stays in protected storage on this Mac. Every operation requires a new
        security code.
      </p>
      <div className="mt-3 flex flex-wrap gap-2">
        {missing ? (
          <Button
            className="h-9 text-[11px]"
            onClick={() => chooseAction("import")}
            type="button"
            variant="outline"
          >
            Import credential
          </Button>
        ) : (
          <>
            <Button
              className="h-9 text-[11px]"
              onClick={() => chooseAction("unlock")}
              type="button"
              variant="outline"
            >
              Unlock for local use
            </Button>
            <Button
              className="h-9 text-[11px]"
              onClick={() => chooseAction("replace")}
              type="button"
              variant="outline"
            >
              Replace credential
            </Button>
            <Button
              className="h-9 text-[11px]"
              onClick={() => chooseAction("remove")}
              type="button"
              variant="outline"
            >
              <Trash2 aria-hidden="true" className="size-3" />
              Remove credential
            </Button>
          </>
        )}
      </div>
      {action ? (
        <form
          className="mt-4 grid max-w-[520px] gap-3 border-l-2 border-forest pl-4"
          onSubmit={submit}
        >
          <h3 className="text-[11px] font-semibold">
            {action === "import"
              ? "Import machine credential"
              : action === "replace"
                ? "Replace machine credential"
                : action === "unlock"
                  ? "Unlock machine credential"
                  : "Remove machine credential"}
          </h3>
          {action === "import" || action === "replace" ? (
            <div>
              <Button
                className="h-9 text-[11px]"
                disabled={busy}
                onClick={select}
                type="button"
                variant="outline"
              >
                Choose credential in macOS
              </Button>
              {selected ? (
                <p className="mt-2 text-[10px] text-ready-foreground">
                  Credential selected. Its filename is not retained or shown.
                </p>
              ) : null}
            </div>
          ) : null}
          {action === "replace" ? (
            <label className="flex items-start gap-2 text-[11px] leading-5">
              <input
                className="mt-1"
                checked={confirmed}
                onChange={(event) => setConfirmed(event.target.checked)}
                type="checkbox"
              />
              Replace the credential for {workspace.organisationDisplayName}, ABN{" "}
              {workspace.organisationCanonicalAbn}.
            </label>
          ) : null}
          {action !== "remove" ? (
            <label className="grid gap-1.5 text-[11px] font-medium">
              Credential password
              <input
                autoComplete="off"
                className={fieldClassName}
                maxLength={1024}
                onChange={(event) => setPassword(event.target.value)}
                required
                type="password"
                value={password}
              />
            </label>
          ) : (
            <label className="flex items-start gap-2 text-[11px] leading-5">
              <input
                className="mt-1"
                checked={confirmed}
                onChange={(event) => setConfirmed(event.target.checked)}
                type="checkbox"
              />
              Remove the credential for {workspace.organisationDisplayName}, ABN{" "}
              {workspace.organisationCanonicalAbn}. Direct SBR will become unavailable.
            </label>
          )}
          <label className="grid max-w-[220px] gap-1.5 text-[11px] font-medium">
            Fresh six-digit code
            <input
              autoComplete="one-time-code"
              className={fieldClassName}
              inputMode="numeric"
              maxLength={6}
              onChange={(event) => setTotp(event.target.value)}
              pattern="[0-9]{6}"
              required
              value={totp}
            />
          </label>
          <div className="flex gap-2">
            <Button
              className="h-9 text-[11px]"
              disabled={busy || ((action === "import" || action === "replace") && !selected)}
              type="submit"
            >
              {busy ? <LoaderCircle aria-hidden="true" className="size-3.5 animate-spin" /> : null}
              Continue
            </Button>
            <Button
              className="h-9 text-[11px]"
              disabled={busy}
              onClick={() => chooseAction(action)}
              type="button"
              variant="ghost"
            >
              Cancel
            </Button>
          </div>
        </form>
      ) : null}
      {notice ? (
        <p aria-live="polite" className="mt-3 text-[11px] text-muted-foreground" role="status">
          {notice}
        </p>
      ) : null}
    </section>
  );
}
