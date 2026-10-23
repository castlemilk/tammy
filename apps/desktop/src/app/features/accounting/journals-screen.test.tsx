import { create } from "@bufbuild/protobuf";
import {
  AccountDesignation,
  AccountSchema,
  AccountStatus,
  AccountType,
  GetJournalRequestSchema,
  GetJournalResponseSchema,
  JournalLineSchema,
  JournalSchema,
  JournalSource,
  JournalState,
  ListAccountsRequestSchema,
  ListAccountsResponseSchema,
  ListJournalsRequestSchema,
  ListJournalsResponseSchema,
  NormalBalance,
  PostManualJournalRequestSchema,
  PostManualJournalResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  CivilDateSchema,
  MoneySchema,
  PageInfoSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { createConveyorFixture, renderWithConveyor as render } from "../../test/conveyor-fixtures";
import { JournalsScreen } from "./journals-screen";

const accountsCodec = createProtoMethodCodec({
  input: ListAccountsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 131_072,
  output: ListAccountsResponseSchema,
});
const journalsCodec = createProtoMethodCodec({
  input: ListJournalsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: ListJournalsResponseSchema,
});

it("renders retained journals and exposes the balanced manual journal action", async () => {
  const organisationId = "018f0000-0000-7000-8000-000000000004";
  const accounts = [
    create(AccountSchema, {
      id: "018f0000-0000-7000-8000-000000000201",
      organisationId,
      version: 1n,
      code: "6100",
      name: "Office expenses",
      type: AccountType.EXPENSE,
      normalBalance: NormalBalance.DEBIT,
      status: AccountStatus.ACTIVE,
      designation: AccountDesignation.ORDINARY,
      reportClassification: "profit_loss.expense",
      cashFlowClassification: "noncash",
    }),
    create(AccountSchema, {
      id: "018f0000-0000-7000-8000-000000000202",
      organisationId,
      version: 1n,
      code: "3100",
      name: "Owner contributions",
      type: AccountType.EQUITY,
      normalBalance: NormalBalance.CREDIT,
      status: AccountStatus.ACTIVE,
      designation: AccountDesignation.ORDINARY,
      reportClassification: "balance_sheet.equity",
      cashFlowClassification: "noncash",
    }),
  ];
  const journal = create(JournalSchema, {
    id: "018f0000-0000-7000-8000-000000000210",
    organisationId,
    version: 1n,
    state: JournalState.POSTED,
    source: JournalSource.MANUAL,
    postingDate: create(CivilDateSchema, { year: 2026, month: 8, day: 10 }),
    memo: "Office supplies",
    totalDebits: create(MoneySchema, { currencyCode: "AUD", minorUnits: 31900n }),
    totalCredits: create(MoneySchema, { currencyCode: "AUD", minorUnits: 31900n }),
    financialRevision: 1n,
  });
  const fixture = createConveyorFixture();
  fixture.procedures.accounting.listAccounts.mockImplementation(async (frame: Uint8Array) => {
    expect(accountsCodec.decodeRequest(frame).organisationId).toBe(organisationId);
    return accountsCodec.encodeResponse(
      create(ListAccountsResponseSchema, {
        accounts,
        page: create(PageInfoSchema, { returnedCount: 2 }),
      }),
    );
  });
  fixture.procedures.accounting.listJournals.mockImplementation(async (frame: Uint8Array) => {
    expect(journalsCodec.decodeRequest(frame).organisationId).toBe(organisationId);
    return journalsCodec.encodeResponse(
      create(ListJournalsResponseSchema, {
        journals: [journal],
        page: create(PageInfoSchema, { returnedCount: 1 }),
      }),
    );
  });
  const onNavigate = vi.fn();
  const user = userEvent.setup();

  render(
    <JournalsScreen
      api={fixture.conveyor.accounting}
      onNavigate={onNavigate}
      path="/accounting/journals"
      workspace={{
        workspaceId: "018f0000-0000-7000-8000-000000000001",
        userId: "018f0000-0000-7000-8000-000000000002",
        sessionId: "018f0000-0000-7000-8000-000000000003",
        organisationId,
      }}
    />,
  );

  expect(await screen.findByText("Office supplies")).toBeTruthy();
  expect(screen.getByText("$319.00")).toBeTruthy();
  const journalAction = screen.getByRole("button", { name: "Open journal: Office supplies" });
  journalAction.focus();
  await user.keyboard("{Enter}");
  expect(onNavigate).toHaveBeenCalledWith(`/accounting/journals?journal=${journal.id}`);
  await user.click(screen.getByRole("button", { name: "New journal" }));
  expect(screen.getByLabelText("Debit account")).toBeTruthy();
  expect(screen.getByLabelText("Credit account")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Post journal" })).toBeTruthy();
});

const detailCodec = createProtoMethodCodec({
  input: GetJournalRequestSchema,
  output: GetJournalResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 262144,
});
const postCodec = createProtoMethodCodec({
  input: PostManualJournalRequestSchema,
  output: PostManualJournalResponseSchema,
  maximumRequestBytes: 131072,
  maximumResponseBytes: 262144,
});
const testWorkspace = {
  workspaceId: "018f0000-0000-7000-8000-000000000001",
  userId: "018f0000-0000-7000-8000-000000000002",
  sessionId: "018f0000-0000-7000-8000-000000000003",
  organisationId: "018f0000-0000-7000-8000-000000000004",
};
function journalFixture() {
  const fixture = createConveyorFixture();
  const accounts = [
    create(AccountSchema, {
      id: "018f0000-0000-7000-8000-000000000201",
      organisationId: testWorkspace.organisationId,
      version: 1n,
      code: "6100",
      name: "Office expenses",
      type: AccountType.EXPENSE,
      designation: AccountDesignation.ORDINARY,
      status: AccountStatus.ACTIVE,
      normalBalance: NormalBalance.DEBIT,
    }),
    create(AccountSchema, {
      id: "018f0000-0000-7000-8000-000000000202",
      organisationId: testWorkspace.organisationId,
      version: 1n,
      code: "3100",
      name: "Owner contributions",
      type: AccountType.EQUITY,
      designation: AccountDesignation.ORDINARY,
      status: AccountStatus.ACTIVE,
      normalBalance: NormalBalance.CREDIT,
    }),
  ];
  const debit = accounts[0];
  const credit = accounts[1];
  if (!debit || !credit) throw new Error("ACCOUNT_FIXTURE_MISSING");
  const journal = create(JournalSchema, {
    id: "018f0000-0000-7000-8000-000000000210",
    organisationId: testWorkspace.organisationId,
    version: 1n,
    state: JournalState.POSTED,
    source: JournalSource.MANUAL,
    postingDate: create(CivilDateSchema, { year: 2024, month: 5, day: 10 }),
    memo: "Synthetic studio supplies",
    totalDebits: create(MoneySchema, { currencyCode: "AUD", minorUnits: 1550n }),
    totalCredits: create(MoneySchema, { currencyCode: "AUD", minorUnits: 1550n }),
    financialRevision: 1n,
    lines: [
      create(JournalLineSchema, {
        id: "018f0000-0000-7000-8000-000000000211",
        journalId: "018f0000-0000-7000-8000-000000000210",
        sequence: 1,
        accountId: debit.id,
        description: "Synthetic studio supplies",
        debit: create(MoneySchema, { currencyCode: "AUD", minorUnits: 1550n }),
        credit: create(MoneySchema, { currencyCode: "AUD" }),
      }),
      create(JournalLineSchema, {
        id: "018f0000-0000-7000-8000-000000000212",
        journalId: "018f0000-0000-7000-8000-000000000210",
        sequence: 2,
        accountId: credit.id,
        description: "Synthetic studio supplies",
        debit: create(MoneySchema, { currencyCode: "AUD" }),
        credit: create(MoneySchema, { currencyCode: "AUD", minorUnits: 1550n }),
      }),
    ],
  });
  fixture.procedures.accounting.listAccounts.mockResolvedValue(
    accountsCodec.encodeResponse(
      create(ListAccountsResponseSchema, {
        accounts,
        page: create(PageInfoSchema, { returnedCount: accounts.length }),
      }),
    ),
  );
  fixture.procedures.accounting.listJournals.mockResolvedValue(
    journalsCodec.encodeResponse(
      create(ListJournalsResponseSchema, {
        journals: [journal],
        page: create(PageInfoSchema, { returnedCount: 1 }),
      }),
    ),
  );
  fixture.procedures.accounting.getJournal.mockResolvedValue(
    detailCodec.encodeResponse(create(GetJournalResponseSchema, { journal })),
  );
  fixture.procedures.accounting.postManualJournal.mockResolvedValue(
    postCodec.encodeResponse(create(PostManualJournalResponseSchema, { journal })),
  );
  const onNavigate = vi.fn();
  const element = (path = "/accounting/journals") => (
    <JournalsScreen
      api={fixture.conveyor.accounting}
      onNavigate={onNavigate}
      path={path}
      workspace={testWorkspace}
    />
  );
  return { fixture, journal, debit, credit, onNavigate, element };
}
async function fillDraft(user: ReturnType<typeof userEvent.setup>, debit: string, credit: string) {
  const open = await screen.findByRole("button", { name: "New journal" });
  await waitFor(() => expect((open as HTMLButtonElement).disabled).toBe(false));
  await user.click(open);
  expect(document.activeElement).toBe(screen.getByLabelText("Date"));
  fireEvent.change(screen.getByLabelText("Date"), { target: { value: "2024-05-10" } });
  await user.type(screen.getByLabelText("Description"), "Synthetic studio supplies");
  await user.type(screen.getByLabelText("Amount (AUD)"), "15.50");
  await user.selectOptions(screen.getByRole("combobox", { name: "Debit account" }), debit);
  await user.selectOptions(screen.getByRole("combobox", { name: "Credit account" }), credit);
}

it("retains a cancelled draft and restores focus while respecting composition", async () => {
  const f = journalFixture();
  const user = userEvent.setup();
  render(f.element());
  await fillDraft(user, f.debit.id, f.credit.id);
  const description = screen.getByLabelText("Description");
  fireEvent.keyDown(screen.getByLabelText("Date"), { key: "Escape" });
  fireEvent.keyDown(screen.getByRole("combobox", { name: "Debit account" }), { key: "Escape" });
  expect(screen.getByRole("form", { name: "Manual journal entry" })).toBeTruthy();
  description.focus();
  fireEvent.keyDown(description, { key: "Escape", isComposing: true });
  expect(screen.getByRole("form", { name: "Manual journal entry" })).toBeTruthy();
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("form", { name: "Manual journal entry" })).toBeNull();
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "New journal" }));
  await user.click(screen.getByRole("button", { name: "New journal" }));
  expect((screen.getByLabelText("Description") as HTMLInputElement).value).toBe(
    "Synthetic studio supplies",
  );
  expect((screen.getByLabelText("Amount (AUD)") as HTMLInputElement).valueAsNumber).toBe(15.5);
  expect(f.fixture.procedures.accounting.postManualJournal).not.toHaveBeenCalled();
});

it("posts one exact balanced command when submit is repeated while pending", async () => {
  const f = journalFixture();
  const user = userEvent.setup();
  let finish: ((value: Uint8Array) => void) | undefined;
  f.fixture.procedures.accounting.postManualJournal.mockImplementation(
    () =>
      new Promise<Uint8Array>((resolve) => {
        finish = resolve;
      }),
  );
  render(f.element());
  await fillDraft(user, f.debit.id, f.credit.id);
  const form = screen.getByRole("form", { name: "Manual journal entry" });
  fireEvent.submit(form);
  fireEvent.submit(form);
  expect(f.fixture.procedures.accounting.postManualJournal).toHaveBeenCalledOnce();
  expect(screen.getByLabelText("Description").matches(":disabled")).toBe(true);
  expect((screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement).disabled).toBe(true);
  const frame = f.fixture.procedures.accounting.postManualJournal.mock.calls[0]?.[0];
  if (!frame) throw new Error("POST_FRAME_MISSING");
  const request = postCodec.decodeRequest(frame);
  expect(request.lines).toHaveLength(2);
  expect(request.lines[0]?.debit?.minorUnits).toBe(1550n);
  expect(request.lines[1]?.credit?.minorUnits).toBe(1550n);
  expect(request.lines[0]?.accountId).toBe(f.debit.id);
  expect(request.lines[1]?.accountId).toBe(f.credit.id);
  expect(request.commandContext?.authentication?.sessionId).toBe(testWorkspace.sessionId);
  await act(async () => {
    if (!finish) throw new Error("POST_FINISH_MISSING");
    finish(
      postCodec.encodeResponse(create(PostManualJournalResponseSchema, { journal: f.journal })),
    );
  });
  await waitFor(() => expect(f.onNavigate).toHaveBeenCalledOnce());
  expect(f.onNavigate).toHaveBeenCalledWith(`/accounting/journals?journal=${f.journal.id}`);
});

it("retains a failed posting draft and focuses its persistent error", async () => {
  const f = journalFixture();
  const user = userEvent.setup();
  f.fixture.procedures.accounting.postManualJournal.mockRejectedValueOnce(
    new Error("synthetic interrupted response"),
  );
  render(f.element());
  await fillDraft(user, f.debit.id, f.credit.id);
  await user.click(screen.getByRole("button", { name: "Post journal" }));
  const error = await screen.findByRole("region", { name: "Journal error" });
  expect(within(error).getByRole("alert").textContent).toContain("could not be posted");
  expect(document.activeElement).toBe(error);
  expect((screen.getByLabelText("Description") as HTMLInputElement).value).toBe(
    "Synthetic studio supplies",
  );
  expect((screen.getByLabelText("Amount (AUD)") as HTMLInputElement).valueAsNumber).toBe(15.5);
  expect((screen.getByLabelText("Description") as HTMLInputElement).disabled).toBe(false);
  expect(f.onNavigate).not.toHaveBeenCalled();
});

it("restores retained search and the journal action after detail navigation", async () => {
  const f = journalFixture();
  const user = userEvent.setup();
  const view = render(f.element());
  const search = screen.getByRole("searchbox", { name: "Search journal descriptions" });
  await user.type(search, "studio");
  const open = await screen.findByRole("button", {
    name: "Open journal: Synthetic studio supplies",
  });
  await user.click(open);
  view.rerender(f.element(`/accounting/journals?journal=${f.journal.id}`));
  const heading = await screen.findByRole("heading", { name: "Synthetic studio supplies" });
  await waitFor(() => expect(document.activeElement).toBe(heading));
  expect(screen.queryByRole("button", { name: "Post journal" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Journals" }));
  view.rerender(f.element());
  expect(
    (screen.getByRole("searchbox", { name: "Search journal descriptions" }) as HTMLInputElement)
      .value,
  ).toBe("studio");
  expect(document.activeElement).toBe(
    screen.getByRole("button", { name: "Open journal: Synthetic studio supplies" }),
  );
  await user.clear(screen.getByRole("searchbox", { name: "Search journal descriptions" }));
  await user.type(
    screen.getByRole("searchbox", { name: "Search journal descriptions" }),
    "No matching synthetic entry",
  );
  expect(screen.getByRole("heading", { name: "No matching journals" })).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Clear search" }));
  expect(document.activeElement).toBe(
    screen.getByRole("searchbox", { name: "Search journal descriptions" }),
  );
  expect(
    screen.getByRole("button", { name: "Open journal: Synthetic studio supplies" }),
  ).toBeTruthy();
});

it("shows unavailable detail explicitly and retries only its read", async () => {
  const f = journalFixture();
  const user = userEvent.setup();
  f.fixture.procedures.accounting.getJournal.mockRejectedValueOnce(
    new Error("synthetic unavailable detail"),
  );
  render(f.element(`/accounting/journals?journal=${f.journal.id}`));
  expect(await screen.findByRole("heading", { name: "Journal unavailable" })).toBeTruthy();
  expect(screen.queryByRole("table", { name: "Journal history" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Retry journal" }));
  expect(await screen.findByRole("heading", { name: "Synthetic studio supplies" })).toBeTruthy();
  expect(f.fixture.procedures.accounting.getJournal).toHaveBeenCalledTimes(2);
  expect(f.fixture.procedures.accounting.postManualJournal).not.toHaveBeenCalled();
});

it("distinguishes initial loading from an empty journal history", async () => {
  const f = journalFixture();
  let finish: ((value: Uint8Array) => void) | undefined;
  f.fixture.procedures.accounting.listJournals.mockImplementation(
    () =>
      new Promise<Uint8Array>((resolve) => {
        finish = resolve;
      }),
  );
  render(f.element());
  expect(await screen.findByRole("heading", { name: "Loading journals" })).toBeTruthy();
  expect((screen.getByRole("button", { name: "New journal" }) as HTMLButtonElement).disabled).toBe(
    true,
  );
  await act(async () => {
    if (!finish) throw new Error("LIST_FINISH_MISSING");
    finish(
      journalsCodec.encodeResponse(
        create(ListJournalsResponseSchema, { journals: [], page: create(PageInfoSchema) }),
      ),
    );
  });
  expect(await screen.findByRole("heading", { name: "No journals yet" })).toBeTruthy();
  expect((screen.getByRole("button", { name: "New journal" }) as HTMLButtonElement).disabled).toBe(
    false,
  );
  expect(f.fixture.procedures.accounting.postManualJournal).not.toHaveBeenCalled();
});

it("recovers an unavailable history through its read-only retry", async () => {
  const f = journalFixture();
  const user = userEvent.setup();
  f.fixture.procedures.accounting.listJournals.mockRejectedValueOnce(
    new Error("synthetic history failure"),
  );
  render(f.element());
  expect(await screen.findByRole("heading", { name: "Journals unavailable" })).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Retry journals" }));
  expect(
    await screen.findByRole("button", { name: "Open journal: Synthetic studio supplies" }),
  ).toBeTruthy();
  expect(f.fixture.procedures.accounting.postManualJournal).not.toHaveBeenCalled();
});

it("retains exact large posted amounts in the read-only detail", async () => {
  const f = journalFixture();
  const minorUnits = 9_007_199_254_740_991n;
  f.journal.totalDebits = create(MoneySchema, { currencyCode: "AUD", minorUnits });
  f.journal.totalCredits = create(MoneySchema, { currencyCode: "AUD", minorUnits });
  const debit = f.journal.lines[0];
  const credit = f.journal.lines[1];
  if (!debit || !credit) throw new Error("LINES_MISSING");
  debit.debit = create(MoneySchema, { currencyCode: "AUD", minorUnits });
  credit.credit = create(MoneySchema, { currencyCode: "AUD", minorUnits });
  f.fixture.procedures.accounting.getJournal.mockResolvedValue(
    detailCodec.encodeResponse(create(GetJournalResponseSchema, { journal: f.journal })),
  );
  render(f.element(`/accounting/journals?journal=${f.journal.id}`));
  const region = await screen.findByRole("region", { name: "Journal entry lines table" });
  expect(within(region).getAllByText("$90,071,992,547,409.91")).toHaveLength(4);
  expect(screen.queryByRole("button", { name: "Post journal" })).toBeNull();
});

it("clears stale validation feedback after correcting the draft without posting", async () => {
  const f = journalFixture();
  const user = userEvent.setup();
  render(f.element());
  await fillDraft(user, f.credit.id, f.credit.id);
  await user.click(screen.getByRole("button", { name: "Post journal" }));
  expect(await screen.findByRole("alert")).toHaveProperty(
    "textContent",
    expect.stringContaining("Choose two different accounts"),
  );
  await user.selectOptions(screen.getByRole("combobox", { name: "Debit account" }), f.debit.id);
  expect(screen.queryByRole("alert")).toBeNull();
  expect((screen.getByLabelText("Amount (AUD)") as HTMLInputElement).valueAsNumber).toBe(15.5);
  expect(f.fixture.procedures.accounting.postManualJournal).not.toHaveBeenCalled();
});
