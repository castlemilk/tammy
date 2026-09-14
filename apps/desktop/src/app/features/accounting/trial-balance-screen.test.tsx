import { create } from "@bufbuild/protobuf";
import {
  GetTrialBalanceRequestSchema,
  GetTrialBalanceResponseSchema,
  TrialBalanceLineSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import { MoneySchema, SourceRefSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
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
