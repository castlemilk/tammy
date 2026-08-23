import { create } from "@bufbuild/protobuf";
import {
  SecretInputSchema,
  TotpCodeInputSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  ConfirmTOTPRequestSchema,
  ConfirmTOTPResponseSchema,
  EnrolTOTPRequestSchema,
  EnrolTOTPResponseSchema,
  FactorState,
  GetCurrentUserRequestSchema,
  GetCurrentUserResponseSchema,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import { LoaderCircle, ShieldCheck } from "lucide-react";
import { type FormEvent, useEffect, useRef, useState } from "react";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { Button } from "../../components/ui/button";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import {
  authentication,
  fieldClassName,
  isUuidV7,
  uuidV7,
  validTimestamp,
} from "./sbr-form-support";

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

export function TotpSetup({
  api,
  onEnabled,
  workspace,
}: {
  readonly api: Pick<TammyDesktopAPI, "confirmTotp" | "enrolTotp" | "getCurrentUser">;
  readonly onEnabled: () => void;
  readonly workspace: AuthenticatedWorkspace & { readonly userFactorState?: FactorState };
}) {
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [pending, setPending] = useState<{ factorId: string; material: string }>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [factorState, setFactorState] = useState(workspace.userFactorState);
  const [outcomeLocked, setOutcomeLocked] = useState(false);
  const materialBytes = useRef<Uint8Array | undefined>(undefined);
  const inFlight = useRef(false);
  const refreshInFlight = useRef(false);
  const mounted = useRef(true);
  const factorPrincipal = useRef(workspace.userId);

  const clearProvisioning = () => {
    materialBytes.current?.fill(0);
    materialBytes.current = undefined;
    setPending(undefined);
    setCode("");
  };
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      materialBytes.current?.fill(0);
      materialBytes.current = undefined;
    };
  }, []);
  useEffect(() => {
    if (factorPrincipal.current !== workspace.userId) factorPrincipal.current = workspace.userId;
    materialBytes.current?.fill(0);
    materialBytes.current = undefined;
    setPending(undefined);
    setCode("");
    setFactorState(workspace.userFactorState);
    setOutcomeLocked(false);
    setError(undefined);
  }, [workspace.userFactorState, workspace.userId]);

  const refreshFactor = async () => {
    if (refreshInFlight.current) return;
    refreshInFlight.current = true;
    try {
      const frame = currentUserCodec.encodeRequest(
        create(GetCurrentUserRequestSchema, { authentication: authentication(workspace) }),
      );
      const responseFrame = await api.getCurrentUser(frame);
      const response = currentUserCodec.decodeResponse(responseFrame);
      if (!mounted.current) return;
      const state = response.user?.factorState;
      if (
        !response.user ||
        response.user.id !== workspace.userId ||
        (state !== undefined &&
          state !== FactorState.PENDING_CONFIRMATION &&
          state !== FactorState.ENABLED &&
          state !== FactorState.DISABLED)
      )
        throw new Error("invalid current user");
      setFactorState(state);
      setOutcomeLocked(false);
      if (state === FactorState.ENABLED) {
        onEnabled();
      } else if (state === FactorState.PENDING_CONFIRMATION) {
        setError("A pending setup was found. Restart setup to invalidate it and create a new key.");
      } else {
        setError("No pending setup was found. You can begin setup again.");
      }
    } catch {
      if (mounted.current) {
        setOutcomeLocked(true);
        setError("Security status is unavailable. Refresh security status before retrying.");
      }
    } finally {
      refreshInFlight.current = false;
    }
  };

  const enrol = async (event: FormEvent) => {
    event.preventDefault();
    if (inFlight.current || outcomeLocked) return;
    inFlight.current = true;
    setBusy(true);
    setError(undefined);
    try {
      const passwordBytes = new TextEncoder().encode(password);
      const request = create(EnrolTOTPRequestSchema, {
        commandContext: { authentication: authentication(workspace), idempotencyKey: uuidV7() },
        currentPassword: create(SecretInputSchema, { utf8: passwordBytes }),
        restartPending: factorState === FactorState.PENDING_CONFIRMATION,
      });
      setPassword("");
      const frame = enrolCodec.encodeRequest(request);
      const response = await (async () => {
        try {
          const responseFrame = await api.enrolTotp(frame);
          try {
            return enrolCodec.decodeResponse(responseFrame);
          } finally {
            responseFrame.fill(0);
          }
        } finally {
          frame.fill(0);
          passwordBytes.fill(0);
          request.currentPassword?.utf8.fill(0);
        }
      })();
      const responseSecret = response.provisioningSecret?.utf8;
      try {
        if (
          !response.factor ||
          !isUuidV7(response.factor.id) ||
          response.factor.userId !== workspace.userId ||
          response.factor.version < 1n ||
          response.factor.state !== FactorState.PENDING_CONFIRMATION ||
          !validTimestamp(response.factor.createdAt) ||
          !responseSecret?.length
        )
          throw new Error("invalid enrolment response");
        const bytes = new Uint8Array(responseSecret);
        const material = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
        if (!/^[A-Z2-7]{32}$/.test(material)) throw new Error("invalid provisioning secret");
        if (!mounted.current) {
          bytes.fill(0);
          return;
        }
        materialBytes.current = bytes;
        setPending({ factorId: response.factor.id, material });
      } finally {
        responseSecret?.fill(0);
      }
    } catch {
      if (mounted.current) {
        clearProvisioning();
        setOutcomeLocked(true);
        setError("TOTP setup outcome is unknown. Refresh security status before retrying.");
        await refreshFactor();
      }
    } finally {
      inFlight.current = false;
      if (mounted.current) setBusy(false);
    }
  };

  const confirm = async (event: FormEvent) => {
    event.preventDefault();
    if (inFlight.current || outcomeLocked || !pending || !/^\d{6}$/.test(code)) return;
    inFlight.current = true;
    setBusy(true);
    setError(undefined);
    try {
      const request = create(ConfirmTOTPRequestSchema, {
        authentication: authentication(workspace),
        factorId: pending.factorId,
        code: create(TotpCodeInputSchema, { value: code }),
      });
      setCode("");
      const frame = confirmCodec.encodeRequest(request);
      const response = await (async () => {
        try {
          return confirmCodec.decodeResponse(await api.confirmTotp(frame));
        } finally {
          frame.fill(0);
        }
      })();
      if (!mounted.current) return;
      if (
        !response.factor ||
        !isUuidV7(response.factor.id) ||
        response.factor.id !== pending.factorId ||
        response.factor.userId !== workspace.userId ||
        response.factor.version < 1n ||
        response.factor.state !== FactorState.ENABLED ||
        !validTimestamp(response.factor.createdAt)
      )
        throw new Error("invalid confirmation response");
      clearProvisioning();
      onEnabled();
    } catch {
      if (mounted.current) {
        clearProvisioning();
        setCode("");
        setOutcomeLocked(true);
        setError("Confirmation outcome is unknown. Refresh security status before retrying.");
        await refreshFactor();
      }
    } finally {
      inFlight.current = false;
      if (mounted.current) setBusy(false);
    }
  };

  return (
    <section aria-labelledby="totp-setup-heading" className="border-t border-border pt-4">
      <div className="flex items-start gap-3">
        <ShieldCheck aria-hidden="true" className="mt-0.5 size-4 text-forest" />
        <div className="min-w-0 flex-1">
          <h2 id="totp-setup-heading" className="text-[12px] font-semibold">
            Set up a security code
          </h2>
          <p className="mt-1 text-[10px] leading-5 text-muted-foreground">
            A fresh six-digit code is required before credential administration is available.
          </p>
          {!pending ? (
            <form className="mt-3 grid max-w-[420px] gap-3" onSubmit={enrol}>
              <label className="grid gap-1.5 text-[11px] font-medium">
                Current administrator password
                <input
                  autoComplete="current-password"
                  className={fieldClassName}
                  maxLength={1024}
                  onChange={(event) => setPassword(event.target.value)}
                  required
                  type="password"
                  value={password}
                />
              </label>
              <Button className="h-9 w-fit text-[11px]" disabled={busy} type="submit">
                {busy ? (
                  <LoaderCircle aria-hidden="true" className="size-3.5 animate-spin" />
                ) : null}
                {factorState === FactorState.PENDING_CONFIRMATION
                  ? "Restart TOTP setup"
                  : "Begin TOTP setup"}
              </Button>
            </form>
          ) : (
            <form className="mt-3 grid max-w-[520px] gap-3" onSubmit={confirm}>
              <div className="border-l-2 border-forest bg-muted/40 px-3 py-2">
                <p className="text-[10px] font-medium">Provisioning material — shown once</p>
                <code
                  className="mt-1 block break-all text-[11px]"
                  data-testid="totp-provisioning-material"
                >
                  {pending.material}
                </code>
              </div>
              <label className="grid max-w-[220px] gap-1.5 text-[11px] font-medium">
                Six-digit code
                <input
                  autoComplete="one-time-code"
                  className={fieldClassName}
                  inputMode="numeric"
                  maxLength={6}
                  onChange={(event) => setCode(event.target.value)}
                  pattern="[0-9]{6}"
                  required
                  value={code}
                />
              </label>
              <Button className="h-9 w-fit text-[11px]" disabled={busy} type="submit">
                Confirm security code
              </Button>
            </form>
          )}
          {error ? (
            <p className="mt-3 text-[11px] text-destructive" role="alert">
              {error}
            </p>
          ) : null}
          {outcomeLocked ? (
            <Button
              className="mt-3 h-9 w-fit text-[11px]"
              disabled={busy}
              onClick={refreshFactor}
              type="button"
              variant="outline"
            >
              Refresh security status
            </Button>
          ) : null}
        </div>
      </div>
    </section>
  );
}
