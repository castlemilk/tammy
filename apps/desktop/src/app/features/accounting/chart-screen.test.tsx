import { create } from "@bufbuild/protobuf";
import {
  AccountDesignation,
  AccountSchema,
  AccountStatus,
  AccountType,
  CreateAccountRequestSchema,
  CreateAccountResponseSchema,
  ListAccountsRequestSchema,
  ListAccountsResponseSchema,
  NormalBalance,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import { PageInfoSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { createConveyorFixture } from "../../test/conveyor-fixtures";
import { ChartScreen } from "./chart-screen";

const codec = createProtoMethodCodec({
  input: ListAccountsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 131_072,
  output: ListAccountsResponseSchema,
});

const workspace = {
  sessionId: "018f0000-0000-7000-8000-000000000003",
  userId: "018f0000-0000-7000-8000-000000000002",
  workspaceId: "018f0000-0000-7000-8000-000000000001",
  organisationId: "018f0000-0000-7000-8000-000000000004",
};
const createCodec = createProtoMethodCodec({
  input: CreateAccountRequestSchema,
  maximumRequestBytes: 32768,
  maximumResponseBytes: 32768,
  output: CreateAccountResponseSchema,
});
function account(
  code: string,
  type = AccountType.EXPENSE,
  designation = AccountDesignation.ORDINARY,
  status = AccountStatus.ACTIVE,
) {
  return create(AccountSchema, {
    id: `018f0000-0000-7000-8000-${code.padStart(12, "0")}`,
    organisationId: workspace.organisationId,
    code,
    name: `Synthetic account ${code}`,
    type,
    designation,
    status,
    normalBalance: [
      AccountType.LIABILITY,
      AccountType.EQUITY,
      AccountType.REVENUE,
      AccountType.OTHER_REVENUE,
    ].includes(type)
      ? NormalBalance.CREDIT
      : NormalBalance.DEBIT,
    version: 1n,
    reportClassification: "custom.account",
    cashFlowClassification: "noncash",
  });
}
function readyChart(accounts = [account("6100")]) {
  const fixture = createConveyorFixture();
  fixture.procedures.accounting.listAccounts.mockResolvedValue(
    codec.encodeResponse(create(ListAccountsResponseSchema, { accounts })),
  );
  render(
    <QueryClientProvider client={fixture.queryClient}>
      <ChartScreen api={fixture.conveyor.accounting} workspace={workspace} />
    </QueryClientProvider>,
  );
  return fixture;
}
async function entry() {
  const add = await screen.findByRole("button", { name: "Add account" });
  await waitFor(() => expect(add.hasAttribute("disabled")).toBe(false));
  await userEvent.click(add);
  await userEvent.type(screen.getByLabelText("Code"), "6700");
  await userEvent.type(screen.getByLabelText("Account name"), "Synthetic insurance");
  const form = screen.getByLabelText("Account name").closest("form");
  if (!form) throw new Error("Expected ordinary account form");
  return form;
}

it("groups every loaded type without dropping unknown classifications and separates protection from lifecycle", async () => {
  readyChart([
    account("6200"),
    account("1100", AccountType.ASSET, AccountDesignation.CONTROL),
    account("1000", AccountType.ASSET, AccountDesignation.CONTROL),
    account("3000", AccountType.EQUITY, AccountDesignation.SYSTEM),
    account("2000", AccountType.LIABILITY),
    account("4000", AccountType.REVENUE),
    account("4100", AccountType.OTHER_REVENUE),
    account("6100", AccountType.EXPENSE, AccountDesignation.ORDINARY, AccountStatus.ARCHIVED),
    account("7100", AccountType.OTHER_EXPENSE),
    account("8000", AccountType.CONTRA),
    account(
      "9000",
      AccountType.UNSPECIFIED,
      AccountDesignation.UNSPECIFIED,
      AccountStatus.UNSPECIFIED,
    ),
  ]);
  expect(await screen.findByText("11 loaded accounts")).toBeTruthy();
  const groups = screen.getAllByRole("button", { expanded: true });
  expect(groups.map((node) => node.textContent)).toEqual([
    "Assets (2)",
    "Liabilities (1)",
    "Equity (1)",
    "Income (1)",
    "Other income (1)",
    "Expenses (2)",
    "Other expenses (1)",
    "Contra (1)",
    "Unclassified (1)",
  ]);
  const names = screen.getAllByRole("cell").map((node) => node.textContent);
  expect(names.indexOf("1000")).toBeLessThan(names.indexOf("1100"));
  expect(names.indexOf("6100")).toBeLessThan(names.indexOf("6200"));
  const control = screen.getByText("Synthetic account 1000").closest("tr");
  const system = screen.getByText("Synthetic account 3000").closest("tr");
  const archived = screen.getByText("Synthetic account 6100").closest("tr");
  if (!control || !system || !archived) throw new Error("Expected account rows");
  expect(control.textContent).toContain("Protected · Control");
  expect(system.textContent).toContain("Protected · System");
  expect(archived.textContent).toContain("OrdinaryArchived");
  expect(within(control).queryByRole("button")).toBeNull();
  expect(screen.getByText("Unclassified (1)")).toBeTruthy();
  expect(screen.getAllByText("Unknown")).toHaveLength(2);
});

it("supports group keyboard disclosure and reveals matching loaded accounts when search changes", async () => {
  const fixture = readyChart([account("1000", AccountType.ASSET), account("6200")]);
  const assets = await screen.findByRole("button", { name: "Assets (1)" });
  assets.focus();
  await userEvent.keyboard("{Enter}");
  expect(assets.getAttribute("aria-expanded")).toBe("false");
  expect(screen.queryByText("Synthetic account 1000")).toBeNull();
  const search = screen.getByRole("searchbox");
  await userEvent.type(search, "1000");
  expect(screen.getByText("Synthetic account 1000")).toBeTruthy();
  expect(screen.queryByText("Synthetic account 6200")).toBeNull();
  await userEvent.clear(search);
  await userEvent.type(search, "NO MATCH");
  expect(screen.getByRole("heading", { name: "No matching accounts" })).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "Clear search" }));
  expect(document.activeElement).toBe(search);
  expect(screen.getByText("Synthetic account 6200")).toBeTruthy();
  expect(fixture.procedures.accounting.listAccounts).toHaveBeenCalledOnce();
});

it("retains native constraints and cancelled values, restores focus and respects composition/select Escape", async () => {
  const fixture = readyChart();
  await entry();
  const code = screen.getByLabelText<HTMLInputElement>("Code");
  expect(code.required).toBe(true);
  expect(code.maxLength).toBe(32);
  expect(screen.getByLabelText<HTMLInputElement>("Account name").maxLength).toBe(160);
  fireEvent.keyDown(code, { key: "Escape", isComposing: true });
  expect(screen.getByLabelText("Account name")).toBeTruthy();
  fireEvent.keyDown(screen.getByRole("combobox"), { key: "Escape" });
  expect(screen.getByLabelText("Account name")).toBeTruthy();
  fireEvent.keyDown(code, { key: "Escape" });
  await waitFor(() =>
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Add account" })),
  );
  await userEvent.click(screen.getByRole("button", { name: "Add account" }));
  expect(code.isConnected).toBe(false);
  expect(screen.getByLabelText<HTMLInputElement>("Code").value).toBe("6700");
  expect(screen.getByLabelText<HTMLInputElement>("Account name").value).toBe("Synthetic insurance");
  expect(fixture.procedures.accounting.createAccount).not.toHaveBeenCalled();
});

it("guards repeated submits synchronously and preserves the exact ordinary account command", async () => {
  const fixture = readyChart();
  let resolve: (frame: Uint8Array) => void = () => {};
  fixture.procedures.accounting.createAccount.mockImplementation(
    () =>
      new Promise<Uint8Array>((done) => {
        resolve = done;
      }),
  );
  const form = await entry();
  try {
    act(() => {
      fireEvent.submit(form);
      fireEvent.submit(form);
    });
    expect(fixture.procedures.accounting.createAccount).toHaveBeenCalledOnce();
    const frame = fixture.procedures.accounting.createAccount.mock.calls[0]?.[0];
    if (!frame) throw new Error("Expected create frame");
    expect(createCodec.decodeRequest(frame)).toMatchObject({
      organisationId: workspace.organisationId,
      code: "6700",
      name: "Synthetic insurance",
      type: AccountType.EXPENSE,
      normalBalance: NormalBalance.DEBIT,
      reportClassification: "custom.account",
      cashFlowClassification: "noncash",
      commandContext: {
        authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
      },
    });
    expect(createCodec.decodeRequest(frame).commandContext?.idempotencyKey).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
    expect(screen.getByLabelText("Account name").matches(":disabled")).toBe(true);
    expect(screen.getByRole("button", { name: "Cancel" }).hasAttribute("disabled")).toBe(true);
    fireEvent.keyDown(screen.getByLabelText("Code"), { key: "Escape" });
    expect(screen.getByLabelText("Account name")).toBeTruthy();
  } finally {
    await act(async () => {
      resolve(
        createCodec.encodeResponse(
          create(CreateAccountResponseSchema, { account: account("6700") }),
        ),
      );
    });
  }
  expect(await screen.findByRole("status")).toBeTruthy();
  expect(screen.queryByLabelText("Account name")).toBeNull();
  await waitFor(() =>
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Add account" })),
  );
});

it("preserves rejected input, focuses persistent feedback and clears it after correction without writing", async () => {
  const fixture = readyChart();
  fixture.procedures.accounting.createAccount.mockRejectedValue(new Error("SYNTHETIC_DUPLICATE"));
  await entry();
  await userEvent.click(screen.getByRole("button", { name: "Save account" }));
  expect(await screen.findByRole("alert")).toBeTruthy();
  expect(document.activeElement).toBe(screen.getByRole("region", { name: "Account error" }));
  expect(screen.getByLabelText<HTMLInputElement>("Code").value).toBe("6700");
  await userEvent.type(screen.getByLabelText("Code"), "A");
  expect(screen.queryByRole("alert")).toBeNull();
  expect(fixture.procedures.accounting.createAccount).toHaveBeenCalledOnce();
});

it("distinguishes a confirmed creation from refresh failure and blocks another write until a verified read", async () => {
  const fixture = readyChart();
  fixture.procedures.accounting.createAccount.mockResolvedValue(
    createCodec.encodeResponse(create(CreateAccountResponseSchema, { account: account("6700") })),
  );
  vi.spyOn(fixture.conveyor.accounting.listAccounts, "invalidate").mockRejectedValueOnce(
    new Error("SYNTHETIC_REFRESH_FAILURE"),
  );
  await entry();
  await userEvent.click(screen.getByRole("button", { name: "Save account" }));
  expect((await screen.findByRole("alert")).textContent).toContain(
    "Account saved, but the chart could not refresh",
  );
  expect(screen.getByRole("button", { name: "Add account" }).hasAttribute("disabled")).toBe(true);
  expect(screen.queryByLabelText("Account name")).toBeNull();
  fixture.procedures.accounting.listAccounts.mockResolvedValue(
    codec.encodeResponse(
      create(ListAccountsResponseSchema, { accounts: [account("6100"), account("6700")] }),
    ),
  );
  await userEvent.click(screen.getByRole("button", { name: "Refresh accounts" }));
  await screen.findByText("Synthetic account 6700");
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Add account" }).hasAttribute("disabled")).toBe(
      false,
    ),
  );
  expect(screen.queryByRole("alert")).toBeNull();
  expect(fixture.procedures.accounting.createAccount).toHaveBeenCalledOnce();
});

it("shows loading, empty and read failure distinctly and retries reads explicitly", async () => {
  const fixture = createConveyorFixture();
  let reject: (error: Error) => void = () => {};
  fixture.procedures.accounting.listAccounts.mockImplementationOnce(
    () =>
      new Promise<Uint8Array>((_resolve, done) => {
        reject = done;
      }),
  );
  render(
    <QueryClientProvider client={fixture.queryClient}>
      <ChartScreen api={fixture.conveyor.accounting} workspace={workspace} />
    </QueryClientProvider>,
  );
  expect(screen.getByRole("heading", { name: "Loading accounts" })).toBeTruthy();
  expect(screen.getByRole("button", { name: "Add account" }).hasAttribute("disabled")).toBe(true);
  await act(async () => {
    reject(new Error("SYNTHETIC_READ_FAILURE"));
  });
  expect(await screen.findByRole("heading", { name: "Chart unavailable" })).toBeTruthy();
  fixture.procedures.accounting.listAccounts.mockResolvedValue(
    codec.encodeResponse(create(ListAccountsResponseSchema, {})),
  );
  await userEvent.click(screen.getByRole("button", { name: "Refresh accounts" }));
  expect(await screen.findByRole("heading", { name: "No accounts yet" })).toBeTruthy();
  expect(fixture.procedures.accounting.listAccounts).toHaveBeenCalledTimes(2);
});

it("renders the installed local chart through the named accounting method", async () => {
  const fixture = createConveyorFixture();
  fixture.procedures.accounting.listAccounts.mockImplementation(async (frame: Uint8Array) => {
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

  render(
    <QueryClientProvider client={fixture.queryClient}>
      <ChartScreen
        api={fixture.conveyor.accounting}
        workspace={{
          sessionId: "018f0000-0000-7000-8000-000000000003",
          userId: "018f0000-0000-7000-8000-000000000002",
          workspaceId: "018f0000-0000-7000-8000-000000000001",
          organisationId: "018f0000-0000-7000-8000-000000000004",
        }}
      />
    </QueryClientProvider>,
  );

  expect(await screen.findByText("Business bank")).toBeTruthy();
  expect(screen.getByText("Accounts payable")).toBeTruthy();
  expect(screen.getByText("2 loaded accounts")).toBeTruthy();
  expect(screen.getByText("Asset")).toBeTruthy();
  expect(screen.getByText("Liability")).toBeTruthy();
  expect(fixture.procedures.accounting.listAccounts).toHaveBeenCalledOnce();
});
