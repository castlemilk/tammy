import { type FormEvent, useEffect, useState } from "react";

import type { InvoiceAccountingRecord } from "../../../shared/invoice-accounting";
import { HorizontalScrollArea } from "../../components/horizontal-scroll-area";
import { Button } from "../../components/ui/button";
import { SurfaceHeading } from "../../components/workspace-primitives";
import { invoiceFieldClass } from "./invoice-line-table";

export interface InvoiceAccountingApi {
  beginPayment?(invoiceId: string): void;
  get(invoiceId: string): Promise<InvoiceAccountingRecord>;
  post(invoiceId: string, expectedVersion: number): Promise<InvoiceAccountingRecord>;
  recordPayment(
    invoiceId: string,
    expectedAccountingVersion: number,
    payment: {
      readonly date: string;
      readonly amountMinor: number;
      readonly bankAccountId: string;
      readonly reference: string;
    },
  ): Promise<InvoiceAccountingRecord>;
  listBankAccounts(): Promise<
    readonly { readonly id: string; readonly code: string; readonly name: string }[]
  >;
}

export function InvoiceAccountingPanel({
  api,
  invoiceId,
  invoiceVersion,
  issueDate,
  incomeMinor,
  gstMinor,
}: {
  readonly api: InvoiceAccountingApi;
  readonly invoiceId: string;
  readonly invoiceVersion: number;
  readonly issueDate: string;
  readonly incomeMinor: number;
  readonly gstMinor: number;
}) {
  const [accounting, setAccounting] = useState<InvoiceAccountingRecord>();
  const [loading, setLoading] = useState(true);
  const [attempt, setAttempt] = useState(0);
  const [reviewed, setReviewed] = useState(false);
  const [paymentOpen, setPaymentOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [notice, setNotice] = useState<string>();
  const [date, setDate] = useState(() => laterDate(localToday(), issueDate));
  const [amount, setAmount] = useState("");
  const [reference, setReference] = useState("");
  const [bankAccountId, setBankAccountId] = useState("");
  const [attemptedPayment, setAttemptedPayment] = useState<string>();
  const [banks, setBanks] = useState<Awaited<ReturnType<InvoiceAccountingApi["listBankAccounts"]>>>(
    [],
  );
  const [banksLoading, setBanksLoading] = useState(false);

  // biome-ignore lint/correctness/useExhaustiveDependencies: The retry counter reloads failed accounting reads.
  useEffect(() => {
    let active = true;
    setLoading(true);
    setError(undefined);
    void api
      .get(invoiceId)
      .then((value) => {
        if (active) setAccounting(value);
      })
      .catch(() => {
        if (active) setError("Invoice accounting could not be loaded. Refresh to try again.");
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [api, invoiceId, attempt]);

  async function post() {
    if (!reviewed || busy || accounting?.posted) return;
    setBusy(true);
    setError(undefined);
    setNotice(undefined);
    try {
      setAccounting(await api.post(invoiceId, invoiceVersion));
      setNotice("Invoice added to accounts.");
    } catch {
      setError(
        "The invoice could not be added. Check its accounting status and that the issue period is open, then refresh.",
      );
    } finally {
      setBusy(false);
    }
  }

  async function openPayment() {
    if (!accounting?.posted || busy) return;
    api.beginPayment?.(invoiceId);
    setAttemptedPayment(undefined);
    setPaymentOpen(true);
    setError(undefined);
    setNotice(undefined);
    setAmount((accounting.outstandingMinor / 100).toFixed(2));
    setDate(laterDate(localToday(), issueDate));
    setBanksLoading(true);
    try {
      const value = await api.listBankAccounts();
      setBanks(value);
      setBankAccountId(value[0]?.id ?? "");
    } catch {
      setError(
        "Bank accounts could not be loaded. Close and reopen payment recording to try again.",
      );
    } finally {
      setBanksLoading(false);
    }
  }

  async function savePayment(event: FormEvent) {
    event.preventDefault();
    if (!accounting?.posted || busy) return;
    setError(undefined);
    const amountMinor = parseAmount(amount);
    const payment = {
      date,
      amountMinor: amountMinor ?? 0,
      bankAccountId,
      reference: reference.trim(),
    };
    const identity = JSON.stringify(payment);
    if (!validDate(date) || date < issueDate) {
      setError("Choose a valid payment date on or after the invoice issue date.");
      return;
    }
    if (
      amountMinor === undefined ||
      amountMinor <= 0 ||
      (amountMinor > accounting.outstandingMinor && attemptedPayment !== identity)
    ) {
      setError(
        "Enter an amount above zero, with at most two decimal places, no greater than the outstanding balance.",
      );
      return;
    }
    if (!banks.some((bank) => bank.id === bankAccountId)) {
      setError("Choose an active business bank account.");
      return;
    }
    setBusy(true);
    setNotice(undefined);
    setAttemptedPayment(identity);
    try {
      setAccounting(await api.recordPayment(invoiceId, accounting.version, payment));
      setAttemptedPayment(undefined);
      setPaymentOpen(false);
      setReference("");
      setNotice("Payment recorded.");
    } catch {
      setError(
        "The payment could not be recorded. Refresh accounting before trying again; your payment details are still here.",
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <section
      className="work-surface grid min-w-0 gap-4 overflow-hidden pb-5"
      aria-label="Invoice accounting"
      aria-busy={loading || busy}
    >
      <SurfaceHeading
        title="Income &amp; payments"
        description="Posted revenue, tax and receipt history for this invoice."
        action={
          <Button
            className="h-11 text-[13px]"
            disabled={busy || loading}
            onClick={() => setAttempt((value) => value + 1)}
            type="button"
            variant="ghost"
          >
            Refresh accounting
          </Button>
        }
      />
      {loading ? (
        <p className="px-5 text-[13px] text-muted-foreground">Loading invoice accounting…</p>
      ) : null}
      {error ? (
        <p className="px-5 text-[13px] text-destructive" role="alert">
          {error}
        </p>
      ) : null}
      {notice ? (
        <p className="px-5 text-[13px] text-forest" role="status">
          {notice}
        </p>
      ) : null}
      {accounting?.posted ? (
        <>
          <p className="px-5 text-[12px] text-muted-foreground">
            {accounting.gstBasis === "cash"
              ? "Cash basis · GST follows recorded payments"
              : accounting.gstBasis === "non-cash"
                ? "Non-cash basis · GST recorded on issue"
                : "GST basis unavailable"}
          </p>
          <dl className="grid grid-cols-4 gap-4 px-5 max-[760px]:grid-cols-2 max-[460px]:grid-cols-1">
            <Metric label="Income (ex GST)" minor={incomeMinor} testId="posted-invoice-income" />
            <Metric label="Invoice GST" minor={gstMinor} />
            <Metric label="Received" minor={accounting.receivedMinor} testId="invoice-received" />
            <Metric
              label="Outstanding"
              minor={accounting.outstandingMinor}
              testId="invoice-outstanding"
            />
          </dl>
          <p className="px-5 text-[12px] leading-5 text-muted-foreground">
            Income is in the business ledger. Recording a receipt updates the bank and balance
            owing; it does not add income again.
          </p>
          {!paymentOpen && accounting.outstandingMinor > 0 ? (
            <Button
              className="mx-5 h-11 justify-self-start text-[13px]"
              disabled={busy}
              onClick={openPayment}
              type="button"
              variant="outline"
            >
              Record payment
            </Button>
          ) : null}
          {accounting.outstandingMinor === 0 ? (
            <p className="px-5 text-[13px] font-medium text-forest">No outstanding balance.</p>
          ) : null}
          {paymentOpen ? (
            <form
              className="grid gap-4 border-t border-border p-5"
              noValidate
              onSubmit={savePayment}
            >
              <div className="grid grid-cols-2 gap-3 max-[620px]:grid-cols-1">
                <label className="grid gap-1.5 text-[12px] font-semibold">
                  Payment date
                  <input
                    className={invoiceFieldClass}
                    disabled={busy}
                    min={issueDate}
                    onChange={(event) => setDate(event.target.value)}
                    type="date"
                    value={date}
                  />
                </label>
                <label className="grid gap-1.5 text-[12px] font-semibold">
                  Amount received (AUD)
                  <input
                    className={invoiceFieldClass}
                    disabled={busy}
                    inputMode="decimal"
                    onChange={(event) => setAmount(event.target.value)}
                    value={amount}
                  />
                </label>
                <label className="grid gap-1.5 text-[12px] font-semibold">
                  Bank account
                  <select
                    className={invoiceFieldClass}
                    disabled={busy || banksLoading}
                    onChange={(event) => setBankAccountId(event.target.value)}
                    value={bankAccountId}
                  >
                    <option value="">
                      {banksLoading ? "Loading bank accounts…" : "Choose a bank account"}
                    </option>
                    {banks.map((bank) => (
                      <option key={bank.id} value={bank.id}>
                        {bank.code} · {bank.name}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="grid gap-1.5 text-[12px] font-semibold">
                  Payment reference (optional)
                  <input
                    className={invoiceFieldClass}
                    disabled={busy}
                    maxLength={200}
                    onChange={(event) => setReference(event.target.value)}
                    value={reference}
                  />
                </label>
              </div>
              {!banksLoading && banks.length === 0 ? (
                <p className="text-[12px] leading-5 text-muted-foreground">
                  No active cash or bank account is available. Review the chart of accounts before
                  recording this receipt.
                </p>
              ) : null}
              <p className="text-[12px] text-muted-foreground">
                Record money actually received. This saves a local accounting receipt.
              </p>
              <div className="flex flex-wrap gap-2">
                <Button
                  className="h-11 text-[13px]"
                  disabled={busy || banksLoading || banks.length === 0}
                  type="submit"
                >
                  {busy ? "Saving…" : "Save payment"}
                </Button>
                <Button
                  className="h-11 text-[13px]"
                  disabled={busy}
                  onClick={() => setPaymentOpen(false)}
                  type="button"
                  variant="outline"
                >
                  Cancel payment
                </Button>
              </div>
            </form>
          ) : null}
          {accounting.payments.length ? (
            <HorizontalScrollArea
              label="Scrollable recorded invoice payments"
              className="mx-5 rounded-xl border border-border bg-background"
            >
              <table
                aria-label="Recorded invoice payments"
                className="w-full min-w-[520px] border-collapse text-left text-[14px]"
              >
                <thead>
                  <tr className="border-b border-border text-muted-foreground">
                    <th className="py-2 pr-3">Date</th>
                    <th className="py-2 pr-3">Reference</th>
                    <th className="py-2 text-right">Received</th>
                  </tr>
                </thead>
                <tbody>
                  {accounting.payments.map((payment) => (
                    <tr className="border-b border-border last:border-0" key={payment.id}>
                      <td className="py-2 pr-3">{payment.paymentDate}</td>
                      <td className="py-2 pr-3">{payment.reference || "—"}</td>
                      <td className="whitespace-nowrap py-2 text-right tabular-nums">
                        {money(payment.amountMinor)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {accounting.paymentCount > accounting.payments.length ? (
                <p className="mt-2 text-[12px] text-muted-foreground">
                  Showing {accounting.payments.length} of {accounting.paymentCount} receipts. Totals
                  include every receipt.
                </p>
              ) : null}
            </HorizontalScrollArea>
          ) : null}
        </>
      ) : accounting ? (
        <>
          <p className="px-5 text-[15px] font-semibold">Issued invoice needs accounting review</p>
          <p className="px-5 text-[13px] leading-6 text-muted-foreground">
            Older issued invoices have no automatic income entry. Check for an existing manual entry
            before adding this invoice to avoid recording the sale twice.
          </p>
          <label className="mx-5 flex items-start gap-2 text-[13px]">
            <input
              checked={reviewed}
              disabled={busy}
              onChange={(event) => setReviewed(event.target.checked)}
              type="checkbox"
            />
            I checked this invoice has not already been entered manually.
          </label>
          <Button
            className="mx-5 h-11 justify-self-start text-[13px]"
            disabled={!reviewed || busy}
            onClick={post}
            type="button"
          >
            {busy ? "Adding…" : "Add to accounts"}
          </Button>
        </>
      ) : null}
    </section>
  );
}

function Metric({
  label,
  minor,
  testId,
}: {
  readonly label: string;
  readonly minor: number;
  readonly testId?: string;
}) {
  return (
    <div data-testid={testId}>
      <dt className="text-[12px] text-muted-foreground">{label}</dt>
      <dd className="mt-2 break-words text-[22px] font-semibold tabular-nums">{money(minor)}</dd>
    </div>
  );
}
function money(minor: number) {
  return new Intl.NumberFormat("en-AU", { style: "currency", currency: "AUD" }).format(minor / 100);
}
function localToday() {
  const date = new Date();
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}
function laterDate(a: string, b: string) {
  return a > b ? a : b;
}
function parseAmount(value: string): number | undefined {
  const match = value.trim().match(/^(\d+)(?:\.(\d{1,2}))?$/);
  if (!match) return undefined;
  const amount = Number(match[1]) * 100 + Number((match[2] ?? "").padEnd(2, "0"));
  return Number.isSafeInteger(amount) ? amount : undefined;
}
function validDate(value: string) {
  const date = new Date(`${value}T00:00:00Z`);
  return (
    /^\d{4}-\d{2}-\d{2}$/.test(value) &&
    Number.isFinite(date.getTime()) &&
    date.toISOString().slice(0, 10) === value
  );
}
