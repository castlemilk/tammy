import { create } from "@bufbuild/protobuf";
import {
  AccountDesignation,
  AccountSchema,
  AccountStatus,
  AccountType,
  ListAccountsRequestSchema,
  ListAccountsResponseSchema,
  NormalBalance,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import { PageInfoSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { ChartScreen } from "./chart-screen";

const codec = createProtoMethodCodec({
  input: ListAccountsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 131_072,
  output: ListAccountsResponseSchema,
});

it("renders the installed local chart through the named accounting method", async () => {
  const listAccounts = vi.fn(async (frame: Uint8Array) => {
    const request = codec.decodeRequest(frame);
    expect(request.organisationId).toBe("018f0000-0000-7000-8000-000000000004");
    expect(request.authentication).toMatchObject({
      actorUserId: "018f0000-0000-7000-8000-000000000002",
      sessionId: "018f0000-0000-7000-8000-000000000003",
    });
    expect(request.page?.pageSize).toBe(200);
    return codec.encodeResponse(
      create(ListAccountsResponseSchema, {
        accounts: [
          create(AccountSchema, {
            id: "018f0000-0000-7000-8000-000000000101",
            organisationId: request.organisationId,
            version: 1n,
            code: "1000",
            name: "Business bank",
            type: AccountType.ASSET,
            normalBalance: NormalBalance.DEBIT,
            status: AccountStatus.ACTIVE,
            designation: AccountDesignation.CONTROL,
            reportClassification: "balance_sheet.cash",
            cashFlowClassification: "operating",
          }),
          create(AccountSchema, {
            id: "018f0000-0000-7000-8000-000000000106",
            organisationId: request.organisationId,
            version: 1n,
            code: "2000",
            name: "Accounts payable",
            type: AccountType.LIABILITY,
            normalBalance: NormalBalance.CREDIT,
            status: AccountStatus.ACTIVE,
            designation: AccountDesignation.CONTROL,
            reportClassification: "balance_sheet.payables",
            cashFlowClassification: "operating",
          }),
        ],
        page: create(PageInfoSchema, { returnedCount: 2 }),
      }),
    );
  });
  const api = {
    createAccount: vi.fn(),
    listAccounts,
  } satisfies Pick<TammyDesktopAPI, "createAccount" | "listAccounts">;

  render(
    <ChartScreen
      api={api}
      workspace={{
        sessionId: "018f0000-0000-7000-8000-000000000003",
        userId: "018f0000-0000-7000-8000-000000000002",
        workspaceId: "018f0000-0000-7000-8000-000000000001",
        organisationId: "018f0000-0000-7000-8000-000000000004",
      }}
    />,
  );

  expect(await screen.findByText("Business bank")).toBeTruthy();
  expect(screen.getByText("Accounts payable")).toBeTruthy();
  expect(screen.getByText("2 accounts")).toBeTruthy();
  expect(screen.getByText("Asset")).toBeTruthy();
  expect(screen.getByText("Liability")).toBeTruthy();
  expect(listAccounts).toHaveBeenCalledOnce();
});
