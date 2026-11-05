import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { create } from "@bufbuild/protobuf";
import { expect, type Page } from "@playwright/test";
import {
  AccountType,
  CreateAccountRequestSchema,
  CreateAccountResponseSchema,
  GetTrialBalanceRequestSchema,
  GetTrialBalanceResponseSchema,
  NormalBalance,
  PostManualJournalRequestSchema,
  PostManualJournalResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  AuthenticationContextSchema,
  CivilDateSchema,
  MoneySchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import { createProtoMethodCodec } from "../../../src/shared/proto-ipc";
import { invokeConveyorFrame } from "./conveyor";
import type { AuthenticatedAccountingWorkspace } from "./current-accounting-workflow";

export const trialBalanceCapturePhase = process.env.TAMMY_TRIAL_BALANCE_CAPTURE;
if (
  trialBalanceCapturePhase !== undefined &&
  !["before", "after"].includes(trialBalanceCapturePhase)
)
  throw new Error("TRIAL_BALANCE_CAPTURE_PHASE_INVALID");
const balanceCodec = createProtoMethodCodec({
  input: GetTrialBalanceRequestSchema,
  output: GetTrialBalanceResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 524288,
});
const accountCodec = createProtoMethodCodec({
  input: CreateAccountRequestSchema,
  output: CreateAccountResponseSchema,
  maximumRequestBytes: 32768,
  maximumResponseBytes: 32768,
});
const journalCodec = createProtoMethodCodec({
  input: PostManualJournalRequestSchema,
  output: PostManualJournalResponseSchema,
  maximumRequestBytes: 131072,
  maximumResponseBytes: 262144,
});
const fixtureCodes = ["1299-TB", "2299-TB"];
const fixtureNames = [
  "Synthetic studio equipment and long-term production tools",
  "Synthetic equipment finance",
];
const money = (minorUnits: bigint) => create(MoneySchema, { currencyCode: "AUD", minorUnits });
const date = (day: number) => create(CivilDateSchema, { year: 2024, month: 5, day });
const key = (n: number) =>
  `01900000-0000-7000-8000-${(0x7b000 + n).toString(16).padStart(12, "0")}`;
const label = (minor: bigint) =>
  `$${(minor / 100n).toLocaleString("en-AU")}.${(minor % 100n).toString().padStart(2, "0")}`;

export async function runTrialBalanceJourney(
  page: Page,
  workspace: AuthenticatedAccountingWorkspace,
) {
  const authentication = create(AuthenticationContextSchema, {
    actorUserId: workspace.userId,
    sessionId: workspace.sessionId,
  });
  const read = async (day = 12) =>
    balanceCodec.decodeResponse(
      await invokeConveyorFrame(
        page,
        "accounting",
        "getTrialBalance",
        balanceCodec.encodeRequest(
          create(GetTrialBalanceRequestSchema, {
            authentication,
            organisationId: workspace.organisationId,
            asOfDate: date(day),
          }),
        ),
      ),
    );
  const original = await read();
  const ids: string[] = [];
  for (const [index, type] of [AccountType.ASSET, AccountType.LIABILITY].entries()) {
    const code = fixtureCodes[index];
    const name = fixtureNames[index];
    if (!code || !name) throw new Error("TRIAL_BALANCE_FIXTURE_INVALID");
    const response = accountCodec.decodeResponse(
      await invokeConveyorFrame(
        page,
        "accounting",
        "createAccount",
        accountCodec.encodeRequest(
          create(CreateAccountRequestSchema, {
            commandContext: { authentication, idempotencyKey: key(index) },
            organisationId: workspace.organisationId,
            code,
            name,
            type,
            normalBalance: index === 0 ? NormalBalance.DEBIT : NormalBalance.CREDIT,
            reportClassification: "balance_sheet.manual",
            cashFlowClassification: "noncash",
          }),
        ),
      ),
    );
    if (!response.account) throw new Error("TRIAL_BALANCE_FIXTURE_ACCOUNT_MISSING");
    ids.push(response.account.id);
  }
  const [assetId, liabilityId] = ids;
  if (!assetId || !liabilityId) throw new Error("TRIAL_BALANCE_FIXTURE_ACCOUNT_MISSING");
  const post = async (index: number, day: number, amount: bigint) => {
    const response = journalCodec.decodeResponse(
      await invokeConveyorFrame(
        page,
        "accounting",
        "postManualJournal",
        journalCodec.encodeRequest(
          create(PostManualJournalRequestSchema, {
            commandContext: { authentication, idempotencyKey: key(10 + index) },
            organisationId: workspace.organisationId,
            postingDate: date(day),
            memo: `Synthetic Trial balance boundary ${day} / ${index}`,
            lines: [
              {
                clientLineId: key(100 + index * 2),
                accountId: assetId,
                debit: money(amount),
                credit: money(0n),
                description: "Synthetic equipment",
              },
              {
                clientLineId: key(101 + index * 2),
                accountId: liabilityId,
                debit: money(0n),
                credit: money(amount),
                description: "Synthetic finance",
              },
            ],
          }),
        ),
      ),
    );
    expect(response.journal).toBeDefined();
  };
  await post(0, 11, 200000n);
  await post(1, 12, 100000n);
  await post(2, 13, 77777n);
  const snapshot = await read();
  expect(snapshot.totalDebits?.minorUnits).toBe((original.totalDebits?.minorUnits ?? 0n) + 300000n);
  expect(snapshot.totalCredits?.minorUnits).toBe(snapshot.totalDebits?.minorUnits);
  expect((await read(13)).totalDebits?.minorUnits).toBe(
    (snapshot.totalDebits?.minorUnits ?? 0n) + 77777n,
  );
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.getByRole("link", { name: "Trial balance", exact: true }).click();
  const region = page.getByRole("region", { name: "Account balances table", exact: true });
  await expect(region.getByText(fixtureNames[0] ?? "", { exact: true })).toBeVisible();
  const verify = async (value = snapshot) => {
    for (const line of value.lines) {
      const row = region.getByRole("row").filter({ hasText: line.name });
      await expect(row).toContainText(label(line.debits?.minorUnits ?? 0n));
      await expect(row).toContainText(label(line.credits?.minorUnits ?? 0n));
    }
    const total = region.locator("tfoot");
    await expect(total).toContainText(label(value.totalDebits?.minorUnits ?? 0n));
    await expect(total).toContainText(label(value.totalCredits?.minorUnits ?? 0n));
  };
  await verify();
  await capture(page, "01-trial-balance");
  if (trialBalanceCapturePhase !== "before") {
    await expect(page.getByText("Debits equal credits", { exact: true })).toBeVisible();
    const refresh = page.getByRole("button", { name: "Refresh trial balance", exact: true });
    await refresh.dblclick();
    await expect(refresh).toBeEnabled();
    await verify();
    expect(await read()).toEqual(snapshot);
    await capture(page, "02-confirmed-refresh");
  }
  await page.emulateMedia({ reducedMotion: "reduce" });
  for (const width of [900, 390, 320]) {
    await page.setViewportSize({ width, height: 1000 });
    await region.evaluate((e) => {
      e.scrollLeft = 0;
    });
    await region.focus();
    await page.keyboard.press("Shift+Tab");
    await page.keyboard.press("Tab");
    await expect(region).toBeFocused();
    await expect(region).toHaveCSS("outline-style", "solid");
    if (trialBalanceCapturePhase !== "before")
      await expect(region).toHaveCSS("outline-offset", "-2px");
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
    await capture(page, `03-report-focus-${width}`, true);
    if (width < 900) {
      await page.keyboard.press("ArrowRight");
      await expect.poll(() => region.evaluate((e) => e.scrollLeft)).toBeGreaterThan(0);
      await region.evaluate((e) => {
        e.scrollLeft = e.scrollWidth;
      });
      await capture(page, `04-credit-and-total-${width}`, true);
    }
  }
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.getByRole("link", { name: "Chart of accounts", exact: true }).click();
  await page.goBack();
  await expect(page.getByRole("heading", { name: "Trial balance", exact: true })).toBeVisible();
  await verify();
  await page.goForward();
  await expect(page.getByRole("heading", { name: "Chart of accounts", exact: true })).toBeVisible();
  expect(await read()).toEqual(snapshot);
  await page.getByRole("link", { name: "Trial balance", exact: true }).click();
  await verify();
  await capture(page, "05-returned-report");
  if (trialBalanceCapturePhase !== "before") {
    const amount = 9007199254740991n;
    await post(3, 12, amount);
    const large = await read();
    expect(large.totalDebits?.minorUnits).toBe((snapshot.totalDebits?.minorUnits ?? 0n) + amount);
    await page.getByRole("button", { name: "Refresh trial balance", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Refresh trial balance", exact: true }),
    ).toBeEnabled();
    await verify(large);
    await capture(page, "06-exact-large-amounts");
    await page.setViewportSize({ width: 320, height: 1000 });
    await region.focus();
    await page.keyboard.press("Shift+Tab");
    await page.keyboard.press("Tab");
    await expect(region).toBeFocused();
    await page.keyboard.press("ArrowRight");
    await expect.poll(() => region.evaluate((e) => e.scrollLeft)).toBeGreaterThan(0);
    await region.evaluate((e) => {
      e.scrollLeft = e.scrollWidth;
    });
    const total = region.locator("tfoot td").last();
    await expect(total).toBeInViewport({ ratio: 1 });
    await expect(total).toHaveText(label(large.totalCredits?.minorUnits ?? 0n));
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
    await capture(page, "07-large-credit-focus-320", true);
    expect(await read()).toEqual(large);
  }
}

async function capture(page: Page, name: string, focused = false) {
  if (!trialBalanceCapturePhase) return;
  const directory = path.resolve(
    import.meta.dirname,
    "../../../../../output/playwright/soft-precision-trial-balance-2026-10-23",
    trialBalanceCapturePhase,
  );
  await mkdir(directory, { recursive: true });
  if (!focused)
    await page.getByRole("heading", { level: 1 }).evaluate((heading) => {
      for (let node = heading.parentElement; node; node = node.parentElement)
        node.scrollTo({ top: 0, left: 0, behavior: "instant" });
      document.scrollingElement?.scrollTo({ top: 0, left: 0, behavior: "instant" });
    });
  await writeFile(
    path.join(directory, `${name}.png`),
    await page.screenshot({ animations: "disabled" }),
    { flag: "wx" },
  );
}
