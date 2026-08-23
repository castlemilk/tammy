import { create } from "@bufbuild/protobuf";
import {
  ImportSbrProductIdRequestSchema,
  ImportSbrProductIdResponseSchema,
  ProductIdState,
  RemoveSbrProductIdRequestSchema,
  RemoveSbrProductIdResponseSchema,
} from "@tammy/connect-client/tammy/v1/sbr_pb.js";
import { LoaderCircle } from "lucide-react";
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
  input: ImportSbrProductIdRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 8_192,
  output: ImportSbrProductIdResponseSchema,
});
const removeCodec = createProtoMethodCodec({
  input: RemoveSbrProductIdRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 8_192,
  output: RemoveSbrProductIdResponseSchema,
});

export function ProductIdForm({
  api,
  onChanged,
  productIdentifier,
  serviceIdentifier,
  state,
  workspace,
}: {
  readonly api: Pick<TammyDesktopAPI, "assertTotp" | "importSbrProductId" | "removeSbrProductId">;
  readonly onChanged: () => void;
  readonly productIdentifier: string;
  readonly serviceIdentifier: string;
  readonly state: ProductIdState;
  readonly workspace: AuthenticatedWorkspace;
}) {
  const [action, setAction] = useState<"import" | "remove">();
  const [productId, setProductId] = useState("");
  const [totp, setTotp] = useState("");
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<string>();
  const inFlight = useRef(false);
  const mounted = useRef(true);
  const clear = () => {
    setProductId("");
    setTotp("");
  };
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const choose = (next: "import" | "remove") => {
    if (inFlight.current) return;
    clear();
    setNotice(undefined);
    setAction((value) => (value === next ? undefined : next));
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!action || inFlight.current || !/^\d{6}$/.test(totp)) return;
    inFlight.current = true;
    setBusy(true);
    setNotice(undefined);
    let mutationStarted = false;
    try {
      const purpose = action === "import" ? SBR_PURPOSE.importProduct : SBR_PURPOSE.removeProduct;
      const fresh = await assertFreshFactor(api, workspace, totp, purpose);
      if (!mounted.current) return;
      const context = commandContext(workspace, fresh);
      let result: ProductIdState;
      if (action === "import") {
        const command = importCodec.encodeRequest(
          create(ImportSbrProductIdRequestSchema, {
            commandContext: context,
            evteProductIdentifier: productIdentifier,
            evteServiceIdentifier: serviceIdentifier,
          }),
        );
        try {
          const pending = api.importSbrProductId({ command, productId });
          mutationStarted = true;
          clear();
          result = importCodec.decodeResponse(await pending).productIdState;
        } finally {
          command.fill(0);
        }
      } else {
        const command = removeCodec.encodeRequest(
          create(RemoveSbrProductIdRequestSchema, {
            commandContext: context,
            evteProductIdentifier: productIdentifier,
            evteServiceIdentifier: serviceIdentifier,
          }),
        );
        try {
          const pending = api.removeSbrProductId(command);
          mutationStarted = true;
          clear();
          result = removeCodec.decodeResponse(await pending).productIdState;
        } finally {
          command.fill(0);
        }
      }
      const expectedState = action === "import" ? ProductIdState.PRESENT : ProductIdState.MISSING;
      if (result !== expectedState) throw new Error("invalid response");
      if (!mounted.current) return;
      setAction(undefined);
      setNotice("Product ID status updated.");
      onChanged();
    } catch {
      if (mounted.current) {
        clear();
        setNotice(
          mutationStarted
            ? unknownOutcomeCopy
            : "Authorization failed. No Product ID operation was started.",
        );
      }
    } finally {
      inFlight.current = false;
      if (mounted.current) setBusy(false);
    }
  };

  return (
    <section aria-labelledby="product-id-heading" className="border-t border-border pt-4">
      <h2 id="product-id-heading" className="text-[12px] font-semibold">
        EVTE Product ID
      </h2>
      <p className="mt-1 text-[10px] leading-5 text-muted-foreground">
        Protected locally for the authenticated registration scope. The value is never displayed
        after import.
      </p>
      <dl className="mt-2 grid max-w-[620px] grid-cols-[150px_minmax(0,1fr)] gap-y-1 text-[10px]">
        <dt className="text-muted-foreground">Product scope</dt>
        <dd className="m-0 break-all">{productIdentifier}</dd>
        <dt className="text-muted-foreground">Service scope</dt>
        <dd className="m-0 break-all">{serviceIdentifier}</dd>
      </dl>
      <div className="mt-3 flex gap-2">
        {state !== ProductIdState.PRESENT ? (
          <Button
            className="h-9 text-[11px]"
            onClick={() => choose("import")}
            type="button"
            variant="outline"
          >
            Import Product ID
          </Button>
        ) : (
          <Button
            className="h-9 text-[11px]"
            onClick={() => choose("remove")}
            type="button"
            variant="outline"
          >
            Remove Product ID
          </Button>
        )}
      </div>
      {action ? (
        <form
          className="mt-4 grid max-w-[420px] gap-3 border-l-2 border-forest pl-4"
          onSubmit={submit}
        >
          {action === "import" ? (
            <label className="grid gap-1.5 text-[11px] font-medium">
              Product ID value
              <input
                autoComplete="off"
                className={fieldClassName}
                maxLength={1024}
                onChange={(event) => setProductId(event.target.value)}
                required
                type="password"
                value={productId}
              />
            </label>
          ) : (
            <p className="text-[11px] leading-5">
              Remove the protected Product ID for this exact EVTE product and service scope.
            </p>
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
            <Button className="h-9 text-[11px]" disabled={busy} type="submit">
              {busy ? <LoaderCircle aria-hidden="true" className="size-3.5 animate-spin" /> : null}
              Continue
            </Button>
            <Button
              className="h-9 text-[11px]"
              disabled={busy}
              onClick={() => choose(action)}
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
