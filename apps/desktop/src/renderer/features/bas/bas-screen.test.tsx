import { create } from "@bufbuild/protobuf";
import { CivilDateSchema, MoneySchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  BasSourceLineSchema,
  BasWorkpaperSchema,
  BasWorkpaperStatus,
  CreateBasDraftRequestSchema,
  CreateBasDraftResponseSchema,
  GetCurrentBasDraftRequestSchema,
  GetCurrentBasDraftResponseSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { render, screen } from "@testing-library/react";
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
  } satisfies Pick<TammyDesktopAPI, "createBasDraft" | "getCurrentBasDraft">;
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

  await user.click(await screen.findByRole("button", { name: "Create local draft" }));
  expect(await screen.findByText("Draft — not lodged", { selector: "p" })).toBeTruthy();
  expect(screen.getByText("Officeworks Ltd")).toBeTruthy();
  expect(screen.getByRole("cell", { name: "$29.00" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: /lodge|submit|declare/i })).toBeNull();
  expect(api.createBasDraft).toHaveBeenCalledOnce();
});
