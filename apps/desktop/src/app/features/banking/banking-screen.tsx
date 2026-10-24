import { create } from "@bufbuild/protobuf";
import {
  type BankStatementLine,
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
import { Check, FileInput, RefreshCw, Upload, X } from "lucide-react";
import { type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";

import type { TammyConveyorModule } from "../../../conveyor/client";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { HorizontalScrollArea } from "../../components/horizontal-scroll-area";
import {
  WorkFilters,
  WorkHeading,
  WorkNotice,
  WorkSearch,
  WorkState,
} from "../../components/review-workspace";
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
  TammyConveyorModule<"banking">,
  | "completeBankReconciliation"
  | "getBankingSummary"
  | "importBankStatement"
  | "listBankStatementLines"
  | "matchBankStatementLine"
>;

export function BankingScreen({
  api,
  onBankingChanged,
  workspace,
}: {
  readonly api: BankingAPI;
  readonly onBankingChanged?: () => Promise<void>;
  readonly workspace: AuthenticatedWorkspace | undefined;
}) {
  const [openingBalance, setOpeningBalance] = useState("");
  const [rows, setRows] = useState("");
  const [importOpen, setImportOpen] = useState(false);
  const [selected, setSelected] = useState<BankStatementLine>();
  const [reference, setReference] = useState("");
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<"all" | "unmatched" | "matched" | "reconciled">("all");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [notice, setNotice] = useState<string>();
  const [refreshError, setRefreshError] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const active = useRef(false);
  const attempt = useRef<{ signature: string; key: string } | undefined>(undefined);
  const importButton = useRef<HTMLButtonElement>(null);
  const openingInput = useRef<HTMLInputElement>(null);
  const referenceInput = useRef<HTMLInputElement>(null);
  const searchInput = useRef<HTMLInputElement>(null);
  const confirmation = useRef<HTMLElement>(null);
  const errorMessage = useRef<HTMLElement>(null);
  useEffect(() => {
    if (importOpen) openingInput.current?.focus();
  }, [importOpen]);
  useEffect(() => {
    if (selected) referenceInput.current?.focus();
  }, [selected]);
  useEffect(() => {
    if (notice) confirmation.current?.focus();
  }, [notice]);
  useEffect(() => {
    if (error) errorMessage.current?.focus();
  }, [error]);
  const matchButtons = useRef(new Map<string, HTMLButtonElement>());
  const attemptKey = (signature: string) => {
    if (attempt.current?.signature !== signature) attempt.current = { signature, key: uuidV7() };
    return attempt.current.key;
  };
  const organisationId = workspace?.organisationId ?? "";
  const userId = workspace?.userId ?? "";
  const sessionId = workspace?.sessionId ?? "";
  const linesInput = useMemo(
    () =>
      listCodec.encodeRequest(
        create(ListBankStatementLinesRequestSchema, {
          authentication: { actorUserId: userId, sessionId },
          organisationId,
          page: create(PageRequestSchema, { pageSize: 200 }),
        }),
      ),
    [organisationId, sessionId, userId],
  );
  const summaryInput = useMemo(
    () =>
      summaryCodec.encodeRequest(
        create(GetBankingSummaryRequestSchema, {
          authentication: { actorUserId: userId, sessionId },
          organisationId,
        }),
      ),
    [organisationId, sessionId, userId],
  );
  const linesQuery = api.listBankStatementLines.useQuery({
    enabled: organisationId.length > 0,
    input: linesInput,
    retry: false,
  });
  const summaryQuery = api.getBankingSummary.useQuery({
    enabled: organisationId.length > 0,
    input: summaryInput,
    retry: false,
  });
  const lines = useMemo(() => {
    if (!linesQuery.data) return [];
    try {
      return listCodec.decodeResponse(linesQuery.data).lines;
    } catch {
      return [];
    }
  }, [linesQuery.data]);
  const summary = useMemo(() => {
    if (!summaryQuery.data) return undefined;
    try {
      return summaryCodec.decodeResponse(summaryQuery.data);
    } catch {
      return undefined;
    }
  }, [summaryQuery.data]);
  const status: "loading" | "ready" | "unavailable" =
    !organisationId || linesQuery.isError || summaryQuery.isError
      ? "unavailable"
      : linesQuery.data && summaryQuery.data
        ? "ready"
        : "loading";
  const load = useCallback(async () => {
    const [lineResult, summaryResult] = await Promise.all([
      linesQuery.refetch(),
      summaryQuery.refetch(),
    ]);
    if (lineResult.isError || summaryResult.isError) throw new Error("refresh failed");
    await onBankingChanged?.();
  }, [linesQuery.refetch, onBankingChanged, summaryQuery.refetch]);
  const refresh = async () => {
    setRefreshing(true);
    setRefreshError(false);
    try {
      await load();
    } catch {
      setRefreshError(true);
    } finally {
      setRefreshing(false);
    }
  };
  const refreshAfterSave = async () => {
    setRefreshing(true);
    try {
      await load();
      setRefreshError(false);
    } catch {
      setRefreshError(true);
    } finally {
      setRefreshing(false);
    }
  };
  const begin = () => {
    if (active.current) return false;
    active.current = true;
    setBusy(true);
    setError(undefined);
    setNotice(undefined);
    return true;
  };
  const end = () => {
    active.current = false;
    setBusy(false);
  };
  const command = (key: string) =>
    create(CommandContextSchema, {
      authentication: {
        actorUserId: workspace?.userId ?? "",
        sessionId: workspace?.sessionId ?? "",
      },
      idempotencyKey: key,
    });

  const importStatement = async (event: FormEvent) => {
    event.preventDefault();
    if (!workspace?.organisationId || !begin()) return;
    let confirmed = false;
    try {
      const inputs = rows
        .split(/\r?\n/)
        .filter((row) => row.trim())
        .map((row) => {
          const parts = row.split(",");
          const [date = "", description = "", amount = ""] = parts;
          const dateParts = date.trim().match(/^(\d{4})-(\d{2})-(\d{2})$/);
          if (parts.length !== 3 || !dateParts || !description.trim())
            throw new Error("invalid row");
          return {
            transactionDate: create(CivilDateSchema, {
              year: Number(dateParts[1]),
              month: Number(dateParts[2]),
              day: Number(dateParts[3]),
            }),
            description: description.trim(),
            amount: create(MoneySchema, {
              currencyCode: "AUD",
              minorUnits: decimalToMinor(amount.trim()),
            }),
          };
        });
      if (inputs.length < 1 || inputs.length > 1000) throw new Error("invalid count");
      const request = create(ImportBankStatementRequestSchema, {
        commandContext: command(attemptKey(JSON.stringify(["import", openingBalance, rows]))),
        organisationId: workspace.organisationId,
        openingBalance: create(MoneySchema, {
          currencyCode: "AUD",
          minorUnits: decimalToMinor(openingBalance),
        }),
        lines: inputs,
      });
      const response = importCodec.decodeResponse(
        await api.importBankStatement(importCodec.encodeRequest(request)),
      );
      if (!response.statementImport) throw new Error("missing confirmation");
      confirmed = true;
      attempt.current = undefined;
      setImportOpen(false);
      setOpeningBalance("");
      setRows("");
      setQuery("");
      setFilter("all");
      setNotice(
        `Statement imported · ${response.statementImport.lineCount} lines retained. Review each match before reconciliation.`,
      );
      await refreshAfterSave();
    } catch {
      if (!confirmed)
        setError(
          "The statement was not confirmed. Use one row per line: YYYY-MM-DD,description,amount. Check the opening balance and try again; the same unchanged request can be retried safely.",
        );
    } finally {
      end();
    }
  };
  const match = async (event: FormEvent) => {
    event.preventDefault();
    if (!workspace || !selected) return;
    if (!reference.trim() || reference.trim().length > 128) {
      setError("Enter a reviewed source reference from 1 to 128 characters.");
      return;
    }
    if (!begin()) return;
    try {
      const response = matchCodec.decodeResponse(
        await api.matchBankStatementLine(
          matchCodec.encodeRequest(
            create(MatchBankStatementLineRequestSchema, {
              commandContext: command(
                attemptKey(
                  JSON.stringify([
                    "match",
                    selected.id,
                    selected.version.toString(),
                    reference.trim(),
                  ]),
                ),
              ),
              lineId: selected.id,
              expectedVersion: selected.version,
              matchReference: reference.trim(),
            }),
          ),
        ),
      );
      if (
        !response.line ||
        response.line.id !== selected.id ||
        response.line.status !== BankStatementLineStatus.MATCHED
      )
        throw new Error("missing match");
      setSelected(undefined);
      setReference("");
      attempt.current = undefined;
      setNotice(
        "Match saved. This reference records your review; no accounting entry or payment was created.",
      );
      await refreshAfterSave();
    } catch {
      setError(
        "The match could not be confirmed. Your reference is still here. Try again, or refresh if this line changed elsewhere.",
      );
    } finally {
      end();
    }
  };
  const reconcile = async () => {
    if (!workspace?.organisationId || !begin()) return;
    try {
      const response = reconcileCodec.decodeResponse(
        await api.completeBankReconciliation(
          reconcileCodec.encodeRequest(
            create(CompleteBankReconciliationRequestSchema, {
              commandContext: command(
                attemptKey(
                  JSON.stringify([
                    "reconcile",
                    summary?.importedLineCount,
                    summary?.unreconciledLineCount,
                  ]),
                ),
              ),
              organisationId: workspace.organisationId,
            }),
          ),
        ),
      );
      if (!response.closingBalance) throw new Error("missing reconciliation");
      attempt.current = undefined;
      setNotice(
        `Reconciliation complete · ${response.reconciledLineCount} lines reconciled. Closing balance ${formatMoney(response.closingBalance.minorUnits)}.`,
      );
      await refreshAfterSave();
    } catch {
      setError(
        "Reconciliation could not be confirmed. Match every imported line, refresh the view, and try again.",
      );
    } finally {
      end();
    }
  };
  const count = (value: BankStatementLineStatus) =>
    lines.filter((line) => line.status === value).length;
  const visible = lines.filter(
    (line) =>
      (filter === "all" ||
        (filter === "unmatched" && line.status === BankStatementLineStatus.UNMATCHED) ||
        (filter === "matched" && line.status === BankStatementLineStatus.MATCHED) ||
        (filter === "reconciled" && line.status === BankStatementLineStatus.RECONCILED)) &&
      [line.description, line.matchReference]
        .join(" ")
        .toLowerCase()
        .includes(query.trim().toLowerCase()),
  );
  const hasLines = (summary?.importedLineCount ?? 0) > 0;
  const canReconcile =
    status === "ready" &&
    !refreshError &&
    !refreshing &&
    !linesQuery.isFetching &&
    !summaryQuery.isFetching &&
    !busy &&
    hasLines &&
    (summary?.unmatchedLineCount ?? 0) === 0 &&
    (summary?.unreconciledLineCount ?? 0) > 0;
  const closeMatch = () => {
    const returnTarget = selected ? matchButtons.current.get(selected.id) : undefined;
    (returnTarget ?? searchInput.current)?.focus();
    setSelected(undefined);
    setReference("");
    setError(undefined);
  };
  return (
    <div className="mx-auto grid w-full max-w-[1120px] gap-4">
      <WorkHeading
        area="Banking"
        title="Banking"
        description="Review your statement and confirm each match before reconciliation."
        action={
          !importOpen ? (
            <Button
              ref={importButton}
              disabled={busy || refreshing || status !== "ready" || refreshError}
              onClick={() => {
                setImportOpen(true);
                setSelected(undefined);
                setError(undefined);
                setNotice(undefined);
              }}
              type="button"
            >
              <Upload aria-hidden="true" className="size-4" />
              Import statement
            </Button>
          ) : undefined
        }
      />
      {notice ? (
        <section
          ref={confirmation}
          aria-label="Banking confirmation"
          className="focus-ring rounded-xl"
          tabIndex={-1}
        >
          <WorkNotice success>{notice}</WorkNotice>
        </section>
      ) : null}
      {refreshError ? (
        <WorkNotice>
          The action was saved, but the latest statement view could not be loaded. Refresh before
          continuing.{" "}
          <Button disabled={busy || refreshing} onClick={refresh} type="button" variant="outline">
            Refresh statement lines
          </Button>
        </WorkNotice>
      ) : null}
      {error ? (
        <section
          ref={errorMessage}
          aria-label="Banking error"
          className="focus-ring rounded-xl"
          tabIndex={-1}
        >
          <WorkNotice>{error}</WorkNotice>
        </section>
      ) : null}
      {importOpen ? (
        <form
          aria-label="Statement import"
          className="work-surface overflow-hidden"
          onSubmit={importStatement}
          onKeyDown={(event) => {
            if (
              event.key !== "Escape" ||
              event.defaultPrevented ||
              event.nativeEvent.isComposing ||
              busy
            )
              return;
            event.preventDefault();
            setImportOpen(false);
            setError(undefined);
            requestAnimationFrame(() => importButton.current?.focus());
          }}
        >
          <div className="flex items-start justify-between gap-3 border-b border-border p-4">
            <div>
              <h2 className="font-serif text-[24px]">Import local CSV rows</h2>
              <p className="mt-1 text-[14px] leading-6 text-muted-foreground">
                Enter your opening balance and rows. Every match needs your review.
              </p>
            </div>
            <Button
              aria-label="Close statement import"
              disabled={busy}
              onClick={() => {
                setImportOpen(false);
                setError(undefined);
                requestAnimationFrame(() => importButton.current?.focus());
              }}
              type="button"
              variant="ghost"
            >
              <X aria-hidden="true" className="size-4" />
            </Button>
          </div>
          <div className="grid grid-cols-[minmax(0,.7fr)_minmax(0,1.3fr)] gap-4 p-4 max-[1050px]:grid-cols-1">
            <div>
              <label className="grid gap-2 text-[12px] font-semibold">
                Opening balance · AUD
                <input
                  ref={openingInput}
                  className="work-input tabular-nums"
                  disabled={busy}
                  onChange={(event) => setOpeningBalance(event.target.value)}
                  required
                  step="0.01"
                  type="number"
                  value={openingBalance}
                />
              </label>
              <p className="mt-3 text-[14px] leading-6 text-muted-foreground">
                Use the balance immediately before these transactions. Negative amounts represent
                money out.
              </p>
              <div className="mt-4 rounded-xl bg-background p-3 text-[13px] leading-6">
                <p className="font-semibold">Three columns, one transaction per line</p>
                <code className="break-words">YYYY-MM-DD,description,signed amount</code>
                <p className="mt-2 text-muted-foreground">
                  1–1,000 rows · AUD · no header row or commas inside descriptions
                </p>
              </div>
            </div>
            <label className="grid content-start gap-2 text-[12px] font-semibold">
              CSV rows
              <textarea
                className="focus-ring min-h-48 min-w-0 resize-y rounded-xl border border-border bg-surface p-3 font-mono text-[14px] font-normal leading-6"
                disabled={busy}
                onChange={(event) => setRows(event.target.value)}
                required
                value={rows}
                spellCheck={false}
              />
            </label>
          </div>
          <div className="flex flex-wrap gap-3 border-t border-border px-4 py-3">
            <Button disabled={busy} type="submit">
              <FileInput aria-hidden="true" className="size-4" />
              {busy ? "Importing…" : "Import statement"}
            </Button>
            <Button
              disabled={busy}
              onClick={() => {
                setImportOpen(false);
                setRows("");
                setOpeningBalance("");
                setError(undefined);
                requestAnimationFrame(() => importButton.current?.focus());
              }}
              type="button"
              variant="ghost"
            >
              Cancel import
            </Button>
          </div>
        </form>
      ) : null}
      <div
        className={`grid min-w-0 gap-4 ${selected ? "min-[1380px]:grid-cols-[minmax(0,1fr)_340px]" : ""}`}
      >
        <section
          aria-label="Statement workspace"
          className="work-surface min-w-0 self-start overflow-hidden"
        >
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-4 py-3">
            <h2 className="font-serif text-[24px] leading-8">Statement lines</h2>
            <p className="text-[12px] text-muted-foreground">Imported CSV · no live bank feed</p>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border p-3">
            <WorkFilters
              label="Statement status"
              value={filter}
              onChange={setFilter}
              options={[
                { value: "all", label: "All lines", count: lines.length },
                {
                  value: "unmatched",
                  label: "Unmatched",
                  count: count(BankStatementLineStatus.UNMATCHED),
                },
                {
                  value: "matched",
                  label: "Matched",
                  count: count(BankStatementLineStatus.MATCHED),
                },
                {
                  value: "reconciled",
                  label: "Reconciled",
                  count: count(BankStatementLineStatus.RECONCILED),
                },
              ]}
            />
            <div
              className={`flex w-full min-w-0 gap-2 ${selected ? "max-w-[260px]" : "max-w-[330px]"}`}
            >
              <WorkSearch
                inputRef={searchInput}
                label="Search statement lines"
                value={query}
                onChange={setQuery}
              />
              <Button
                aria-label="Refresh statement lines"
                disabled={busy || refreshing || status === "loading"}
                onClick={refresh}
                variant="ghost"
                type="button"
              >
                <RefreshCw aria-hidden="true" className="size-4" />
              </Button>
            </div>
          </div>
          {status !== "ready" ? (
            <WorkState
              title={
                status === "loading" ? "Loading statement lines" : "Banking data is unavailable"
              }
              loading={status === "loading"}
              description="Statement data is read from this local workspace."
              action={
                status === "unavailable" ? (
                  <Button onClick={refresh} type="button" variant="outline">
                    Try again
                  </Button>
                ) : undefined
              }
            />
          ) : visible.length === 0 ? (
            <WorkState
              title={
                lines.length ? "No matching statement lines" : "A calm start to reconciliation"
              }
              description={
                lines.length
                  ? "Try another description, reference or status."
                  : "Import your local statement rows to begin. Each line needs an explicit reviewed reference before reconciliation."
              }
            />
          ) : (
            <HorizontalScrollArea label="Scrollable statement lines">
              <table
                className="work-table banking-lines-table min-w-[680px]"
                aria-label="Statement lines"
              >
                <thead>
                  <tr>
                    <th>Date</th>
                    <th>Description & reference</th>
                    <th className="text-right">Amount · AUD</th>
                    <th>State</th>
                    <th className="text-right">Action</th>
                  </tr>
                </thead>
                <tbody>
                  {visible.map((line) => (
                    <tr
                      className={
                        selected?.id === line.id ? "bg-forest-soft" : "hover:bg-background"
                      }
                      key={line.id}
                    >
                      <td className="whitespace-nowrap tabular-nums">
                        {formatDate(line.transactionDate)}
                      </td>
                      <td className="min-w-[220px]">
                        <p className="font-semibold">{line.description}</p>
                        <p className="mt-0.5 text-[12px] leading-4 text-muted-foreground">
                          {line.matchReference || "No reviewed reference yet"}
                        </p>
                      </td>
                      <td className="whitespace-nowrap text-right font-semibold tabular-nums">
                        {formatMoney(line.amount?.minorUnits)}
                      </td>
                      <td>
                        <Status status={line.status} />
                      </td>
                      <td className="text-right">
                        {line.status === BankStatementLineStatus.UNMATCHED ? (
                          <Button
                            aria-expanded={selected?.id === line.id}
                            aria-controls={
                              selected?.id === line.id ? "bank-match-review" : undefined
                            }
                            ref={(node) => {
                              if (node) matchButtons.current.set(line.id, node);
                              else matchButtons.current.delete(line.id);
                            }}
                            disabled={
                              busy ||
                              refreshing ||
                              linesQuery.isFetching ||
                              summaryQuery.isFetching ||
                              status !== "ready" ||
                              refreshError
                            }
                            onClick={() => {
                              if (selected?.id === line.id) {
                                referenceInput.current?.focus();
                                return;
                              }
                              setSelected(line);
                              setReference("");
                              setError(undefined);
                              setNotice(undefined);
                              setImportOpen(false);
                            }}
                            type="button"
                            variant={selected?.id === line.id ? "default" : "ghost"}
                          >
                            Match
                          </Button>
                        ) : (
                          <Check
                            aria-label="Review complete"
                            className="ml-auto size-4 text-forest"
                          />
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </HorizontalScrollArea>
          )}
          <p className="border-t border-border px-4 py-2 text-[12px] text-muted-foreground">
            {lines.length} loaded (up to 200).
          </p>
          <section
            aria-label="Reconciliation summary"
            className="border-t border-border bg-forest-soft p-4"
          >
            <h3 className="text-[14px] font-semibold text-forest">Reconciliation summary</h3>
            <dl className="mt-3 flex min-w-0 flex-wrap gap-x-8 gap-y-3">
              <div className="min-w-0 max-w-full">
                <dt className="text-[12px] text-muted-foreground">Latest import balance · AUD</dt>
                <dd className="mt-1 text-[22px] font-semibold leading-7 tabular-nums max-[700px]:text-[18px]">
                  <HorizontalScrollArea
                    label="Latest imported statement balance"
                    className="whitespace-nowrap"
                  >
                    <span>
                      {summary?.latestClosingBalance
                        ? formatMoney(summary.latestClosingBalance.minorUnits)
                        : "—"}
                    </span>
                  </HorizontalScrollArea>
                </dd>
              </div>
              <div>
                <dt className="text-[12px] text-muted-foreground">Unmatched</dt>
                <dd className="mt-1 text-[22px] font-semibold leading-7 tabular-nums">
                  {status === "ready" ? (summary?.unmatchedLineCount ?? 0) : "—"}
                </dd>
              </div>
              <div>
                <dt className="text-[12px] text-muted-foreground">Unreconciled</dt>
                <dd className="mt-1 text-[22px] font-semibold leading-7 tabular-nums">
                  {status === "ready" ? (summary?.unreconciledLineCount ?? 0) : "—"}
                </dd>
              </div>
            </dl>
            <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
              <p className="max-w-[440px] text-[13px] leading-5 text-muted-foreground">
                {canReconcile
                  ? "Every imported line is matched. You can complete reconciliation."
                  : hasLines && summary?.unreconciledLineCount === 0
                    ? "All imported lines are reconciled."
                    : "Review every unmatched line before completing reconciliation."}
              </p>
              <Button disabled={!canReconcile} onClick={reconcile} type="button">
                {busy && !selected && !importOpen ? "Saving…" : "Complete reconciliation"}
              </Button>
            </div>
          </section>
        </section>
        {selected ? (
          <form
            noValidate
            id="bank-match-review"
            aria-label="Match review"
            className="work-surface min-w-0 self-start overflow-hidden"
            onSubmit={match}
            onKeyDown={(event) => {
              if (
                event.key !== "Escape" ||
                event.defaultPrevented ||
                event.nativeEvent.isComposing ||
                busy
              )
                return;
              event.preventDefault();
              closeMatch();
            }}
          >
            <div className="flex items-start justify-between gap-3 border-b border-border bg-background p-4">
              <div>
                <p className="text-[12px] font-semibold text-forest">Review a match</p>
                <h2 className="mt-1 break-words font-serif text-[22px] leading-7">
                  {selected.description}
                </h2>
                <p className="mt-2 text-[13px] leading-5 text-muted-foreground">
                  {formatDate(selected.transactionDate)} ·{" "}
                  <span className="font-semibold tabular-nums">
                    {formatMoney(selected.amount?.minorUnits)}
                  </span>
                </p>
              </div>
              <Button
                aria-label="Close match review"
                disabled={busy}
                onClick={closeMatch}
                type="button"
                variant="ghost"
              >
                <X aria-hidden="true" className="size-4" />
              </Button>
            </div>
            <div className="grid gap-3 p-4">
              <label className="grid content-start gap-2 text-[12px] font-semibold">
                Reviewed source reference
                <input
                  ref={referenceInput}
                  aria-invalid={error ? true : undefined}
                  aria-describedby="match-reference-help"
                  className="work-input"
                  disabled={busy}
                  maxLength={128}
                  onChange={(event) => setReference(event.target.value)}
                  required
                  value={reference}
                />
              </label>
              <p id="match-reference-help" className="text-[13px] leading-5 text-muted-foreground">
                Record the invoice, receipt or journal reference you reviewed. A match does not link
                a document, post a journal or send a payment.
              </p>
            </div>
            <div className="flex flex-wrap gap-2 border-t border-border px-4 py-3">
              <Button disabled={busy || !reference.trim()} type="submit">
                {busy ? "Saving…" : "Confirm match"}
              </Button>
              <Button disabled={busy} onClick={closeMatch} type="button" variant="ghost">
                Cancel match
              </Button>
            </div>
          </form>
        ) : null}
      </div>
    </div>
  );
}
function Status({ status }: { readonly status: BankStatementLineStatus }) {
  return (
    <span
      className={`inline-flex whitespace-nowrap rounded-full px-2.5 py-1 text-[12px] font-semibold ${status === BankStatementLineStatus.UNMATCHED ? "bg-warning-soft text-warning" : "bg-success-soft text-forest"}`}
    >
      {status === BankStatementLineStatus.RECONCILED
        ? "Reconciled"
        : status === BankStatementLineStatus.MATCHED
          ? "Matched"
          : "Unmatched"}
    </span>
  );
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
