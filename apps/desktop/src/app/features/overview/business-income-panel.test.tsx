import { create } from "@bufbuild/protobuf";
import {
  BusinessIncomeSummarySchema,
  GetBusinessIncomeSummaryRequestSchema,
  GetBusinessIncomeSummaryResponseSchema,
} from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import {
  GetOrganisationRequestSchema,
  GetOrganisationResponseSchema,
  GstBasis,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
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

it("exports the selected custom dates, separate balance date and displayed revision without sending amounts", async () => {
  const fixture = exportFixture();
  await screen.findByTestId("business-income-total");
  await screen.findByText(/Trusted Name/);
  fireEvent.change(screen.getByLabelText("Income period start"), {
    target: { value: "2026-09-01" },
  });
  fireEvent.change(screen.getByLabelText("Income period end"), { target: { value: "2026-09-30" } });
  fireEvent.change(screen.getByLabelText("Balances as of"), { target: { value: "2026-10-05" } });
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Export CSV" })).toHaveProperty("disabled", false),
  );
  fireEvent.click(screen.getByRole("button", { name: "Export CSV" }));
  await waitFor(() =>
    expect(fixture.procedures.reporting.exportIncome).toHaveBeenCalledWith({
      authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
      organisationId: workspace.organisationId,
      expectedOrganisationVersion: "7",
      format: "csv",
      periodStart: "2026-09-01",
      periodEnd: "2026-09-30",
      asOfDate: "2026-10-05",
      expectedFinancialRevision: "9",
    }),
  );
  expect(await screen.findByText("Income CSV exported.")).toBeTruthy();
  expect(screen.getByLabelText("Balances as of")).toHaveProperty("value", "2026-10-05");
});
const workspace = {
  workspaceId: "018f0000-0000-7000-8000-000000000001",
  userId: "018f0000-0000-7000-8000-000000000002",
  sessionId: "018f0000-0000-7000-8000-000000000003",
  organisationId: "018f0000-0000-7000-8000-000000000004",
};
function exportFixture() {
  const fixture = createConveyorFixture();
  const organisationCodec = createProtoMethodCodec({
    input: GetOrganisationRequestSchema,
    output: GetOrganisationResponseSchema,
    maximumRequestBytes: 8192,
    maximumResponseBytes: 32768,
  });
  fixture.procedures.organisation.getOrganisation.mockResolvedValue(
    organisationCodec.encodeResponse(
      create(GetOrganisationResponseSchema, {
        organisation: {
          id: workspace.organisationId,
          version: 7n,
          legalName: "Trusted Name",
          displayName: "Trusted",
          abn: "51824753556",
        },
      }),
    ),
  );
  fixture.procedures.accounting.getBusinessIncomeSummary.mockImplementation(async (frame) => {
    const request = codec.decodeRequest(frame);
    return codec.encodeResponse(
      create(GetBusinessIncomeSummaryResponseSchema, {
        summary: {
          organisationId: request.organisationId,
          periodStart: request.periodStart,
          periodEnd: request.periodEnd,
          asOfDate: request.asOfDate,
          financialRevision: 9n,
          gstBasis: GstBasis.CASH,
          businessIncome: { currencyCode: "AUD", minorUnits: 1n },
          invoiceIncome: { currencyCode: "AUD", minorUnits: 1n },
          paymentsReceived: { currencyCode: "AUD", minorUnits: 0n },
          outstanding: { currencyCode: "AUD", minorUnits: 0n },
          invoiceGst: { currencyCode: "AUD", minorUnits: 0n },
          unpostedIncome: { currencyCode: "AUD", minorUnits: 0n },
        },
      }),
    );
  });
  fixture.procedures.reporting.exportIncome.mockResolvedValue({ saved: true });
  renderWithConveyor(
    <BusinessIncomePanel
      api={fixture.conveyor.accounting}
      exportApi={fixture.conveyor.reporting}
      organisationApi={fixture.conveyor.organisation}
      now={new Date(2026, 9, 2)}
      workspace={workspace}
    />,
  );
  return fixture;
}
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

it("blocks exports for invalid income dates and a deferred summary refresh with old figures still displayed", async () => {
  const fixture = exportFixture();
  await screen.findByTestId("business-income-total");
  await screen.findByText(/Trusted Name/);
  const blocked = () => {
    expect(screen.getByRole("button", { name: "Export CSV" })).toHaveProperty("disabled", true);
    expect(screen.getByRole("button", { name: "Export PDF" })).toHaveProperty("disabled", true);
  };
  fireEvent.change(screen.getByLabelText("Balances as of"), { target: { value: "" } });
  blocked();
  fireEvent.click(screen.getByRole("button", { name: "Export CSV" }));
  expect(fixture.procedures.reporting.exportIncome).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText("Balances as of"), { target: { value: "2026-10-02" } });
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Export CSV" })).toHaveProperty("disabled", false),
  );
  fireEvent.change(screen.getByLabelText("Income period start"), {
    target: { value: "2027-01-01" },
  });
  blocked();
  fireEvent.change(screen.getByLabelText("Income period start"), {
    target: { value: "2026-10-01" },
  });
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Export CSV" })).toHaveProperty("disabled", false),
  );
  const retained =
    await fixture.procedures.accounting.getBusinessIncomeSummary.mock.results[0]?.value;
  let finish: ((frame: Uint8Array) => void) | undefined;
  fixture.procedures.accounting.getBusinessIncomeSummary.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Refresh income" }));
  await waitFor(() => expect(finish).toBeDefined());
  expect(screen.getByTestId("business-income-total").textContent).toContain("$0.01");
  blocked();
  fireEvent.click(screen.getByRole("button", { name: "Export PDF" }));
  expect(fixture.procedures.reporting.exportIncome).not.toHaveBeenCalled();
  if (!retained || !finish) throw new Error("Missing deferred summary");
  await act(async () => {
    finish?.(retained);
  });
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Export CSV" })).toHaveProperty("disabled", false),
  );
});
