import { useEffect, useState } from "react";
import type { InvoiceLogoPreview } from "../../../shared/invoice-template";
import { Button } from "../../components/ui/button";
import type { InvoicesApi } from "./invoices-screen";

export function InvoiceLogo({
  api,
  id,
  label,
}: {
  readonly api: Pick<InvoicesApi, "getLogo">;
  readonly id: string;
  readonly label: string;
}) {
  const [preview, setPreview] = useState<InvoiceLogoPreview>();
  const [error, setError] = useState(false);
  const [attempt, setAttempt] = useState(0);
  // biome-ignore lint/correctness/useExhaustiveDependencies: The retry counter deliberately reloads a failed preview.
  useEffect(() => {
    let active = true;
    setPreview(undefined);
    setError(false);
    void api.getLogo(id).then(
      (value) => {
        if (active) setPreview(value);
      },
      () => {
        if (active) setError(true);
      },
    );
    return () => {
      active = false;
    };
  }, [api, id, attempt]);

  if (error)
    return (
      <div className="flex flex-wrap items-center gap-3 text-[12px] text-muted-foreground">
        <span role="alert">Logo preview could not be loaded.</span>
        <Button
          className="h-11 text-[13px]"
          onClick={() => setAttempt((value) => value + 1)}
          type="button"
          variant="outline"
        >
          Retry {label === "Invoice logo" ? "invoice logo" : "logo preview"}
        </Button>
      </div>
    );
  return preview?.id === id ? (
    <img
      alt={label}
      className="max-h-14 max-w-[150px] object-contain object-left"
      src={preview.dataUrl}
    />
  ) : (
    <p className="text-[12px] text-muted-foreground">Loading logo…</p>
  );
}
