import { expect, test } from "./fixtures";
import { setupAndRunCurrentAccountingWorkflow } from "./support/current-accounting-workflow";

test("runs the current accounting workflows through the packaged app", async ({
  electronHarness,
}) => {
  await setupAndRunCurrentAccountingWorkflow(electronHarness.page, electronHarness);
  expect(electronHarness.consoleErrors).toEqual([]);
  expect(electronHarness.pageErrors).toEqual([]);
});
