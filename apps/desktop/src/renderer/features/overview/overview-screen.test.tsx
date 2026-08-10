import { create } from "@bufbuild/protobuf";
import { SourceRefSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  AttentionItemKind,
  AttentionItemSchema,
  AttentionRevisionVectorSchema,
  BasAttentionStatus,
  GetAttentionSummaryRequestSchema,
  GetAttentionSummaryResponseSchema,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";
import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { OverviewScreen } from "./overview-screen";

const codec = createProtoMethodCodec({
  input: GetAttentionSummaryRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 65_536,
  output: GetAttentionSummaryResponseSchema,
});

it("renders the live local attention summary through the named protobuf method", async () => {
  const getAttentionSummary = vi.fn(async (frame: Uint8Array) => {
    const request = codec.decodeRequest(frame);
    expect(request.organisationId).toBe("018f0000-0000-7000-8000-000000000004");
    expect(request.authentication?.actorUserId).toBe("018f0000-0000-7000-8000-000000000002");
    expect(request.authentication?.sessionId).toBe("018f0000-0000-7000-8000-000000000003");
    expect(request.asOfDate).toMatchObject({ year: 2026, month: 8, day: 10 });
    expect(request.reportingPeriod?.startDate).toMatchObject({ year: 2026, month: 7, day: 1 });
    expect(request.reportingPeriod?.endDate).toMatchObject({ year: 2026, month: 9, day: 30 });
    return codec.encodeResponse(
      create(GetAttentionSummaryResponseSchema, {
        documentsNeedingReview: 3,
        documentsReviewedInPeriod: 8,
        bankingLinesNeedingReconciliation: 2,
        bankingLinesUnreconciledInPeriod: 5,
        currentDraftBasWorkpapers: 1,
        basStatus: BasAttentionStatus.DRAFT_NOT_LODGED,
        attentionItems: [
          create(AttentionItemSchema, {
            kind: AttentionItemKind.DOCUMENT_REVIEW,
            resource: create(SourceRefSchema, {
              type: "document",
              id: "018f0000-0000-7000-8000-000000000010",
              revision: 1n,
              contentHash: new Uint8Array(32).fill(7),
            }),
            label: "Officeworks invoice",
          }),
        ],
        revisions: create(AttentionRevisionVectorSchema, { financialRevision: 9n }),
        asOfDate: request.asOfDate,
        reportingPeriod: request.reportingPeriod,
      }),
    );
  });
  const api = { getAttentionSummary } satisfies Pick<TammyDesktopAPI, "getAttentionSummary">;

  render(
    <OverviewScreen
      api={api}
      now={new Date("2026-08-10T12:00:00+10:00")}
      workspace={{
        sessionId: "018f0000-0000-7000-8000-000000000003",
        userId: "018f0000-0000-7000-8000-000000000002",
        workspaceId: "018f0000-0000-7000-8000-000000000001",
        organisationId: "018f0000-0000-7000-8000-000000000004",
      }}
    />,
  );

  expect(await screen.findByText("8 reviewed this quarter")).toBeTruthy();
  expect(screen.getByText("5 unreconciled this quarter")).toBeTruthy();
  expect(screen.getByText("Draft — not lodged")).toBeTruthy();
  expect(screen.getByText("Officeworks invoice")).toBeTruthy();
  expect(getAttentionSummary).toHaveBeenCalledOnce();
});
