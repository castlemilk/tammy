import { AlertTriangle, CheckCircle2, LoaderCircle, RefreshCw, WifiOff } from "lucide-react";
import type { SystemDiagnostics } from "../../../shared/desktop-api";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Card, CardContent, CardHeader } from "../../components/ui/card";
import { Separator } from "../../components/ui/separator";

export type DiagnosticsState =
  | { readonly status: "loading" }
  | { readonly diagnostics: SystemDiagnostics; readonly status: "ready" }
  | { readonly status: "unavailable" };

interface DiagnosticsCardProps {
  readonly onRetry: () => void;
  readonly state: DiagnosticsState;
}

const diagnosticLabels = {
  apiVersion: "API version",
  coreVersion: "Core version",
} as const;

export function DiagnosticsCard({ onRetry, state }: DiagnosticsCardProps) {
  return (
    <Card className="w-full max-w-[680px] overflow-hidden shadow-[0_1px_0_oklch(0.32_0.02_150/0.06)]">
      <CardHeader className="pb-4">
        <p className="text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
          System check
        </p>
        <h2 className="text-base font-bold tracking-tight">Local workspace foundation</h2>
      </CardHeader>
      <Separator />
      <CardContent className="pt-6">
        <div aria-live="polite" aria-atomic="true" role="status">
          {state.status === "loading" && <LoadingState />}
          {state.status === "ready" && <ReadyState diagnostics={state.diagnostics} />}
          {state.status === "unavailable" && <UnavailableState onRetry={onRetry} />}
        </div>
      </CardContent>
    </Card>
  );
}

function LoadingState() {
  return (
    <div className="flex min-h-32 items-start gap-3">
      <LoaderCircle
        aria-hidden="true"
        className="mt-0.5 size-5 shrink-0 animate-spin text-muted-foreground motion-reduce:animate-none"
      />
      <div className="grid gap-1">
        <p className="font-semibold">Starting local engine</p>
        <p className="max-w-md text-sm leading-5 text-muted-foreground">
          Preparing the private accounting service on this device.
        </p>
      </div>
    </div>
  );
}

function ReadyState({ diagnostics }: { readonly diagnostics: SystemDiagnostics }) {
  const versionRows = [
    { label: diagnosticLabels.apiVersion, value: diagnostics.apiVersion },
    { label: diagnosticLabels.coreVersion, value: diagnostics.coreVersion },
  ] as const;

  return (
    <div className="grid gap-5">
      <div className="flex items-start gap-3">
        <CheckCircle2 aria-hidden="true" className="mt-0.5 size-5 shrink-0 text-ready" />
        <div className="grid gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <p className="font-semibold">Local engine ready</p>
            <Badge variant="offline">
              <WifiOff aria-hidden="true" className="size-3" />
              Offline
            </Badge>
          </div>
          <p className="text-sm leading-5 text-muted-foreground">No cloud required</p>
        </div>
      </div>
      <dl className="grid grid-cols-2 border-y border-border bg-muted [&>div+div]:border-l [&>div+div]:border-border max-[520px]:grid-cols-1 max-[520px]:[&>div+div]:border-l-0 max-[520px]:[&>div+div]:border-t">
        {versionRows.map((row, index) => (
          <div
            className="grid min-w-0 gap-1 px-4 py-3 max-[520px]:border-l-0"
            key={row.label}
            data-column={index + 1}
          >
            <dt className="text-xs font-medium text-muted-foreground">{row.label}</dt>
            <dd className="min-w-0 [overflow-wrap:anywhere] font-mono text-sm font-semibold tabular-nums text-foreground">
              {row.value}
            </dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

function UnavailableState({ onRetry }: { readonly onRetry: () => void }) {
  return (
    <div className="flex min-h-32 items-start gap-3">
      <AlertTriangle aria-hidden="true" className="mt-0.5 size-5 shrink-0 text-danger" />
      <div className="grid gap-4">
        <div className="grid gap-1">
          <p className="font-semibold">Local engine unavailable</p>
          <p className="max-w-md text-sm leading-5 text-muted-foreground">
            The local accounting service did not start. Your data has not left this device.
          </p>
        </div>
        <Button onClick={onRetry} type="button" variant="outline">
          <RefreshCw aria-hidden="true" className="size-4" />
          Retry local engine
        </Button>
      </div>
    </div>
  );
}
