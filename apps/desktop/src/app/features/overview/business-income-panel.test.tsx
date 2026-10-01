import { create } from "@bufbuild/protobuf";
import {
  BusinessIncomeSummarySchema,
  GetBusinessIncomeSummaryRequestSchema,
  GetBusinessIncomeSummaryResponseSchema,
} from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { GstBasis } from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { expect, it } from "vitest";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { createConveyorFixture, renderWithConveyor } from "../../test/conveyor-fixtures";
import { BusinessIncomePanel } from "./business-income-panel";

const codec = createProtoMethodCodec({
  input: GetBusinessIncomeSummaryRequestSchema,
  output: GetBusinessIncomeSummaryResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 32768,
});
const workspace = {
  workspaceId: "018f0000-0000-7000-8000-000000000001",
  userId: "018f0000-0000-7000-8000-000000000002",
  sessionId: "018f0000-0000-7000-8000-000000000003",
  organisationId: "018f0000-0000-7000-8000-000000000004",
};
it("reads ledger business income with explicit periods and balances as of a separate date", async () => {
  const fixture = createConveyorFixture();
  fixture.procedures.accounting.getBusinessIncomeSummary.mockImplementation(
    async (frame: Uint8Array) => {
      const request = codec.decodeRequest(frame);
      expect(request.organisationId).toBe(workspace.organisationId);
      expect(request.authentication?.actorUserId).toBe(workspace.userId);
      return codec.encodeResponse(
        create(GetBusinessIncomeSummaryResponseSchema, {
          summary: create(BusinessIncomeSummarySchema, {
            organisationId: request.organisationId,
            periodStart: request.periodStart,
            periodEnd: request.periodEnd,
            asOfDate: request.asOfDate,
            businessIncome: { currencyCode: "AUD", minorUnits: 25000n },
            invoiceIncome: { currencyCode: "AUD", minorUnits: 10000n },
            paymentsReceived: { currencyCode: "AUD", minorUnits: 5500n },
            outstanding: { currencyCode: "AUD", minorUnits: 16500n },
            invoiceGst: { currencyCode: "AUD", minorUnits: 500n },
            unpostedIncome: { currencyCode: "AUD", minorUnits: 4000n },
            issuedCount: 1n,
            unpostedIssuedCount: 2n,
            gstBasis: GstBasis.CASH,
            financialRevision: 4n,
          }),
        }),
      );
    },
  );
  renderWithConveyor(
    <BusinessIncomePanel
      api={fixture.conveyor.accounting}
      now={new Date(2026, 9, 2)}
      workspace={workspace}
    />,
  );
  expect(await screen.findByTestId("business-income-total")).toHaveProperty(
    "textContent",
    expect.stringContaining("$250.00"),
  );
  expect(screen.getByTestId("business-invoice-income").textContent).toContain("$100.00");
  expect(screen.getByTestId("business-outstanding").textContent).toContain("$165.00");
  expect(screen.getByText(/2 older issued invoices need accounting review/)).toBeTruthy();
  const initialFrame = fixture.procedures.accounting.getBusinessIncomeSummary.mock.calls[0]?.[0];
  if (!initialFrame) throw new Error("Missing initial income request");
  const initial = codec.decodeRequest(initialFrame);
  expect(initial.periodStart).toMatchObject({ year: 2026, month: 10, day: 1 });
  expect(initial.periodEnd).toMatchObject({ year: 2026, month: 12, day: 31 });
  expect(initial.asOfDate).toMatchObject({ year: 2026, month: 10, day: 2 });
  fireEvent.change(screen.getByLabelText("Income period"), { target: { value: "financial-year" } });
  await waitFor(() =>
    expect(fixture.procedures.accounting.getBusinessIncomeSummary).toHaveBeenCalledTimes(2),
  );
  const yearlyFrame = fixture.procedures.accounting.getBusinessIncomeSummary.mock.calls[1]?.[0];
  if (!yearlyFrame) throw new Error("Missing yearly income request");
  const yearly = codec.decodeRequest(yearlyFrame);
  expect(yearly.periodStart).toMatchObject({ year: 2026, month: 7, day: 1 });
  expect(yearly.periodEnd).toMatchObject({ year: 2027, month: 6, day: 30 });
  expect(yearly.asOfDate).toMatchObject({ year: 2026, month: 10, day: 2 });
});
