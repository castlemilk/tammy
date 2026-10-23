import { create } from "@bufbuild/protobuf";
import {
  AccountStatus,
  AccountType,
  ListAccountsRequestSchema,
  ListAccountsResponseSchema,
  TaxTreatment,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  ListDocumentsRequestSchema,
  ListDocumentsResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import {
  GetInvoiceAccountingRequestSchema,
  GetInvoiceAccountingResponseSchema,
  GetInvoiceTemplateRequestSchema,
  GetInvoiceTemplateResponseSchema,
  InvoiceAccountingSchema,
  InvoicePaymentSchema,
  InvoiceSchema,
  InvoiceState,
  ListInvoicesRequestSchema,
  ListInvoicesResponseSchema,
  RecordInvoicePaymentRequestSchema,
  RecordInvoicePaymentResponseSchema,
} from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { GstBasis } from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import type { TammyConveyor } from "../../../conveyor/client";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { ConnectedInvoicesScreen } from "./connected-invoices-screen";

const workspace = {
  userId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073901",
  sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073902",
  workspaceId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073903",
  organisationId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073904",
};
const invoiceId = "01900f3c-7b2e-7cc4-98c4-dc0c0c073906";
const bankId = "01900f3c-7b2e-7cc4-98c4-dc0c0c073907";
const limit = { maximumRequestBytes: 16_384, maximumResponseBytes: 262_144 };
const list = createProtoMethodCodec({
  ...limit,
  input: ListInvoicesRequestSchema,
  output: ListInvoicesResponseSchema,
});
const getAccounting = createProtoMethodCodec({
  ...limit,
  input: GetInvoiceAccountingRequestSchema,
  output: GetInvoiceAccountingResponseSchema,
});
const paymentCodec = createProtoMethodCodec({
  ...limit,
  input: RecordInvoicePaymentRequestSchema,
  output: RecordInvoicePaymentResponseSchema,
});
const accounts = createProtoMethodCodec({
  ...limit,
  input: ListAccountsRequestSchema,
  output: ListAccountsResponseSchema,
});
const documents = createProtoMethodCodec({
  ...limit,
  input: ListDocumentsRequestSchema,
  output: ListDocumentsResponseSchema,
});
const template = createProtoMethodCodec({
  ...limit,
  input: GetInvoiceTemplateRequestSchema,
  output: GetInvoiceTemplateResponseSchema,
});

function fixture({ lostResponse = false, failedInvalidation = false } = {}) {
  const invoice = create(InvoiceSchema, {
    id: invoiceId,
    organisationId: workspace.organisationId,
    version: 2n,
    state: InvoiceState.ISSUED,
    sellerName: "Gamma Systems Pty Ltd",
    sellerAbn: "51824753556",
    subtotalMinor: 10000n,
    gstMinor: 1000n,
    totalMinor: 11000n,
    draft: {
      number: "RETRY-001",
      issueDate: "2026-09-30",
      dueDate: "2026-10-14",
      customerName: "Customer",
      customerAddress: "Melbourne",
      sellerAddress: "Melbourne",
      gstRegistered: true,
      lines: [
        {
          description: "Consulting",
          quantityMilli: 1000,
          unitPriceMinor: 10000n,
          taxTreatment: TaxTreatment.TAXABLE,
        },
      ],
    },
  });
  let accounting = create(InvoiceAccountingSchema, {
    invoiceId,
    organisationId: workspace.organisationId,
    version: 1n,
    posted: true,
    gstBasis: GstBasis.CASH,
    receivedMinor: 0n,
    outstandingMinor: 11000n,
    paymentCount: 0n,
  });
  const committed = new Map<string, Uint8Array>();
  const attempts: ReturnType<typeof paymentCodec.decodeRequest>[] = [];
  const invalidate = vi.fn(() => {
    if (failedInvalidation) throw new Error("Cache unavailable");
    return Promise.resolve();
  });
  const cached = { invalidate };
  const conveyor = {
    accounting: {
      listInvoices: async () =>
        list.encodeResponse(create(ListInvoicesResponseSchema, { invoices: [invoice] })),
      getInvoiceTemplate: async () =>
        template.encodeResponse(
          create(GetInvoiceTemplateResponseSchema, {
            template: {
              organisationId: workspace.organisationId,
              defaults: { paymentTermsDays: 14 },
            },
          }),
        ),
      getInvoiceAccounting: async () =>
        getAccounting.encodeResponse(create(GetInvoiceAccountingResponseSchema, { accounting })),
      recordInvoicePayment: async (frame: Uint8Array) => {
        const request = paymentCodec.decodeRequest(frame);
        attempts.push(request);
        const key = request.commandContext?.idempotencyKey ?? "";
        const replay = committed.get(key);
        if (replay) {
          expect(request).toEqual(attempts[0]);
          return replay;
        }
        if (request.expectedAccountingVersion !== accounting.version)
          throw new Error("Stale accounting version");
        const amount = request.amount?.minorUnits ?? 0n;
        if (amount > accounting.outstandingMinor) throw new Error("Overpayment");
        const payment = create(InvoicePaymentSchema, {
          id: `01900f3c-7b2e-7cc4-98c4-${(committed.size + 1).toString(16).padStart(12, "0")}`,
          invoiceId,
          organisationId: workspace.organisationId,
          paymentDate: request.paymentDate,
          amount: request.amount,
          allocatedGst: { currencyCode: "AUD", minorUnits: 500n },
          basSalesGross: request.amount,
          bankAccountId: bankId,
          journalId: bankId,
          reference: request.reference,
        });
        accounting = create(InvoiceAccountingSchema, {
          ...accounting,
          version: accounting.version + 1n,
          receivedMinor: accounting.receivedMinor + amount,
          outstandingMinor: accounting.outstandingMinor - amount,
          paymentCount: accounting.paymentCount + 1n,
          payments: [...accounting.payments, payment],
        });
        const result = paymentCodec.encodeResponse(
          create(RecordInvoicePaymentResponseSchema, { accounting, payment }),
        );
        committed.set(key, result);
        if (lostResponse && committed.size === 1) throw new Error("Success response lost");
        return result;
      },
      listAccounts: Object.assign(
        async () =>
          accounts.encodeResponse(
            create(ListAccountsResponseSchema, {
              accounts: [
                {
                  id: bankId,
                  organisationId: workspace.organisationId,
                  version: 1n,
                  code: "1000",
                  name: "Business bank",
                  type: AccountType.ASSET,
                  status: AccountStatus.ACTIVE,
                  reportClassification: "balance_sheet.cash",
                },
              ],
            }),
          ),
        cached,
      ),
      getBusinessIncomeSummary: cached,
      listJournals: cached,
      getTrialBalance: cached,
    },
    documents: {
      listDocuments: async () => documents.encodeResponse(create(ListDocumentsResponseSchema)),
    },
    reporting: { getAttentionSummary: cached, getCurrentBasDraft: cached },
  } as unknown as TammyConveyor;
  return { conveyor, attempts, committed, invalidate };
}

async function openPayment() {
  await userEvent.click(await screen.findByRole("button", { name: "Open RETRY-001" }));
  await userEvent.click(await screen.findByRole("button", { name: "Payments" }));
  await userEvent.click(await screen.findByRole("button", { name: "Record payment" }));
  fireEvent.change(screen.getByLabelText("Amount received (AUD)"), { target: { value: "55.00" } });
  fireEvent.change(screen.getByLabelText("Payment reference (optional)"), {
    target: { value: "Bank receipt 123" },
  });
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Save payment" })).toHaveProperty("disabled", false),
  );
}

it("replays a lost payment response after refresh without recording another receipt", async () => {
  const { conveyor, attempts, committed } = fixture({ lostResponse: true });
  render(<ConnectedInvoicesScreen conveyor={conveyor} workspace={workspace} />);
  await openPayment();
  await userEvent.click(screen.getByRole("button", { name: "Save payment" }));
  expect(await screen.findByRole("alert")).toHaveProperty(
    "textContent",
    expect.stringContaining("could not be recorded"),
  );
  expect(committed.size).toBe(1);
  await userEvent.click(screen.getByRole("button", { name: "Refresh accounting" }));
  await waitFor(() =>
    expect(screen.getByTestId("invoice-received").textContent).toContain("$55.00"),
  );
  await userEvent.click(screen.getByRole("button", { name: "Save payment" }));
  await screen.findByText("Payment recorded.");
  expect(committed.size).toBe(1);
  expect(attempts).toHaveLength(2);
  expect(attempts[1]).toEqual(attempts[0]);
  expect(screen.getByTestId("invoice-outstanding").textContent).toContain("$55.00");

  // A later, explicitly opened form is a new receipt even when its details match.
  await userEvent.click(screen.getByRole("button", { name: "Record payment" }));
  fireEvent.change(screen.getByLabelText("Payment reference (optional)"), {
    target: { value: "Bank receipt 123" },
  });
  await userEvent.click(screen.getByRole("button", { name: "Save payment" }));
  await screen.findByText("No outstanding balance.");
  expect(committed.size).toBe(2);
  expect(attempts[2]?.commandContext?.idempotencyKey).not.toBe(
    attempts[0]?.commandContext?.idempotencyKey,
  );
});

it("keeps a confirmed payment successful when refreshing derived caches fails", async () => {
  const { conveyor, committed, invalidate } = fixture({ failedInvalidation: true });
  render(<ConnectedInvoicesScreen conveyor={conveyor} workspace={workspace} />);
  await openPayment();
  await userEvent.click(screen.getByRole("button", { name: "Save payment" }));
  await screen.findByText("Payment recorded.");
  expect(screen.queryByRole("alert")).toBeNull();
  expect(committed.size).toBe(1);
  expect(invalidate).toHaveBeenCalledTimes(6);
  expect(screen.getByTestId("invoice-outstanding").textContent).toContain("$55.00");
});

it("replays a lost full-settlement response after the outstanding balance becomes zero", async () => {
  const { conveyor, attempts, committed } = fixture({ lostResponse: true });
  render(<ConnectedInvoicesScreen conveyor={conveyor} workspace={workspace} />);
  await openPayment();
  fireEvent.change(screen.getByLabelText("Amount received (AUD)"), { target: { value: "110.00" } });
  await userEvent.click(screen.getByRole("button", { name: "Save payment" }));
  await screen.findByRole("alert");
  await userEvent.click(screen.getByRole("button", { name: "Refresh accounting" }));
  await screen.findByText("No outstanding balance.");
  await userEvent.click(screen.getByRole("button", { name: "Save payment" }));
  await screen.findByText("Payment recorded.");
  expect(screen.queryByRole("alert")).toBeNull();
  expect(committed.size).toBe(1);
  expect(attempts).toHaveLength(2);
  expect(attempts[1]).toEqual(attempts[0]);
});
