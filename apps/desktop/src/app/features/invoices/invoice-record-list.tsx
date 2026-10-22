import { calculateInvoice, formatAud } from "@tammy/invoices/model";
import { ArrowRight, ChevronLeft, ChevronRight, RefreshCw, X } from "lucide-react";
import { useRef, useState } from "react";
import { WorkFilters, WorkNotice, WorkSearch, WorkState } from "../../components/review-workspace";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import type { InvoiceRecord } from "./invoices-screen";

const pageSize = 10;
type Filter = "all" | InvoiceRecord["state"];

export function InvoiceRecordList({
  records,
  currentId,
  loading,
  error,
  busy,
  onRefresh,
  onOpen,
}: {
  readonly records: readonly InvoiceRecord[];
  readonly currentId: string | undefined;
  readonly loading: boolean;
  readonly error?: string | undefined;
  readonly busy: boolean;
  readonly onRefresh: () => void;
  readonly onOpen: (record: InvoiceRecord) => void;
}) {
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState<Filter>("all");
  const [page, setPage] = useState(0);
  const searchRef = useRef<HTMLInputElement>(null);
  const query = search.trim().toLocaleLowerCase("en-AU");
  const filtered = records.filter(
    (record) =>
      (filter === "all" || record.state === filter) &&
      `${record.draft.number} ${record.draft.customerName}`
        .toLocaleLowerCase("en-AU")
        .includes(query),
  );
  const lastPage = Math.max(0, Math.ceil(filtered.length / pageSize) - 1);
  const visiblePage = Math.min(page, lastPage);
  const start = visiblePage * pageSize;
  const visible = filtered.slice(start, start + pageSize);

  function clearSearch() {
    setSearch("");
    setPage(0);
    searchRef.current?.focus();
  }

  return (
    <section
      className="work-surface min-w-0 overflow-hidden"
      aria-label="Saved invoices"
      aria-busy={loading}
    >
      <div className="flex flex-wrap items-start justify-between gap-4 px-5 py-5 sm:px-6">
        <div>
          <h2 className="text-[16px] font-semibold">Saved invoices</h2>
          <p className="mt-1 text-[13px] leading-5 text-muted-foreground">
            Revisit a draft or open an issued invoice to review receipts.
          </p>
        </div>
        <Button disabled={busy || loading} onClick={onRefresh} type="button" variant="ghost">
          <RefreshCw aria-hidden="true" className="size-4" />
          <span>Refresh saved invoices</span>
        </Button>
      </div>
      {error ? (
        <div className="px-5 pb-4 sm:px-6">
          <WorkNotice>{error}</WorkNotice>
        </div>
      ) : null}
      {records.length ? (
        <div className="flex flex-wrap items-center gap-3 border-y border-border px-5 py-3 sm:px-6">
          <WorkFilters<Filter>
            label="Invoice status"
            onChange={(value) => {
              setFilter(value);
              setPage(0);
            }}
            options={[
              { value: "all", label: "All invoices", count: records.length },
              {
                value: "draft",
                label: "Draft",
                count: records.filter((r) => r.state === "draft").length,
              },
              {
                value: "issued",
                label: "Issued",
                count: records.filter((r) => r.state === "issued").length,
              },
            ]}
            value={filter}
          />
          <div className="flex min-w-0 flex-1 items-center gap-2 sm:min-w-[260px]">
            <WorkSearch
              inputRef={searchRef}
              label="Search saved invoices"
              onChange={(value) => {
                setSearch(value);
                setPage(0);
              }}
              value={search}
            />
            {search ? (
              <Button
                aria-label="Clear invoice search"
                onClick={clearSearch}
                className="size-11 shrink-0 p-0"
                type="button"
                variant="ghost"
              >
                <X aria-hidden="true" className="size-4" />
              </Button>
            ) : null}
          </div>
        </div>
      ) : null}
      {loading ? (
        <WorkState
          loading
          title="Loading invoices…"
          description="Bringing your saved drafts and issued invoices into view."
        />
      ) : error && !records.length ? (
        <WorkState
          title="Saved invoices unavailable"
          description="Refresh the list to try again. Your current draft is still here."
        />
      ) : !records.length ? (
        <WorkState
          title="No saved invoices yet."
          description="Start with the draft below. Your saved invoice will appear here, ready to revisit."
        />
      ) : !filtered.length ? (
        <WorkState
          title="No matching invoices"
          description="Try another invoice number or customer, or choose a different status."
          action={
            <Button
              onClick={() => {
                setFilter("all");
                clearSearch();
              }}
              type="button"
              variant="outline"
            >
              Reset invoice filters
            </Button>
          }
        />
      ) : (
        <>
          <div className="relative min-w-0 overflow-x-auto">
            <table
              className="work-table min-w-[850px] text-[14px] [&_th]:text-[12px]"
              aria-label="Saved invoices"
            >
              <thead>
                <tr>
                  <th scope="col">Invoice / customer</th>
                  <th scope="col">Status</th>
                  <th scope="col">Issue date</th>
                  <th scope="col">Due</th>
                  <th className="text-right" scope="col">
                    Invoice total · AUD
                  </th>
                  <th scope="col">
                    <span className="sr-only">Open invoice</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {visible.map((record) => (
                  <tr
                    className={record.id === currentId ? "bg-forest-soft/60" : ""}
                    key={record.id}
                  >
                    <td>
                      <span className="block font-semibold">
                        {record.draft.number || "Unnumbered"}
                      </span>
                      <span className="mt-1 block text-[13px] text-muted-foreground">
                        {record.draft.customerName || "Customer not set"}
                      </span>
                    </td>
                    <td>
                      <Badge variant={record.state === "issued" ? "offline" : "outline"}>
                        {record.state === "issued" ? "Issued" : "Draft"}
                      </Badge>
                    </td>
                    <td className="whitespace-nowrap tabular-nums">
                      {displayDate(record.draft.issueDate)}
                    </td>
                    <td className="whitespace-nowrap tabular-nums">
                      {displayDate(record.draft.dueDate)}
                    </td>
                    <td className="whitespace-nowrap text-right font-semibold tabular-nums">
                      {invoiceTotal(record)}
                    </td>
                    <td>
                      <Button
                        aria-label={`Open ${record.draft.number || "unnumbered invoice"}`}
                        aria-pressed={record.id === currentId}
                        disabled={busy}
                        onClick={() => onOpen(record)}
                        type="button"
                        variant="ghost"
                      >
                        {record.id === currentId ? "Open now" : "Open"}
                        <ArrowRight aria-hidden="true" className="size-4" />
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border px-5 py-3 text-[13px] text-muted-foreground sm:px-6">
            <p aria-live="polite">
              {start + 1}–{Math.min(start + pageSize, filtered.length)} of {filtered.length}{" "}
              invoices
            </p>
            <div className="flex items-center gap-2">
              <Button
                aria-label="Previous invoice page"
                disabled={visiblePage === 0}
                onClick={() => setPage(visiblePage - 1)}
                className="size-11 shrink-0 p-0"
                type="button"
                variant="ghost"
              >
                <ChevronLeft aria-hidden="true" className="size-4" />
              </Button>
              <span className="tabular-nums">
                Page {visiblePage + 1} of {lastPage + 1}
              </span>
              <Button
                aria-label="Next invoice page"
                disabled={visiblePage === lastPage}
                onClick={() => setPage(visiblePage + 1)}
                className="size-11 shrink-0 p-0"
                type="button"
                variant="ghost"
              >
                <ChevronRight aria-hidden="true" className="size-4" />
              </Button>
            </div>
          </div>
        </>
      )}
    </section>
  );
}

function invoiceTotal(record: InvoiceRecord): string {
  try {
    return formatAud(calculateInvoice(record.draft).totalMinor);
  } catch {
    return "—";
  }
}

function displayDate(value: string): string {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return "—";
  const date = new Date(`${value}T00:00:00Z`);
  if (Number.isNaN(date.getTime())) return "—";
  return new Intl.DateTimeFormat("en-AU", {
    day: "numeric",
    month: "short",
    year: "numeric",
    timeZone: "UTC",
  }).format(date);
}
