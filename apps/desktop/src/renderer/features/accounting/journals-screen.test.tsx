import { create } from "@bufbuild/protobuf";
import {
  AccountDesignation,
  AccountSchema,
  AccountStatus,
  AccountType,
  JournalSchema,
  JournalSource,
  JournalState,
  ListAccountsRequestSchema,
  ListAccountsResponseSchema,
  ListJournalsRequestSchema,
  ListJournalsResponseSchema,
  NormalBalance,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import { CivilDateSchema, MoneySchema, PageInfoSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { JournalsScreen } from "./journals-screen";

const accountsCodec = createProtoMethodCodec({ input: ListAccountsRequestSchema, maximumRequestBytes: 16_384, maximumResponseBytes: 131_072, output: ListAccountsResponseSchema });
const journalsCodec = createProtoMethodCodec({ input: ListJournalsRequestSchema, maximumRequestBytes: 16_384, maximumResponseBytes: 262_144, output: ListJournalsResponseSchema });

it("renders retained journals and exposes the balanced manual journal action", async () => {
  const organisationId = "018f0000-0000-7000-8000-000000000004";
  const accounts = [
    create(AccountSchema, { id: "018f0000-0000-7000-8000-000000000201", organisationId, version: 1n, code: "6100", name: "Office expenses", type: AccountType.EXPENSE, normalBalance: NormalBalance.DEBIT, status: AccountStatus.ACTIVE, designation: AccountDesignation.ORDINARY, reportClassification: "profit_loss.expense", cashFlowClassification: "noncash" }),
    create(AccountSchema, { id: "018f0000-0000-7000-8000-000000000202", organisationId, version: 1n, code: "3100", name: "Owner contributions", type: AccountType.EQUITY, normalBalance: NormalBalance.CREDIT, status: AccountStatus.ACTIVE, designation: AccountDesignation.ORDINARY, reportClassification: "balance_sheet.equity", cashFlowClassification: "noncash" }),
  ];
  const journal = create(JournalSchema, { id: "018f0000-0000-7000-8000-000000000210", organisationId, version: 1n, state: JournalState.POSTED, source: JournalSource.MANUAL, postingDate: create(CivilDateSchema, { year: 2026, month: 8, day: 10 }), memo: "Office supplies", totalDebits: create(MoneySchema, { currencyCode: "AUD", minorUnits: 31900n }), totalCredits: create(MoneySchema, { currencyCode: "AUD", minorUnits: 31900n }), financialRevision: 1n });
  const api = {
    listAccounts: vi.fn(async (frame: Uint8Array) => {
      expect(accountsCodec.decodeRequest(frame).organisationId).toBe(organisationId);
      return accountsCodec.encodeResponse(create(ListAccountsResponseSchema, { accounts, page: create(PageInfoSchema, { returnedCount: 2 }) }));
    }),
    listJournals: vi.fn(async (frame: Uint8Array) => {
      expect(journalsCodec.decodeRequest(frame).organisationId).toBe(organisationId);
      return journalsCodec.encodeResponse(create(ListJournalsResponseSchema, { journals: [journal], page: create(PageInfoSchema, { returnedCount: 1 }) }));
    }),
    getJournal: vi.fn(),
    postManualJournal: vi.fn(),
  } satisfies Pick<TammyDesktopAPI, "getJournal" | "listAccounts" | "listJournals" | "postManualJournal">;
  const user = userEvent.setup();

  render(<JournalsScreen api={api} onNavigate={vi.fn()} path="/accounting/journals" workspace={{ workspaceId: "018f0000-0000-7000-8000-000000000001", userId: "018f0000-0000-7000-8000-000000000002", sessionId: "018f0000-0000-7000-8000-000000000003", organisationId }} />);

  expect(await screen.findByText("Office supplies")).toBeTruthy();
  expect(screen.getByText("$319.00")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "New journal" }));
  expect(screen.getByLabelText("Debit account")).toBeTruthy();
  expect(screen.getByLabelText("Credit account")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Post journal" })).toBeTruthy();
});
