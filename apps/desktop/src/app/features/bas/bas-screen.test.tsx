import { create } from "@bufbuild/protobuf";
import { CivilDateSchema, MoneySchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { GstBasis } from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import {
  GetReportingCapabilityRequestSchema,
  GetReportingCapabilityResponseSchema,
  ReportingCapabilitySchema,
  ReportingCapabilityStatus,
  ReportKind,
} from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import {
  BasSalesSourceLineSchema,
  BasSourceLineSchema,
  BasWorkpaperSchema,
  BasWorkpaperStatus,
  CreateBasDraftRequestSchema,
  CreateBasDraftResponseSchema,
  GetCurrentBasDraftRequestSchema,
  GetCurrentBasDraftResponseSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";

import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { createConveyorFixture, renderWithConveyor as render } from "../../test/conveyor-fixtures";
import { BasScreen } from "./bas-screen";

const getCodec = createProtoMethodCodec({
  input: GetCurrentBasDraftRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: GetCurrentBasDraftResponseSchema,
});
const createCodec = createProtoMethodCodec({
  input: CreateBasDraftRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: CreateBasDraftResponseSchema,
});
const reportingCodec = createProtoMethodCodec({
  input: GetReportingCapabilityRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetReportingCapabilityResponseSchema,
});

it("creates a local BAS draft from reviewed sources without exposing lodgement actions", async () => {
  const organisationId = "018f0000-0000-7000-8000-000000000004";
  const workpaper = create(BasWorkpaperSchema, {
    id: "018f0000-0000-7000-8000-000000000401",
    organisationId,
    version: 1n,
    periodStart: create(CivilDateSchema, { year: 2024, month: 4, day: 1 }),
    periodEnd: create(CivilDateSchema, { year: 2024, month: 6, day: 30 }),
    status: BasWorkpaperStatus.DRAFT_NOT_LODGED,
    salesG1: create(MoneySchema, { currencyCode: "AUD" }),
    gstOnSales1a: create(MoneySchema, { currencyCode: "AUD" }),
    gstCredits1b: create(MoneySchema, { currencyCode: "AUD", minorUnits: 2900n }),
    netGstPayable: create(MoneySchema, { currencyCode: "AUD", minorUnits: -2900n }),
    sources: [
      create(BasSourceLineSchema, {
        documentId: "018f0000-0000-7000-8000-000000000402",
        supplierName: "Officeworks Ltd",
        invoiceNumber: "INV-029847",
        documentDate: create(CivilDateSchema, { year: 2024, month: 5, day: 12 }),
        gross: create(MoneySchema, { currencyCode: "AUD", minorUnits: 31900n }),
        gstCredit: create(MoneySchema, { currencyCode: "AUD", minorUnits: 2900n }),
      }),
    ],
  });
  const fixture = createConveyorFixture();
  let currentWorkpaper: typeof workpaper | undefined;
  fixture.procedures.reporting.getCurrentBasDraft.mockImplementation(async () =>
    getCodec.encodeResponse(
      create(GetCurrentBasDraftResponseSchema, { workpaper: currentWorkpaper }),
    ),
  );
  fixture.procedures.reporting.createBasDraft.mockImplementation(async (frame: Uint8Array) => {
    const request = createCodec.decodeRequest(frame);
    expect(request.organisationId).toBe(organisationId);
    expect(request.periodStart).toMatchObject({ year: 2024, month: 4, day: 1 });
    expect(request.periodEnd).toMatchObject({ year: 2024, month: 6, day: 30 });
    currentWorkpaper = workpaper;
    return createCodec.encodeResponse(create(CreateBasDraftResponseSchema, { workpaper }));
  });
  fixture.procedures.reporting.getReportingCapability.mockImplementation(
    async (frame: Uint8Array) => {
      const request = reportingCodec.decodeRequest(frame);
      const available = request.report === ReportKind.GST_WORKPAPER;
      return reportingCodec.encodeResponse(
        create(GetReportingCapabilityResponseSchema, {
          capability: create(ReportingCapabilitySchema, {
            report: request.report,
            taxYear: request.taxYear,
            entityType: request.entityType,
            status: available
              ? ReportingCapabilityStatus.AVAILABLE
              : ReportingCapabilityStatus.UNSUPPORTED,
            appVersion: "test-core",
            summary: available
              ? "Tammy supports a local reviewed-document GST workpaper only."
              : "Complete BAS preparation, declaration, and lodgement are unavailable.",
          }),
        }),
      );
    },
  );
  const user = userEvent.setup();

  render(
    <BasScreen
      api={fixture.conveyor.reporting}
      now={new Date(2024, 4, 12)}
      workspace={{
        workspaceId: "018f0000-0000-7000-8000-000000000001",
        userId: "018f0000-0000-7000-8000-000000000002",
        sessionId: "018f0000-0000-7000-8000-000000000003",
        organisationId,
      }}
    />,
  );

  expect(
    await screen.findByText("Tammy supports a local reviewed-document GST workpaper only."),
  ).toBeTruthy();
  expect(
    screen.getByText("Complete BAS preparation, declaration, and lodgement are unavailable."),
  ).toBeTruthy();
  expect(fixture.procedures.reporting.getReportingCapability).toHaveBeenCalledTimes(2);

  await user.click(await screen.findByRole("button", { name: "Create local draft" }));
  expect(await screen.findByText("Draft — not lodged", { selector: "p" })).toBeTruthy();
  expect(screen.getByText("Officeworks Ltd")).toBeTruthy();
  expect(screen.getByRole("cell", { name: "$29.00" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: /lodge|submit|declare/i })).toBeNull();
  expect(screen.getByText(/no lodge, submit or declaration control/i)).toBeTruthy();
  expect(fixture.procedures.reporting.createBasDraft).toHaveBeenCalledOnce();
});

it("queries the selected AU tax year and blocks an unavailable workpaper year", async () => {
  const fixture = createConveyorFixture();
  fixture.procedures.reporting.getCurrentBasDraft.mockImplementation(async () =>
    getCodec.encodeResponse(create(GetCurrentBasDraftResponseSchema)),
  );
  fixture.procedures.reporting.getReportingCapability.mockImplementation(
    async (frame: Uint8Array) => {
      const request = reportingCodec.decodeRequest(frame);
      const available = request.report === ReportKind.GST_WORKPAPER && request.taxYear === 2024;
      return reportingCodec.encodeResponse(
        create(GetReportingCapabilityResponseSchema, {
          capability: create(ReportingCapabilitySchema, {
            report: request.report,
            taxYear: request.taxYear,
            entityType: request.entityType,
            status: available
              ? ReportingCapabilityStatus.AVAILABLE
              : ReportingCapabilityStatus.UNSUPPORTED,
            appVersion: "test-core",
            summary: available
              ? "Tammy supports a local reviewed-document GST workpaper only."
              : `Reporting is unavailable for tax year ${request.taxYear}.`,
          }),
        }),
      );
    },
  );
  const user = userEvent.setup();

  render(
    <BasScreen
      api={fixture.conveyor.reporting}
      now={new Date(2024, 4, 12)}
      workspace={{
        workspaceId: "018f0000-0000-7000-8000-000000000001",
        userId: "018f0000-0000-7000-8000-000000000002",
        sessionId: "018f0000-0000-7000-8000-000000000003",
        organisationId: "018f0000-0000-7000-8000-000000000004",
      }}
    />,
  );

  const start = screen.getByLabelText("Period start");
  const end = screen.getByLabelText("Period end");
  await user.clear(start);
  await user.type(start, "2025-04-01");
  await user.clear(end);
  await user.type(end, "2025-06-30");

  await waitFor(() => {
    const requests = fixture.procedures.reporting.getReportingCapability.mock.calls.map(([frame]) =>
      reportingCodec.decodeRequest(frame),
    );
    expect(requests.slice(-2).map((request) => request.taxYear)).toEqual([2025, 2025]);
  });
  expect(await screen.findAllByText("Unsupported in this build")).toHaveLength(2);
  expect(screen.queryByText("Available in this build")).toBeNull();
  const createButton = screen.getByRole("button", { name: "Create local draft" });
  expect((createButton as HTMLButtonElement).disabled).toBe(true);
  await user.click(createButton);
  expect(fixture.procedures.reporting.createBasDraft).not.toHaveBeenCalled();
});

it("keeps saved BAS figures and sales provenance visible until a stale draft is regenerated", async () => {
  const fixture = createConveyorFixture();
  const organisationId = "018f0000-0000-7000-8000-000000000004";
  let workpaper = create(BasWorkpaperSchema, {
    id: "018f0000-0000-7000-8000-000000000401",
    organisationId,
    version: 1n,
    periodStart: { year: 2026, month: 7, day: 1 },
    periodEnd: { year: 2026, month: 9, day: 30 },
    status: BasWorkpaperStatus.DRAFT_NOT_LODGED,
    gstBasis: GstBasis.CASH,
    sourcesChanged: true,
    salesG1: { currencyCode: "AUD", minorUnits: 5500n },
    gstOnSales1a: { currencyCode: "AUD", minorUnits: 500n },
    gstCredits1b: { currencyCode: "AUD", minorUnits: 0n },
    netGstPayable: { currencyCode: "AUD", minorUnits: 500n },
    reviewWarnings: ["Reviewed cash purchases need payment evidence."],
    excludedCashPurchaseCount: 2n,
    salesSources: [
      create(BasSalesSourceLineSchema, {
        invoiceId: "018f0000-0000-7000-8000-000000000402",
        invoiceNumber: "INV-100",
        customerName: "Receipt customer",
        taxDate: { year: 2026, month: 9, day: 30 },
        gross: { currencyCode: "AUD", minorUnits: 5500n },
        gst: { currencyCode: "AUD", minorUnits: 500n },
        paymentId: "018f0000-0000-7000-8000-000000000403",
        sourceRevision: 1n,
        sourceHash: new Uint8Array(32),
      }),
    ],
  });
  fixture.procedures.reporting.getCurrentBasDraft.mockImplementation(async () =>
    getCodec.encodeResponse(create(GetCurrentBasDraftResponseSchema, { workpaper })),
  );
  fixture.procedures.reporting.getReportingCapability.mockImplementation(
    async (frame: Uint8Array) => {
      const request = reportingCodec.decodeRequest(frame);
      return reportingCodec.encodeResponse(
        create(GetReportingCapabilityResponseSchema, {
          capability: {
            report: request.report,
            taxYear: request.taxYear,
            entityType: request.entityType,
            status:
              request.report === ReportKind.GST_WORKPAPER
                ? ReportingCapabilityStatus.AVAILABLE
                : ReportingCapabilityStatus.UNSUPPORTED,
            appVersion: "test",
            summary: "Local workpaper",
          },
        }),
      );
    },
  );
  fixture.procedures.reporting.createBasDraft.mockImplementation(async (frame: Uint8Array) => {
    const request = createCodec.decodeRequest(frame);
    expect(request.periodStart).toMatchObject({ year: 2026, month: 7, day: 1 });
    expect(request.periodEnd).toMatchObject({ year: 2026, month: 9, day: 30 });
    workpaper = create(BasWorkpaperSchema, {
      ...workpaper,
      version: 2n,
      sourcesChanged: false,
      salesG1: create(MoneySchema, { currencyCode: "AUD", minorUnits: 11000n }),
    });
    return createCodec.encodeResponse(create(CreateBasDraftResponseSchema, { workpaper }));
  });
  render(
    <BasScreen
      api={fixture.conveyor.reporting}
      now={new Date(2026, 9, 2)}
      workspace={{
        workspaceId: "018f0000-0000-7000-8000-000000000001",
        userId: "018f0000-0000-7000-8000-000000000002",
        sessionId: "018f0000-0000-7000-8000-000000000003",
        organisationId,
      }}
    />,
  );
  expect(await screen.findByText(/Sources changed since this workpaper was saved/)).toBeTruthy();
  expect(screen.getByText("Cash basis · sales follow recorded payments")).toBeTruthy();
  expect(screen.getByText("Reviewed cash purchases need payment evidence.")).toBeTruthy();
  expect(screen.getByRole("table", { name: "BAS sales sources" }).textContent).toContain(
    "Receipt customer",
  );
  expect(screen.getByTestId("bas-sales-g1").textContent).toContain("$55.00");
  expect(screen.getByLabelText("Period start")).toHaveProperty("value", "2026-07-01");
  await userEvent.click(await screen.findByRole("button", { name: "Regenerate local draft" }));
  await expect(
    fixture.procedures.reporting.createBasDraft.mock.results[0]?.value,
  ).resolves.toBeInstanceOf(Uint8Array);
  await waitFor(() => expect(screen.getByTestId("bas-sales-g1").textContent).toContain("$110.00"));
  expect(screen.queryByText(/Sources changed since this workpaper was saved/)).toBeNull();
});

it.each(["Period start", "Period end"])(
  "preserves the current quarter when %s is focused before an older draft arrives",
  async (label) => {
    const organisationId = "018f0000-0000-7000-8000-000000000004";
    const saved = create(BasWorkpaperSchema, {
      id: "018f0000-0000-7000-8000-000000000401",
      organisationId,
      version: 1n,
      periodStart: { year: 2026, month: 7, day: 1 },
      periodEnd: { year: 2026, month: 9, day: 30 },
      status: BasWorkpaperStatus.DRAFT_NOT_LODGED,
      gstBasis: GstBasis.NON_CASH,
      salesG1: { currencyCode: "AUD", minorUnits: 11000n },
    });
    const deferred = Promise.withResolvers<Uint8Array>();
    const fixture = createConveyorFixture();
    let current = saved;
    fixture.procedures.reporting.getCurrentBasDraft
      .mockImplementationOnce(() => deferred.promise)
      .mockImplementation(async () =>
        getCodec.encodeResponse(create(GetCurrentBasDraftResponseSchema, { workpaper: current })),
      );
    fixture.procedures.reporting.createBasDraft.mockImplementation(async (frame: Uint8Array) => {
      const request = createCodec.decodeRequest(frame);
      expect(request.periodStart).toMatchObject({ year: 2026, month: 10, day: 1 });
      expect(request.periodEnd).toMatchObject({ year: 2026, month: 12, day: 31 });
      current = create(BasWorkpaperSchema, {
        ...saved,
        id: "018f0000-0000-7000-8000-000000000402",
        periodStart: request.periodStart,
        periodEnd: request.periodEnd,
        salesG1: create(MoneySchema, { currencyCode: "AUD", minorUnits: 0n }),
      });
      return createCodec.encodeResponse(
        create(CreateBasDraftResponseSchema, { workpaper: current }),
      );
    });
    fixture.procedures.reporting.getReportingCapability.mockImplementation(
      async (frame: Uint8Array) => {
        const request = reportingCodec.decodeRequest(frame);
        return reportingCodec.encodeResponse(
          create(GetReportingCapabilityResponseSchema, {
            capability: {
              report: request.report,
              taxYear: request.taxYear,
              entityType: request.entityType,
              status: ReportingCapabilityStatus.AVAILABLE,
              appVersion: "test",
              summary: "Local workpaper",
            },
          }),
        );
      },
    );
    render(
      <BasScreen
        api={fixture.conveyor.reporting}
        now={new Date(2026, 9, 2)}
        workspace={{
          workspaceId: "018f0000-0000-7000-8000-000000000001",
          userId: "018f0000-0000-7000-8000-000000000002",
          sessionId: "018f0000-0000-7000-8000-000000000003",
          organisationId,
        }}
      />,
    );
    const user = userEvent.setup();
    const start = screen.getByLabelText("Period start");
    const end = screen.getByLabelText("Period end");
    expect(start).toHaveProperty("value", "2026-10-01");
    expect(end).toHaveProperty("value", "2026-12-31");
    await user.click(screen.getByLabelText(label));
    await act(async () =>
      deferred.resolve(
        getCodec.encodeResponse(create(GetCurrentBasDraftResponseSchema, { workpaper: saved })),
      ),
    );
    expect(await screen.findByText("Saved reporting period: 01/07/2026 – 30/09/2026")).toBeTruthy();
    expect(start).toHaveProperty("value", "2026-10-01");
    expect(end).toHaveProperty("value", "2026-12-31");
    expect(screen.getByTestId("bas-sales-g1").textContent).toContain("$110.00");
    await user.click(screen.getByRole("button", { name: "Regenerate local draft" }));
    expect(await screen.findByText("Saved reporting period: 01/10/2026 – 31/12/2026")).toBeTruthy();
    expect(screen.getByTestId("bas-sales-g1").textContent).toContain("$0.00");
    expect(fixture.procedures.reporting.createBasDraft).toHaveBeenCalledOnce();
  },
);
