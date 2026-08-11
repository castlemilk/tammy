import { create } from "@bufbuild/protobuf";
import { CivilDateSchema, MoneySchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  GetReportingCapabilityRequestSchema,
  GetReportingCapabilityResponseSchema,
  ReportingCapabilitySchema,
  ReportingCapabilityStatus,
  ReportKind,
} from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import {
  BasSourceLineSchema,
  BasWorkpaperSchema,
  BasWorkpaperStatus,
  CreateBasDraftRequestSchema,
  CreateBasDraftResponseSchema,
  GetCurrentBasDraftRequestSchema,
  GetCurrentBasDraftResponseSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
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
  const api = {
    getCurrentBasDraft: vi.fn(async () =>
      getCodec.encodeResponse(create(GetCurrentBasDraftResponseSchema)),
    ),
    createBasDraft: vi.fn(async (frame: Uint8Array) => {
      const request = createCodec.decodeRequest(frame);
      expect(request.organisationId).toBe(organisationId);
      expect(request.periodStart).toMatchObject({ year: 2024, month: 4, day: 1 });
      expect(request.periodEnd).toMatchObject({ year: 2024, month: 6, day: 30 });
      return createCodec.encodeResponse(create(CreateBasDraftResponseSchema, { workpaper }));
    }),
    getReportingCapability: vi.fn(async (frame: Uint8Array) => {
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
    }),
  } satisfies Pick<
    TammyDesktopAPI,
    "createBasDraft" | "getCurrentBasDraft" | "getReportingCapability"
  >;
  const user = userEvent.setup();

  render(
    <BasScreen
      api={api}
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
  expect(api.getReportingCapability).toHaveBeenCalledTimes(2);

  await user.click(await screen.findByRole("button", { name: "Create local draft" }));
  expect(await screen.findByText("Draft — not lodged", { selector: "p" })).toBeTruthy();
  expect(screen.getByText("Officeworks Ltd")).toBeTruthy();
  expect(screen.getByRole("cell", { name: "$29.00" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: /lodge|submit|declare/i })).toBeNull();
  expect(screen.getByText(/no lodge, submit or declaration control/i)).toBeTruthy();
  expect(api.createBasDraft).toHaveBeenCalledOnce();
});

it("queries the selected AU tax year and blocks an unavailable workpaper year", async () => {
  const api = {
    getCurrentBasDraft: vi.fn(async () =>
      getCodec.encodeResponse(create(GetCurrentBasDraftResponseSchema)),
    ),
    createBasDraft: vi.fn(),
    getReportingCapability: vi.fn(async (frame: Uint8Array) => {
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
    }),
  } satisfies Pick<
    TammyDesktopAPI,
    "createBasDraft" | "getCurrentBasDraft" | "getReportingCapability"
  >;
  const user = userEvent.setup();

  render(
    <BasScreen
      api={api}
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
    const requests = api.getReportingCapability.mock.calls.map(([frame]) =>
      reportingCodec.decodeRequest(frame),
    );
    expect(requests.slice(-2).map((request) => request.taxYear)).toEqual([2025, 2025]);
  });
  expect(await screen.findAllByText("Unsupported in this build")).toHaveLength(2);
  expect(screen.queryByText("Available in this build")).toBeNull();
  const createButton = screen.getByRole("button", { name: "Create local draft" });
  expect((createButton as HTMLButtonElement).disabled).toBe(true);
  await user.click(createButton);
  expect(api.createBasDraft).not.toHaveBeenCalled();
});
