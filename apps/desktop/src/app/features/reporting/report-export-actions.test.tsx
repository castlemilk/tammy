import { create } from "@bufbuild/protobuf";
import {
  GetOrganisationRequestSchema,
  GetOrganisationResponseSchema,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { createConveyorFixture, renderWithConveyor } from "../../test/conveyor-fixtures";
import { ReportExportActions } from "./report-export-actions";

const workspace = {
  organisationId: "018f0000-0000-7000-8000-000000000004",
  userId: "018f0000-0000-7000-8000-000000000002",
  sessionId: "018f0000-0000-7000-8000-000000000003",
  workspaceId: "018f0000-0000-7000-8000-000000000001",
};
const codec = createProtoMethodCodec({
  input: GetOrganisationRequestSchema,
  output: GetOrganisationResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 32768,
});
function fixture(legalName = "Trusted Organisation") {
  const f = createConveyorFixture();
  f.procedures.organisation.getOrganisation.mockResolvedValue(
    codec.encodeResponse(
      create(GetOrganisationResponseSchema, {
        organisation: {
          id: workspace.organisationId,
          version: 3n,
          legalName,
          displayName: "Trusted",
          abn: "51824753556",
        },
      }),
    ),
  );
  return f;
}
it.each([false, true])(
  "returns completed export focus only when the user has not moved elsewhere (moved: %s)",
  async (movedElsewhere) => {
    const f = fixture();
    let finish: ((result: { saved: boolean }) => void) | undefined;
    const execute = vi.fn(
      () =>
        new Promise<{ saved: boolean }>((resolve) => {
          finish = resolve;
        }),
    );
    renderWithConveyor(
      <>
        <input aria-label="Other work" />
        <ReportExportActions
          organisationApi={f.conveyor.organisation}
          workspace={workspace}
          label="Income"
          disabled={false}
          execute={execute}
        />
      </>,
    );
    const trigger = screen.getByRole("button", { name: "Export CSV" });
    await waitFor(() => expect(trigger).toHaveProperty("disabled", false));
    trigger.focus();
    fireEvent.click(trigger);
    await waitFor(() => expect(trigger).toHaveProperty("disabled", true));
    // Native Chromium blurs a button disabled for an in-progress export.
    const other = screen.getByRole("textbox", { name: "Other work" });
    other.focus();
    if (!movedElsewhere) other.blur();
    expect(document.activeElement).toBe(movedElsewhere ? other : document.body);
    if (!finish) throw new Error("Export did not start");
    await act(async () => finish?.({ saved: false }));
    await screen.findByText("Income export cancelled.");
    await waitFor(() => expect(document.activeElement).toBe(movedElsewhere ? other : trigger));
    expect(execute).toHaveBeenCalledOnce();
  },
);
it("loads displayed authoritative profile version and exports only bounded identity/dates while retaining cancellation", async () => {
  const f = fixture(),
    execute = vi.fn(async () => ({ saved: false }));
  renderWithConveyor(
    <ReportExportActions
      organisationApi={f.conveyor.organisation}
      workspace={workspace}
      label="Income"
      disabled={false}
      execute={execute}
    />,
  );
  expect(await screen.findByText(/Trusted Organisation/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Export CSV" }));
  await waitFor(() => expect(execute).toHaveBeenCalledWith("csv", "3"));
  expect(await screen.findByText("Income export cancelled.")).toBeTruthy();
});
it("exports CSV for the full 256 Unicode code point legal name and reports unsupported PDF glyphs on export", async () => {
  const legalName = "𐀀".repeat(256),
    f = fixture(legalName);
  const execute = vi.fn(async (format: "pdf" | "csv") => {
    if (format === "pdf") throw new Error("REPORT_PDF_UNSUPPORTED_TEXT");
    return { saved: true };
  });
  renderWithConveyor(
    <ReportExportActions
      organisationApi={f.conveyor.organisation}
      workspace={workspace}
      label="Income"
      disabled={false}
      execute={execute}
    />,
  );
  await screen.findByText(`${legalName} · ABN 51824753556`);
  expect(screen.getByRole("button", { name: "Export CSV" })).toHaveProperty("disabled", false);
  fireEvent.click(screen.getByRole("button", { name: "Export CSV" }));
  expect(await screen.findByText("Income CSV exported.")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Export PDF" }));
  expect(
    await screen.findByText(
      "The PDF exporter cannot preserve some report text. Export CSV to preserve all characters.",
    ),
  ).toBeTruthy();
  expect(execute.mock.calls).toEqual([
    ["csv", "3"],
    ["pdf", "3"],
  ]);
});
it("blocks actions while reports are loading and offers bounded render/refresh guidance", async () => {
  const f = fixture(),
    execute = vi.fn(async () => {
      throw new Error("REPORT_PDF_UNSUPPORTED_TEXT");
    });
  const view = renderWithConveyor(
    <ReportExportActions
      organisationApi={f.conveyor.organisation}
      workspace={workspace}
      label="BAS"
      disabled={true}
      execute={execute}
    />,
  );
  await screen.findByText(/Trusted Organisation/);
  expect(screen.getByRole("button", { name: "Export PDF" })).toHaveProperty("disabled", true);
  view.rerender(
    <ReportExportActions
      organisationApi={f.conveyor.organisation}
      workspace={workspace}
      label="BAS"
      disabled={false}
      execute={execute}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Export PDF" }));
  expect(await screen.findByText(/Export CSV to preserve all characters/)).toBeTruthy();
});
it("does not display a success from an export belonging to a previous session", async () => {
  const f = fixture();
  let finish: ((value: { saved: boolean }) => void) | undefined;
  const execute = vi.fn(
    () =>
      new Promise<{ saved: boolean }>((resolve) => {
        finish = resolve;
      }),
  );
  const view = renderWithConveyor(
    <ReportExportActions
      organisationApi={f.conveyor.organisation}
      workspace={workspace}
      label="Income"
      disabled={false}
      execute={execute}
    />,
  );
  await screen.findByText(/Trusted Organisation/);
  fireEvent.click(screen.getByRole("button", { name: "Export CSV" }));
  await waitFor(() => expect(execute).toHaveBeenCalledOnce());
  view.rerender(
    <ReportExportActions
      organisationApi={f.conveyor.organisation}
      workspace={{ ...workspace, sessionId: "018f0000-0000-7000-8000-000000000009" }}
      label="Income"
      disabled={false}
      execute={execute}
    />,
  );
  finish?.({ saved: true });
  await waitFor(() => expect(screen.queryByText("Income CSV exported.")).toBeNull());
});
