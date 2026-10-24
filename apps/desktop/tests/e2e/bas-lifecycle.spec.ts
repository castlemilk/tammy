import { createHash } from "node:crypto";
import { create, toBinary } from "@bufbuild/protobuf";
import type { Page } from "@playwright/test";
import {
  AccountStatus,
  ListAccountsRequestSchema,
  ListAccountsResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import { PageRequestSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  DocumentCandidateSchema,
  ListDocumentsRequestSchema,
  ListDocumentsResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import { expect, test } from "./fixtures";
import { captureBas, reviewBasAtNarrowWidths } from "./support/bas-journey-capture";
import {
  authenticatedWorkspace,
  setupAndRunCurrentAccountingWorkflow,
} from "./support/current-accounting-workflow";
import {
  purchaseAuth,
  purchaseProto,
  retainPurchaseThroughUi,
} from "./support/purchase-payments-workflow";

async function expectLifecycleStatus(page: Page, message: string) {
  const lifecycle = page.getByRole("region", {
    name: "BAS preparation lifecycle",
    exact: true,
  });
  await expect(lifecycle.getByRole("status").filter({ hasText: message })).toBeVisible();
}

test("prepares and reviews an exact BAS period in the packaged app", async ({
  electronHarness,
}) => {
  test.setTimeout(240_000);
  const workspace = await setupAndRunCurrentAccountingWorkflow(
    electronHarness.page,
    electronHarness,
  );
  const page = electronHarness.currentPage();
  await page.setViewportSize({ width: 1440, height: 1100 });
  const activeWorkspace = await authenticatedWorkspace(page);
  expect({
    workspaceId: activeWorkspace.workspaceId,
    userId: activeWorkspace.userId,
    sessionId: activeWorkspace.sessionId,
    organisationId: activeWorkspace.organisationId,
  }).toEqual(workspace);
  await purchaseProto(
    page,
    "documents",
    "listDocuments",
    ListDocumentsRequestSchema,
    ListDocumentsResponseSchema,
    create(ListDocumentsRequestSchema, {
      authentication: purchaseAuth(workspace),
      organisationId: workspace.organisationId,
      page: create(PageRequestSchema, { pageSize: 200 }),
    }),
  );
  await purchaseProto(
    page,
    "accounting",
    "listAccounts",
    ListAccountsRequestSchema,
    ListAccountsResponseSchema,
    create(ListAccountsRequestSchema, {
      authentication: purchaseAuth(workspace),
      organisationId: workspace.organisationId,
      status: AccountStatus.ACTIVE,
      page: create(PageRequestSchema, { pageSize: 200 }),
    }),
  );
  await page.evaluate(() => window.history.replaceState(null, "", "/"));
  await page.reload();
  const refreshedWorkspace = await authenticatedWorkspace(page);
  expect({
    workspaceId: refreshedWorkspace.workspaceId,
    userId: refreshedWorkspace.userId,
    sessionId: refreshedWorkspace.sessionId,
    organisationId: refreshedWorkspace.organisationId,
  }).toEqual(workspace);

  const profileEvidenceDocument = await retainPurchaseThroughUi(
    page,
    workspace,
    "BAS-LIFECYCLE-PROFILE-EVIDENCE",
    "2026-07-01",
    true,
    false,
    "gst-registration-evidence",
  );
  if (
    !profileEvidenceDocument.candidate ||
    !profileEvidenceDocument.id ||
    profileEvidenceDocument.version < 1n
  ) {
    throw new Error("BAS_LIFECYCLE_RETAINED_PROFILE_EVIDENCE_MISSING");
  }
  const profileEvidence = {
    type: "document",
    id: profileEvidenceDocument.id,
    revision: profileEvidenceDocument.version.toString(),
    hash: createHash("sha256")
      .update(toBinary(DocumentCandidateSchema, profileEvidenceDocument.candidate))
      .digest("hex"),
  };

  await page.getByRole("link", { name: "GST & BAS", exact: true }).click();
  await expect(page.getByRole("heading", { name: "BAS preparation", exact: true })).toBeVisible();
  await expect(
    page.getByText("Workspace admin · prepare and read only", { exact: true }),
  ).toBeVisible();

  const lifecycle = page.getByRole("navigation", { name: "BAS lifecycle stages" });
  await captureBas(page, "01-prepare-arrival");
  for (const stage of ["Prepare", "Review", "Freeze", "Export", "External record"] as const) {
    await expect(lifecycle.getByRole("button", { name: stage, exact: true })).toBeVisible();
  }

  const profileSection = page
    .getByRole("heading", { name: "Obligations profile", exact: true })
    .locator("xpath=ancestor::section[1]");
  await profileSection.getByLabel("Effective from", { exact: true }).fill("2026-07-01");
  for (const [label, value] of [
    ["GST obligation", "active"],
    ["GST registration status", "registered"],
    ["GST reporting method", "simpler"],
    ["GST turnover category", "small"],
    ["GST basis", "accrual"],
    ["GST cadence", "quarterly"],
    ["PAYGI obligation", "not applicable"],
    ["PAYGI method", "not applicable"],
  ] as const) {
    await profileSection.getByRole("combobox", { name: label, exact: true }).selectOption({
      label: value,
    });
  }
  await profileSection
    .getByLabel("GST registration effective from", { exact: true })
    .fill("2025-07-01");
  await profileSection
    .getByLabel("GST decision reason", { exact: true })
    .fill("Synthetic retained registration evidence supports GST reporting for this period.");
  await profileSection
    .getByLabel("PAYGI reason", { exact: true })
    .fill("The retained synthetic PAYGI notice confirms that no instalment applies.");
  const evidenceGroup = page.getByRole("group", {
    name: "Shared retained profile evidence identity",
    exact: true,
  });
  await evidenceGroup.getByLabel("Source type", { exact: true }).fill(profileEvidence.type);
  await evidenceGroup.getByLabel("Source UUID", { exact: true }).fill(profileEvidence.id);
  await evidenceGroup.getByLabel("Revision", { exact: true }).fill(profileEvidence.revision);
  await evidenceGroup.getByLabel("SHA-256 hex", { exact: true }).fill(profileEvidence.hash);
  await page
    .getByLabel("Profile decision reason", { exact: true })
    .fill(
      "Synthetic retained evidence supports GST and the PAYGI not-applicable decision; other obligations remain unknown.",
    );
  await page.getByRole("button", { name: "Save immutable profile revision" }).click();
  await expectLifecycleStatus(page, "Saved BAS obligations profile revision");
  await captureBas(page, "02-profile-saved");

  const lifecycleRegion = page.getByRole("region", {
    name: "BAS preparation lifecycle",
    exact: true,
  });
  const periodSelect = lifecycleRegion.getByRole("combobox", {
    name: "Confirmed period history",
    exact: true,
  });
  await expect
    .poll(async () =>
      (await periodSelect.locator("option").allTextContents()).some((label) =>
        label.startsWith("Suggestion ·"),
      ),
    )
    .toBe(true);
  const options = await periodSelect.locator("option").evaluateAll((items) =>
    items.map((item) => ({
      value: (item as HTMLOptionElement).value,
      text: item.textContent ?? "",
    })),
  );
  const q3 = options.find(
    (item) =>
      item.text.startsWith("Suggestion ·") &&
      item.text.includes("2026-07-01") &&
      item.text.includes("2026-09-30"),
  );
  if (!q3) throw new Error("BAS_LIFECYCLE_Q3_SUGGESTION_MISSING");
  await periodSelect.selectOption(q3.value);
  await page
    .getByLabel("Period confirmation reason", { exact: true })
    .fill(
      "The displayed Q3 2026 calendar period is confirmed; other statement obligations and due-date evidence remain unresolved.",
    );
  await page.getByRole("button", { name: "Confirm selected period" }).click();
  await expectLifecycleStatus(page, "Confirmed BAS period");

  await page.getByRole("button", { name: "Regenerate exact calculation" }).click();
  await expectLifecycleStatus(page, "Prepared exact BAS version");
  await expect(page.getByRole("button", { name: "Save exact PAYG inputs" })).toBeVisible();
  await page
    .getByLabel("PAYG review reason", { exact: true })
    .fill(
      "No verified payroll or instalment amount entered; each unresolved label remains unknown.",
    );
  await page.getByRole("button", { name: "Save exact PAYG inputs" }).click();
  await expectLifecycleStatus(page, "Saved PAYG inputs against the displayed preparation revision");
  // PAYG input changes create a new immutable preparation with no calculation pointer.
  await page.getByRole("button", { name: "Regenerate exact calculation" }).click();
  await expectLifecycleStatus(page, "Prepared exact BAS version");

  await lifecycle.getByRole("button", { name: "Review", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Exact review snapshot", exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("table", { name: "Exact BAS calculation labels" })).toBeVisible();
  await captureBas(page, "03-exact-review");
  await page.getByRole("button", { name: "Run validation on this exact calculation" }).click();
  await expectLifecycleStatus(page, "Created review validation");
  await expect(page.getByRole("heading", { name: "Review findings", exact: true })).toBeVisible();
  await captureBas(page, "04-validation-findings");
  await reviewBasAtNarrowWidths(page);

  await lifecycle.getByRole("button", { name: "Freeze", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Freeze for manual handoff", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText(
      "Workspace admins can prepare and read BAS records, but cannot use approval actions.",
    ),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Approve and freeze exact snapshot" }),
  ).toBeDisabled();
  await captureBas(page, "07-freeze-permission-boundary");

  await lifecycle.getByRole("button", { name: "Export", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Export an exact retained BAS version", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("region", { name: "BAS preparation lifecycle", exact: true })
    .getByRole("combobox", { name: "Exact report source", exact: true })
    .selectOption({ label: "Retained calculation" });
  const exportButton = page.getByRole("button", { name: "Export PDF", exact: true });
  await expect(exportButton).toBeEnabled();
  await captureBas(page, "08-retained-calculation-export");
  await electronHarness.application.evaluate(({ dialog }) => {
    dialog.showSaveDialog = async () => ({ canceled: true, filePath: "" });
  });
  await exportButton.click();
  await expect(
    page
      .getByRole("region", { name: "BAS preparation lifecycle", exact: true })
      .getByRole("status")
      .filter({ hasText: "BAS export cancelled." }),
  ).toHaveText("BAS export cancelled.");
  await captureBas(page, "09-export-cancelled");

  await lifecycle.getByRole("button", { name: "External record", exact: true }).click();
  await expect(
    page.getByRole("heading", {
      name: "Record what the user says happened outside Tammy",
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Record user-reported event as unverified" }),
  ).toBeDisabled();
  await expect(
    page.getByText(/Tammy cannot confirm that the ATO received, processed or accepted anything/),
  ).toBeVisible();

  await captureBas(page, "10-unverified-external-record");
  expect(electronHarness.consoleErrors).toEqual([]);
  expect(electronHarness.pageErrors).toEqual([]);
});
