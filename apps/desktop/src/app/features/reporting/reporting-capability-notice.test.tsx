import { create } from "@bufbuild/protobuf";
import {
  GetReportingCapabilityRequestSchema,
  GetReportingCapabilityResponseSchema,
  ReportingCapabilitySchema,
  ReportingCapabilityStatus,
  ReportingEntityType,
  ReportKind,
} from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { ReportingCapabilityNotice } from "./reporting-capability-notice";

const codec = createProtoMethodCodec({
  input: GetReportingCapabilityRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetReportingCapabilityResponseSchema,
});

it("encodes the exact capability key and renders available status from the core", async () => {
  const getReportingCapability = vi.fn(async (frame: Uint8Array) => {
    const request = codec.decodeRequest(frame);
    expect(request).toMatchObject({
      report: ReportKind.GST_WORKPAPER,
      entityType: ReportingEntityType.AU_BUSINESS,
      taxYear: 2024,
    });
    return codec.encodeResponse(
      create(GetReportingCapabilityResponseSchema, {
        capability: create(ReportingCapabilitySchema, {
          report: request.report,
          taxYear: request.taxYear,
          entityType: request.entityType,
          status: ReportingCapabilityStatus.AVAILABLE,
          appVersion: "test-core",
          summary: "Tammy supports a local reviewed-document GST workpaper only.",
        }),
      }),
    );
  });

  render(
    <ReportingCapabilityNotice
      api={{ getReportingCapability }}
      entityType={ReportingEntityType.AU_BUSINESS}
      fallbackCopy="Tammy does not prepare, declare, or lodge a complete BAS."
      report={ReportKind.GST_WORKPAPER}
      taxYear={2024}
    />,
  );

  const statusRegion = screen.getByRole("status");
  expect(statusRegion.getAttribute("aria-live")).toBe("polite");
  expect(await screen.findByText("Available in this build")).toBeTruthy();
  expect(screen.getByRole("status")).toBe(statusRegion);
  expect(
    screen.getByText("Tammy supports a local reviewed-document GST workpaper only."),
  ).toBeTruthy();
  expect(
    screen.getByText("Tammy does not prepare, declare, or lodge a complete BAS."),
  ).toBeTruthy();
  expect(getReportingCapability).toHaveBeenCalledOnce();
});

it("fails closed without exposing desktop error details", async () => {
  const getReportingCapability = vi
    .fn<TammyDesktopAPI["getReportingCapability"]>()
    .mockRejectedValue(new Error("secret database=/private/workspace.db"));

  render(
    <ReportingCapabilityNotice
      api={{ getReportingCapability }}
      entityType={ReportingEntityType.AU_BUSINESS}
      report={ReportKind.BAS}
      taxYear={2024}
    />,
  );

  expect(await screen.findByText("Reporting support is unavailable in this build.")).toBeTruthy();
  expect(document.body.textContent).not.toContain("secret");
  expect(document.body.textContent).not.toContain("workspace.db");
});
