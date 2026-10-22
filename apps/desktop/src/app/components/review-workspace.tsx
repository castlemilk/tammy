import { useComposedRefs } from "@radix-ui/react-compose-refs";
import { AlertCircle, CheckCircle2, FileSearch, LoaderCircle, Search, X } from "lucide-react";
import { type ReactNode, type Ref, useRef } from "react";
import { Button } from "./ui/button";

export function WorkHeading({
  title,
  description,
  action,
}: {
  readonly title: string;
  readonly description: string;
  readonly action?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-5">
      <div className="max-w-[650px]">
        <p className="work-eyebrow mb-2">Your review workspace</p>
        <h1 className="page-title">{title}</h1>
        <p className="mt-2 text-[14px] leading-6 text-muted-foreground">{description}</p>
      </div>
      {action}
    </div>
  );
}

export function WorkNotice({
  children,
  success = false,
}: {
  readonly children: ReactNode;
  readonly success?: boolean;
}) {
  const Icon = success ? CheckCircle2 : AlertCircle;
  return (
    <div
      className={`flex items-start gap-3 rounded-xl border px-4 py-3 text-[14px] leading-6 ${success ? "border-success-border bg-success-soft text-forest" : "border-warning-border bg-warning-soft"}`}
      role={success ? "status" : "alert"}
    >
      <Icon aria-hidden="true" className="mt-1 size-4 shrink-0" />
      <div className="min-w-0">{children}</div>
    </div>
  );
}

export function WorkState({
  title,
  description,
  loading,
  action,
}: {
  readonly title: string;
  readonly description: string;
  readonly loading?: boolean;
  readonly action?: ReactNode;
}) {
  return (
    <div
      className="grid min-h-60 place-items-center px-5 py-10 text-center"
      role={loading ? "status" : undefined}
    >
      <div className="max-w-[360px]">
        <span className="mx-auto grid size-12 place-items-center rounded-2xl bg-forest-soft text-forest">
          {loading ? (
            <LoaderCircle aria-hidden="true" className="size-5 animate-spin" />
          ) : (
            <FileSearch aria-hidden="true" className="size-5" />
          )}
        </span>
        <h2 className="mt-4 font-serif text-[24px] leading-8">{title}</h2>
        <p className="mt-2 text-[14px] leading-6 text-muted-foreground">{description}</p>
        {action ? <div className="mt-4">{action}</div> : null}
      </div>
    </div>
  );
}

export function WorkSearch({
  inputRef,
  label,
  value,
  onChange,
  clearLabel = "Clear search",
}: {
  readonly label: string;
  readonly value: string;
  readonly onChange: (value: string) => void;
  readonly inputRef?: Ref<HTMLInputElement>;
  readonly clearLabel?: string;
}) {
  const localRef = useRef<HTMLInputElement>(null);
  const ref = useComposedRefs(inputRef, localRef);
  return (
    <div className="flex w-full min-w-0 flex-1 items-center gap-2">
      <label className="relative block w-full min-w-0 flex-1">
        <Search
          aria-hidden="true"
          className="pointer-events-none absolute left-3 top-3.5 size-4 text-muted-foreground"
        />
        <span className="sr-only">{label}</span>
        <input
          className="focus-ring h-11 w-full rounded-xl border border-border bg-surface pl-10 pr-3 text-[14px] [&::-webkit-search-cancel-button]:appearance-none"
          ref={ref}
          type="search"
          placeholder={label}
          value={value}
          onChange={(event) => onChange(event.target.value)}
        />
      </label>
      {value ? (
        <Button
          aria-label={clearLabel}
          className="size-11 shrink-0 p-0"
          onClick={() => {
            onChange("");
            localRef.current?.focus();
          }}
          type="button"
          variant="ghost"
        >
          <X aria-hidden="true" className="size-4" />
        </Button>
      ) : null}
    </div>
  );
}

export function WorkFilters<T extends string>({
  label,
  options,
  value,
  onChange,
}: {
  readonly label: string;
  readonly options: readonly {
    readonly value: T;
    readonly label: string;
    readonly count: number;
  }[];
  readonly value: T;
  readonly onChange: (value: T) => void;
}) {
  return (
    <fieldset aria-label={label} className="flex min-w-0 flex-wrap gap-1">
      {options.map((option) => (
        <button
          type="button"
          key={option.value}
          aria-pressed={value === option.value}
          onClick={() => onChange(option.value)}
          className={`focus-ring flex min-h-11 items-center gap-2 rounded-xl px-3 text-[13px] font-semibold transition-colors ${value === option.value ? "bg-forest-soft text-forest" : "text-muted-foreground hover:bg-muted"}`}
        >
          {option.label}
          <span className="rounded-md bg-surface px-1.5 py-0.5 text-[11px] tabular-nums">
            {option.count}
          </span>
        </button>
      ))}
    </fieldset>
  );
}
