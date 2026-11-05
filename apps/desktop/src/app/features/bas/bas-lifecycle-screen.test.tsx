import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import { createConveyorFixture } from "../../test/conveyor-fixtures";
import { BasLifecycleScreen } from "./bas-lifecycle-screen";

vi.mock("./bas-lifecycle", () => ({
  BasPreparationLifecycle: () => (
    <section aria-label="BAS lifecycle stages">
      <button type="button">Prepare</button>
    </section>
  ),
}));

vi.mock("./bas-screen", () => ({
  BasScreen: ({
    purchaseReviewOnly,
    embedded,
  }: {
    readonly purchaseReviewOnly?: boolean;
    readonly embedded?: boolean;
  }) => (
    <section
      data-purchase-review-only={purchaseReviewOnly ? "true" : "false"}
      data-embedded={embedded ? "true" : "false"}
      data-testid="legacy-bas-screen"
    >
      <button type="button">Regenerate local draft</button>
      <button type="button">Export CSV</button>
      <button type="button">Review purchase source</button>
    </section>
  ),
}));

vi.mock("./business-lodger-role-control", () => ({
  BASLodgerRoleControl: () => <section aria-label="BAS approval authority" />,
}));

it("keeps the lifecycle primary and retains full legacy workpaper controls in a collapsed panel", async () => {
  const fixture = createConveyorFixture();
  render(
    <BasLifecycleScreen
      api={fixture.conveyor.reporting}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      identityApi={fixture.conveyor.organisation}
      lifecycleApi={fixture.conveyor.basPreparation}
      organisationApi={fixture.conveyor.organisation}
      purchaseApi={fixture.conveyor.purchases}
      reportApi={fixture.conveyor.reporting}
      roleControlApi={fixture.conveyor.organisation}
      workspace={undefined}
    />,
  );

  expect(screen.getByRole("region", { name: "BAS lifecycle stages" })).toBeTruthy();
  expect(screen.getAllByRole("region", { name: "BAS approval authority" })).toHaveLength(1);
  expect(
    screen
      .getByRole("region", { name: "BAS lifecycle stages" })
      .compareDocumentPosition(screen.getByRole("region", { name: "BAS approval authority" })) &
      Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  const details = screen
    .getByText("Saved workpapers and exports", { exact: true })
    .closest("details");
  expect(details).not.toBeNull();
  expect(details?.open).toBe(false);
  const legacyScreen = screen.getByTestId("legacy-bas-screen");
  expect(legacyScreen.getAttribute("data-purchase-review-only")).toBe("false");
  expect(legacyScreen.getAttribute("data-embedded")).toBe("true");
  const user = userEvent.setup();
  await user.click(screen.getByText("Saved workpapers and exports", { exact: true }));
  expect(details?.open).toBe(true);
  expect(within(legacyScreen).getByRole("button", { name: "Regenerate local draft" })).toBeTruthy();
  expect(within(legacyScreen).getByRole("button", { name: "Export CSV" })).toBeTruthy();
  expect(within(legacyScreen).getByRole("button", { name: "Review purchase source" })).toBeTruthy();
});
