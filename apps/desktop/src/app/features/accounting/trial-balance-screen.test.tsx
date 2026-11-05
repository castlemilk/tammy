import { create } from "@bufbuild/protobuf";
import {
  GetTrialBalanceRequestSchema,
  GetTrialBalanceResponseSchema,
  TrialBalanceLineSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import { MoneySchema, SourceRefSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";

import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { createConveyorFixture } from "../../test/conveyor-fixtures";
import { TrialBalanceScreen } from "./trial-balance-screen";

const codec = createProtoMethodCodec({
  input: GetTrialBalanceRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 524_288,
  output: GetTrialBalanceResponseSchema,
});

const workspace = {
  workspaceId: "018f0000-0000-7000-8000-000000000001",
  userId: "018f0000-0000-7000-8000-000000000002",
  sessionId: "018f0000-0000-7000-8000-000000000003",
  organisationId: "018f0000-0000-7000-8000-000000000004",
};
function report(debit = 31900n, credit = debit, lines = true) {
  return codec.encodeResponse(
    create(GetTrialBalanceResponseSchema, {
      financialRevision: 7n,
      totalDebits: { currencyCode: "AUD", minorUnits: debit },
      totalCredits: { currencyCode: "AUD", minorUnits: credit },
      lines: lines
        ? [
            {
              code: "6100",
              name: "Synthetic expense",
              debits: { currencyCode: "AUD", minorUnits: 123n },
              credits: { currencyCode: "AUD", minorUnits: 0n },
            },
          ]
        : [],
    }),
  );
}
function mount(
  fixture: ReturnType<typeof createConveyorFixture>,
  active: typeof workspace | undefined = workspace,
) {
  return render(
    <QueryClientProvider client={fixture.queryClient}>
      <TrialBalanceScreen
        api={fixture.conveyor.accounting}
        now={new Date(2026, 0, 1, 0, 30)}
        workspace={active}
      />
    </QueryClientProvider>,
  );
}

it("keeps local civil-date and authenticated query fields while displaying a read-only report date", async () => {
  const fixture = createConveyorFixture();
  fixture.procedures.accounting.getTrialBalance.mockImplementation(async (frame) => {
    expect(codec.decodeRequest(frame)).toMatchObject({
      authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
      organisationId: workspace.organisationId,
      asOfDate: { year: 2026, month: 1, day: 1 },
    });
    expect(codec.decodeRequest(frame).expectedFinancialRevision).toBeUndefined();
    return report();
  });
  const view = mount(fixture);
  await screen.findByText("Synthetic expense");
  expect(view.container.querySelector("time")?.getAttribute("datetime")).toBe("2026-01-01");
  expect(screen.getByText("Debits equal credits")).toBeTruthy();
  expect(screen.queryByRole("textbox")).toBeNull();
  expect(screen.queryByRole("button", { name: /export/i })).toBeNull();
  expect(fixture.procedures.accounting.getTrialBalance).toHaveBeenCalledOnce();
});

it("preserves authoritative totals and explicitly labels unequal columns without summing rows", async () => {
  const fixture = createConveyorFixture();
  fixture.procedures.accounting.getTrialBalance.mockResolvedValue(report(9000n, 8000n));
  const view = mount(fixture);
  await screen.findByText("Synthetic expense");
  expect(view.container.querySelector("tfoot")?.textContent).toContain("$90.00$80.00");
  expect(screen.getByText("$1.23")).toBeTruthy();
  expect(screen.getByText("Debits and credits differ")).toBeTruthy();
  expect(screen.queryByText("Debits equal credits")).toBeNull();
});

it("retains exact bigint cents through the signed 64-bit maximum and negative display", async () => {
  const fixture = createConveyorFixture();
  const value = create(GetTrialBalanceResponseSchema, {
    financialRevision: 8n,
    totalDebits: { currencyCode: "AUD", minorUnits: 9223372036854775807n },
    totalCredits: { currencyCode: "AUD", minorUnits: 9223372036854775807n },
    lines: [
      {
        code: "6100",
        name: "Synthetic signed display",
        debits: { currencyCode: "AUD", minorUnits: -1n },
        credits: { currencyCode: "AUD", minorUnits: 0n },
      },
    ],
  });
  fixture.procedures.accounting.getTrialBalance.mockResolvedValue(codec.encodeResponse(value));
  mount(fixture);
  expect(await screen.findByText("−$0.01")).toBeTruthy();
  expect(screen.getAllByText("$92,233,720,368,547,758.07")).toHaveLength(2);
});

it("describes loading without recommending a financial write", () => {
  const fixture = createConveyorFixture();
  fixture.procedures.accounting.getTrialBalance.mockImplementation(() => new Promise(() => {}));
  mount(fixture);
  expect(screen.getByRole("status").textContent).toContain("Reading the local account balances");
  expect(screen.queryByText(/Post a journal/)).toBeNull();
  expect(
    screen.getByRole("button", { name: "Refresh trial balance" }).hasAttribute("disabled"),
  ).toBe(true);
});

it("recovers a failed read and blocks repeated refresh while the read is pending", async () => {
  const fixture = createConveyorFixture();
  let resolveRead: ((value: Uint8Array) => void) | undefined;
  fixture.procedures.accounting.getTrialBalance
    .mockRejectedValueOnce(new Error("synthetic unavailable"))
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveRead = resolve;
        }),
    );
  mount(fixture);
  await screen.findByRole("heading", { name: "Trial balance unavailable" });
  const button = screen.getByRole("button", { name: "Refresh trial balance" });
  await userEvent.dblClick(button);
  expect(fixture.procedures.accounting.getTrialBalance).toHaveBeenCalledTimes(2);
  expect(button.hasAttribute("disabled")).toBe(true);
  await act(async () => {
    resolveRead?.(report());
  });
  await screen.findByText("Synthetic expense");
  await waitFor(() => expect(button.hasAttribute("disabled")).toBe(false));
  expect(fixture.procedures.accounting.postManualJournal).not.toHaveBeenCalled();
});

it("distinguishes a successful empty report from an unavailable or malformed response", async () => {
  const fixture = createConveyorFixture();
  fixture.procedures.accounting.getTrialBalance.mockResolvedValue(report(0n, 0n, false));
  mount(fixture);
  await screen.findByRole("heading", { name: "No account balances" });
  expect(screen.getAllByText("$0.00")).toHaveLength(2);
  expect(screen.queryByText("Trial balance unavailable")).toBeNull();
});

it("rejects a malformed response without rendering a zero-balanced report", async () => {
  const fixture = createConveyorFixture();
  fixture.procedures.accounting.getTrialBalance.mockResolvedValue(new Uint8Array([255]));
  mount(fixture);
  await screen.findByRole("heading", { name: "Trial balance unavailable" });
  expect(screen.queryByText("Debits equal credits")).toBeNull();
  expect(screen.queryByRole("table")).toBeNull();
});

it("renders the authoritative local trial balance without calculating totals", async () => {
  const fixture = createConveyorFixture();
  fixture.procedures.accounting.getTrialBalance.mockImplementation(async (frame: Uint8Array) => {
    const request = codec.decodeRequest(frame);
    expect(request.organisationId).toBe("018f0000-0000-7000-8000-000000000004");
    expect(request.asOfDate).toMatchObject({ year: 2026, month: 8, day: 10 });
    return codec.encodeResponse(
      create(GetTrialBalanceResponseSchema, {
        lines: [
          create(TrialBalanceLineSchema, {
            account: create(SourceRefSchema, {
              type: "account",
              id: "018f0000-0000-7000-8000-000000000201",
              revision: 1n,
              contentHash: new Uint8Array(32).fill(1),
            }),
            code: "6100",
            name: "Office expenses",
            debits: create(MoneySchema, { currencyCode: "AUD", minorUnits: 31900n }),
            credits: create(MoneySchema, { currencyCode: "AUD" }),
            ledgerNormalBalance: create(MoneySchema, { currencyCode: "AUD", minorUnits: 31900n }),
          }),
        ],
        totalDebits: create(MoneySchema, { currencyCode: "AUD", minorUnits: 31900n }),
        totalCredits: create(MoneySchema, { currencyCode: "AUD", minorUnits: 31900n }),
        financialRevision: 1n,
      }),
    );
  });
  render(
    <QueryClientProvider client={fixture.queryClient}>
      <TrialBalanceScreen
        api={fixture.conveyor.accounting}
        now={new Date("2026-08-10T12:00:00+10:00")}
        workspace={{
          workspaceId: "018f0000-0000-7000-8000-000000000001",
          userId: "018f0000-0000-7000-8000-000000000002",
          sessionId: "018f0000-0000-7000-8000-000000000003",
          organisationId: "018f0000-0000-7000-8000-000000000004",
        }}
      />
    </QueryClientProvider>,
  );
  expect(await screen.findByText("Office expenses")).toBeTruthy();
  expect(screen.getAllByText("$319.00").length).toBeGreaterThanOrEqual(2);
  expect(fixture.procedures.accounting.getTrialBalance).toHaveBeenCalledOnce();
});
