import { create } from "@bufbuild/protobuf";
import {
  BankStatementLineStatus,
  CompleteBankReconciliationRequestSchema,
  CompleteBankReconciliationResponseSchema,
  GetBankingSummaryRequestSchema,
  GetBankingSummaryResponseSchema,
  ImportBankStatementRequestSchema,
  ImportBankStatementResponseSchema,
  ListBankStatementLinesRequestSchema,
  ListBankStatementLinesResponseSchema,
  MatchBankStatementLineRequestSchema,
  MatchBankStatementLineResponseSchema,
} from "@tammy/connect-client/tammy/v1/banking_pb.js";
import {
  CivilDateSchema,
  CommandContextSchema,
  MoneySchema,
  PageRequestSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import { Check, Landmark, Upload } from "lucide-react";
import { type FormEvent, useCallback, useEffect, useState } from "react";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { Button } from "../../components/ui/button";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";

const importCodec = createProtoMethodCodec({
  input: ImportBankStatementRequestSchema,
  maximumRequestBytes: 262_144,
  maximumResponseBytes: 32_768,
  output: ImportBankStatementResponseSchema,
});
const listCodec = createProtoMethodCodec({
  input: ListBankStatementLinesRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: ListBankStatementLinesResponseSchema,
});
const matchCodec = createProtoMethodCodec({
  input: MatchBankStatementLineRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 32_768,
  output: MatchBankStatementLineResponseSchema,
});
const reconcileCodec = createProtoMethodCodec({
  input: CompleteBankReconciliationRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
  output: CompleteBankReconciliationResponseSchema,
});
const summaryCodec = createProtoMethodCodec({
  input: GetBankingSummaryRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
  output: GetBankingSummaryResponseSchema,
});

type BankingAPI = Pick<
  TammyDesktopAPI,
  | "completeBankReconciliation"
  | "getBankingSummary"
  | "importBankStatement"
  | "listBankStatementLines"
  | "matchBankStatementLine"
>;

export function BankingScreen({
  api,
  workspace,
}: {
  readonly api: BankingAPI;
  readonly workspace: AuthenticatedWorkspace | undefined;
}) {
  const [openingBalance, setOpeningBalance] = useState("1000.00");
  const [rows, setRows] = useState("2024-05-12,Officeworks INV-029847,-319.00");
  const [lines, setLines] = useState<
    readonly import("@tammy/connect-client/tammy/v1/banking_pb.js").BankStatementLine[]
  >([]);
  const [summary, setSummary] =
    useState<import("@tammy/connect-client/tammy/v1/banking_pb.js").GetBankingSummaryResponse>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  const load = useCallback(async () => {
    if (!workspace?.organisationId) return;
    const authentication = { actorUserId: workspace.userId, sessionId: workspace.sessionId };
    const [lineFrame, summaryFrame] = await Promise.all([
      api.listBankStatementLines(
        listCodec.encodeRequest(
          create(ListBankStatementLinesRequestSchema, {
            authentication,
            organisationId: workspace.organisationId,
            page: create(PageRequestSchema, { pageSize: 200 }),
          }),
        ),
      ),
      api.getBankingSummary(
        summaryCodec.encodeRequest(
          create(GetBankingSummaryRequestSchema, {
            authentication,
            organisationId: workspace.organisationId,
          }),
        ),
      ),
    ]);
    setLines(listCodec.decodeResponse(lineFrame).lines);
    setSummary(summaryCodec.decodeResponse(summaryFrame));
  }, [api, workspace]);

  useEffect(() => {
    void load().catch(() => setError("Banking data is unavailable."));
  }, [load]);

  const importStatement = async (event: FormEvent) => {
    event.preventDefault();
    if (!workspace?.organisationId) return;
    setBusy(true);
    setError(undefined);
    try {
      const inputs = rows
        .split(/\r?\n/)
        .filter(Boolean)
        .map((row) => {
          const [date = "", description = "", amount = ""] = row.split(",");
          const parts = date.trim().match(/^(\d{4})-(\d{2})-(\d{2})$/);
          if (!parts || !description.trim()) throw new Error("invalid row");
          return {
            transactionDate: create(CivilDateSchema, {
              year: Number(parts[1]),
              month: Number(parts[2]),
              day: Number(parts[3]),
            }),
            description: description.trim(),
            amount: create(MoneySchema, {
              currencyCode: "AUD",
              minorUnits: decimalToMinor(amount.trim()),
            }),
          };
        });
      const request = create(ImportBankStatementRequestSchema, {
        commandContext: command(workspace),
        organisationId: workspace.organisationId,
        openingBalance: create(MoneySchema, {
          currencyCode: "AUD",
          minorUnits: decimalToMinor(openingBalance),
        }),
        lines: inputs,
      });
      await api.importBankStatement(importCodec.encodeRequest(request));
      await load();
    } catch {
      setError("Use one row per line: YYYY-MM-DD,description,amount.");
    } finally {
      setBusy(false);
    }
  };

  const match = async (
    line: import("@tammy/connect-client/tammy/v1/banking_pb.js").BankStatementLine,
  ) => {
    if (!workspace) return;
    setBusy(true);
    setError(undefined);
    try {
      await api.matchBankStatementLine(
        matchCodec.encodeRequest(
          create(MatchBankStatementLineRequestSchema, {
            commandContext: command(workspace),
            lineId: line.id,
            expectedVersion: line.version,
            matchReference: "Reviewed accounting source",
          }),
        ),
      );
      await load();
    } catch {
      setError("The statement line could not be matched.");
    } finally {
      setBusy(false);
    }
  };

  const reconcile = async () => {
    if (!workspace?.organisationId) return;
    setBusy(true);
    setError(undefined);
    try {
      await api.completeBankReconciliation(
        reconcileCodec.encodeRequest(
          create(CompleteBankReconciliationRequestSchema, {
            commandContext: command(workspace),
            organisationId: workspace.organisationId,
          }),
        ),
      );
      await load();
    } catch {
      setError("Match every imported line before completing reconciliation.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto grid w-full max-w-[1120px] gap-4">
      <div className="flex items-end justify-between gap-4">
        <div>
          <h1 className="text-[18px] font-semibold tracking-[-0.02em]">Banking</h1>
          <p className="mt-1 text-[11px] text-muted-foreground">
            Import local statement rows, confirm matches, then reconcile.
          </p>
        </div>
        <div className="rounded-[6px] border border-border bg-surface px-3 py-2 text-right">
          <p className="text-[8px] uppercase tracking-[0.05em] text-muted-foreground">
            Latest statement balance
          </p>
          <p className="mt-1 text-[15px] font-semibold">
            {formatMoney(summary?.latestClosingBalance?.minorUnits)}
          </p>
        </div>
      </div>
      {error ? (
        <p
          className="rounded-[6px] border border-warning-border bg-warning-soft px-3 py-2 text-[10px]"
          role="alert"
        >
          {error}
        </p>
      ) : null}
      <div className="grid grid-cols-[minmax(280px,.75fr)_minmax(460px,1.5fr)] gap-4 max-[900px]:grid-cols-1">
        <form
          className="grid content-start gap-3 rounded-[6px] border border-border bg-surface p-4"
          onSubmit={importStatement}
        >
          <div>
            <p className="text-[9px] font-semibold uppercase tracking-[0.05em] text-muted-foreground">
              Statement import
            </p>
            <h2 className="mt-1 text-[12px] font-semibold">Business transaction account</h2>
          </div>
          <label className="grid gap-1 text-[9px] font-semibold">
            Opening balance
            <input
              className="focus-ring h-8 rounded-[5px] border border-border px-2 text-[10px]"
              onChange={(event) => setOpeningBalance(event.target.value)}
              step="0.01"
              type="number"
              value={openingBalance}
            />
          </label>
          <label className="grid gap-1 text-[9px] font-semibold">
            CSV rows
            <textarea
              className="focus-ring min-h-32 rounded-[5px] border border-border p-2 font-mono text-[9px] leading-5"
              onChange={(event) => setRows(event.target.value)}
              value={rows}
            />
          </label>
          <p className="text-[9px] text-muted-foreground">
            Format: date, description, signed amount. Nothing is matched automatically.
          </p>
          <Button disabled={busy} type="submit">
            <Upload className="size-3" />
            Import statement
          </Button>
        </form>
        <section className="overflow-hidden rounded-[6px] border border-border bg-surface">
          <div className="flex items-center justify-between border-b border-border px-3 py-2">
            <div>
              <p className="text-[9px] font-semibold uppercase tracking-[0.05em] text-muted-foreground">
                Statement lines
              </p>
              <p className="mt-1 text-[9px] text-muted-foreground">
                {summary?.unmatchedLineCount ?? 0} unmatched · {summary?.unreconciledLineCount ?? 0}{" "}
                unreconciled
              </p>
            </div>
            <Button
              disabled={
                busy ||
                lines.length === 0 ||
                (summary?.unmatchedLineCount ?? 0) > 0 ||
                (summary?.unreconciledLineCount ?? 0) === 0
              }
              onClick={reconcile}
              type="button"
            >
              Complete reconciliation
            </Button>
          </div>
          {lines.length === 0 ? (
            <div className="grid min-h-64 place-items-center text-center">
              <div>
                <Landmark className="mx-auto size-5 text-muted-foreground" />
                <p className="mt-2 text-[11px] font-semibold">No statement lines yet</p>
                <p className="mt-1 text-[9px] text-muted-foreground">
                  Import the sample row to begin.
                </p>
              </div>
            </div>
          ) : (
            <table className="w-full border-collapse text-left text-[9px]">
              <thead>
                <tr className="border-b border-border text-muted-foreground">
                  <th className="px-3 py-2">Date</th>
                  <th className="px-3 py-2">Description</th>
                  <th className="px-3 py-2 text-right">Amount</th>
                  <th className="px-3 py-2">State</th>
                  <th className="px-3 py-2 text-right">Action</th>
                </tr>
              </thead>
              <tbody>
                {lines.map((line) => (
                  <tr className="border-b border-border last:border-0" key={line.id}>
                    <td className="px-3 py-2">{formatDate(line.transactionDate)}</td>
                    <td className="px-3 py-2 font-medium">{line.description}</td>
                    <td className="px-3 py-2 text-right font-semibold">
                      {formatMoney(line.amount?.minorUnits)}
                    </td>
                    <td className="px-3 py-2">
                      <Status status={line.status} />
                    </td>
                    <td className="px-3 py-2 text-right">
                      {line.status === BankStatementLineStatus.UNMATCHED ? (
                        <Button
                          className="h-7 px-2 text-[9px]"
                          disabled={busy}
                          onClick={() => match(line)}
                          type="button"
                          variant="outline"
                        >
                          Match
                        </Button>
                      ) : (
                        <Check className="ml-auto size-3 text-forest" />
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>
      </div>
    </div>
  );
}

function Status({ status }: { readonly status: BankStatementLineStatus }) {
  const label =
    status === BankStatementLineStatus.RECONCILED
      ? "Reconciled"
      : status === BankStatementLineStatus.MATCHED
        ? "Matched"
        : "Unmatched";
  return (
    <span
      className={`rounded-full px-2 py-1 text-[8px] font-semibold ${status === BankStatementLineStatus.UNMATCHED ? "bg-warning-soft text-warning" : "bg-success-soft text-forest"}`}
    >
      {label}
    </span>
  );
}
function command(workspace: AuthenticatedWorkspace) {
  return create(CommandContextSchema, {
    authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
    idempotencyKey: uuidV7(),
  });
}
function decimalToMinor(value: string): bigint {
  const match = value.trim().match(/^(-?)(\d+)(?:\.(\d{0,2}))?$/);
  if (!match) throw new Error("invalid amount");
  const minor = BigInt(match[2] ?? "0") * 100n + BigInt((match[3] ?? "").padEnd(2, "0"));
  return match[1] === "-" ? -minor : minor;
}
function formatMoney(value: bigint | undefined): string {
  return new Intl.NumberFormat("en-AU", { style: "currency", currency: "AUD" }).format(
    Number(value ?? 0n) / 100,
  );
}
function formatDate(
  value: { readonly year: number; readonly month: number; readonly day: number } | undefined,
): string {
  return value
    ? `${value.day.toString().padStart(2, "0")}/${value.month.toString().padStart(2, "0")}/${value.year}`
    : "—";
}
function uuidV7(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  let milliseconds = Date.now();
  for (let index = 5; index >= 0; index -= 1) {
    bytes[index] = milliseconds & 0xff;
    milliseconds = Math.floor(milliseconds / 256);
  }
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x70;
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80;
  const hex = [...bytes].map((value) => value.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
