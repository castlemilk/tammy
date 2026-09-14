import { create } from "@bufbuild/protobuf";
import {
  JournalSchema,
  JournalSource,
  JournalState,
  ListJournalsRequestSchema,
  ListJournalsResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  CivilDateSchema,
  MoneySchema,
  PageInfoSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import { screen } from "@testing-library/react";
import { expect, it } from "vitest";

import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { createConveyorFixture, renderWithConveyor as render } from "../../test/conveyor-fixtures";
import { AuditScreen } from "./audit-screen";

const journalsCodec = createProtoMethodCodec({
  input: ListJournalsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: ListJournalsResponseSchema,
});

it("keeps available retained activity visible when another local projection is unavailable", async () => {
  const organisationId = "018f0000-0000-7000-8000-000000000004";
  const fixture = createConveyorFixture();
  fixture.procedures.accounting.listJournals.mockImplementation(async () =>
    journalsCodec.encodeResponse(
      create(ListJournalsResponseSchema, {
        journals: [
          create(JournalSchema, {
            id: "018f0000-0000-7000-8000-000000000501",
            organisationId,
            version: 1n,
            state: JournalState.POSTED,
            source: JournalSource.MANUAL,
            postingDate: create(CivilDateSchema, { year: 2026, month: 8, day: 10 }),
            memo: "Office supplies paid personally",
            totalDebits: create(MoneySchema, { currencyCode: "AUD", minorUnits: 31900n }),
            totalCredits: create(MoneySchema, { currencyCode: "AUD", minorUnits: 31900n }),
          }),
        ],
        page: create(PageInfoSchema, { returnedCount: 1 }),
      }),
    ),
  );

  render(
    <AuditScreen
      accounting={fixture.conveyor.accounting}
      banking={fixture.conveyor.banking}
      documents={fixture.conveyor.documents}
      reporting={fixture.conveyor.reporting}
      workspace={{
        workspaceId: "018f0000-0000-7000-8000-000000000001",
        userId: "018f0000-0000-7000-8000-000000000002",
        sessionId: "018f0000-0000-7000-8000-000000000003",
        organisationId,
      }}
    />,
  );

  expect(await screen.findByText("Office supplies paid personally")).toBeTruthy();
  expect(screen.getByText("Journal")).toBeTruthy();
  expect(screen.getByText(/\$319\.00 debits and credits/)).toBeTruthy();
  expect(
    screen.getByText(/does not replace exported audit-chain verification evidence/),
  ).toBeTruthy();
});
