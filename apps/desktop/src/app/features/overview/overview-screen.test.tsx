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
import { QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";

import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { createConveyorFixture } from "../../test/conveyor-fixtures";
import { OverviewScreen } from "./overview-screen";

const codec = createProtoMethodCodec({
  input: GetAttentionSummaryRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 65_536,
  output: GetAttentionSummaryResponseSchema,
});

const workspace = {
  sessionId: "018f0000-0000-7000-8000-000000000003",
  userId: "018f0000-0000-7000-8000-000000000002",
  workspaceId: "018f0000-0000-7000-8000-000000000001",
  organisationId: "018f0000-0000-7000-8000-000000000004",
};

it("keeps BAS action-needed status distinct from an empty additional-item list", async () => {
  const fixture = createConveyorFixture();
  fixture.procedures.reporting.getAttentionSummary.mockResolvedValue(
    codec.encodeResponse(
      create(GetAttentionSummaryResponseSchema, {
        documentsNeedingReview: 3,
        bankingLinesNeedingReconciliation: 2,
        basStatus: BasAttentionStatus.ACTION_NEEDED,
      }),
    ),
  );
  render(
    <QueryClientProvider client={fixture.queryClient}>
      <OverviewScreen api={fixture.conveyor.reporting} workspace={workspace} />
    </QueryClientProvider>,
  );
  expect(await screen.findByText("No additional review items")).toBeTruthy();
  expect(screen.getByText("Action needed · review this exact BAS preparation")).toBeTruthy();
  expect(screen.getByText("3", { exact: true })).toBeTruthy();
  expect(screen.getByText("2", { exact: true })).toBeTruthy();
  expect(screen.queryByText("Nothing needs review")).toBeNull();
  expect(screen.queryByText(/workflows are up to date/)).toBeNull();
});

it("does not display zero counts or an all-clear result when the attention projection fails", async () => {
  const fixture = createConveyorFixture();
  fixture.procedures.reporting.getAttentionSummary.mockRejectedValue(
    new Error("Synthetic summary unavailable"),
  );
  render(
    <QueryClientProvider client={fixture.queryClient}>
      <OverviewScreen api={fixture.conveyor.reporting} workspace={workspace} />
    </QueryClientProvider>,
  );
  expect(await screen.findByText("Attention summary unavailable")).toBeTruthy();
  expect(screen.queryByText("0", { exact: true })).toBeNull();
  expect(screen.queryByText("No additional review items")).toBeNull();
  expect(fixture.procedures.reporting.getAttentionSummary).toHaveBeenCalledOnce();
});

it("renders the live local attention summary through the named protobuf method", async () => {
  const fixture = createConveyorFixture();
  const preparationId = "018f0000-0000-7000-8000-000000000020";
  const preparationHash = new Uint8Array(32).fill(0x21);
  fixture.procedures.reporting.getAttentionSummary.mockImplementation(async (frame: Uint8Array) => {
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
        basPreparation: create(SourceRefSchema, {
          type: "bas_preparation",
          id: preparationId,
          revision: 3n,
          contentHash: preparationHash,
        }),
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
        ...(request.asOfDate ? { asOfDate: request.asOfDate } : {}),
        ...(request.reportingPeriod ? { reportingPeriod: request.reportingPeriod } : {}),
      }),
    );
  });
  const opened: string[] = [];
  render(
    <QueryClientProvider client={fixture.queryClient}>
      <OverviewScreen
        api={fixture.conveyor.reporting}
        onOpenBas={(path) => opened.push(path)}
        now={new Date("2026-08-10T12:00:00+10:00")}
        workspace={{
          sessionId: "018f0000-0000-7000-8000-000000000003",
          userId: "018f0000-0000-7000-8000-000000000002",
          workspaceId: "018f0000-0000-7000-8000-000000000001",
          organisationId: "018f0000-0000-7000-8000-000000000004",
        }}
      />
    </QueryClientProvider>,
  );

  expect(await screen.findByText("8 reviewed this quarter")).toBeTruthy();
  expect(screen.getByText("5 unreconciled this quarter")).toBeTruthy();
  expect(screen.getByText("Draft — not lodged")).toBeTruthy();
  expect(screen.getByText("Officeworks invoice")).toBeTruthy();
  expect(fixture.procedures.reporting.getAttentionSummary).toHaveBeenCalledOnce();
  const exactBasLink = await screen.findByRole("link", {
    name: `Open exact BAS preparation ${preparationId}`,
  });
  const expectedPath = `/gst-bas?preparation=${preparationId}&revision=3&hash=${[...preparationHash].map((byte) => byte.toString(16).padStart(2, "0")).join("")}`;
  expect(exactBasLink.getAttribute("href")).toBe(expectedPath);
  await userEvent.setup().click(exactBasLink);
  expect(opened).toEqual([expectedPath]);
});
