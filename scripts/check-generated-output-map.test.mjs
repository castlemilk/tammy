import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

async function relativeFiles(directory) {
  const entries = await readdir(directory, { recursive: true, withFileTypes: true });
  return entries
    .filter((entry) => entry.isFile())
    .map((entry) => path.relative(directory, path.join(entry.parentPath, entry.name)))
    .sort();
}

test("generation produces the exact current output map", async () => {
  assert.deepEqual(await relativeFiles("packages/connect-client/src/gen"), [
    "tammy/v1/accounting_pb.ts",
    "tammy/v1/audit_pb.ts",
    "tammy/v1/backup_pb.ts",
    "tammy/v1/banking_pb.ts",
    "tammy/v1/common_pb.ts",
    "tammy/v1/company_return_submission_pb.ts",
    "tammy/v1/company_tax_pb.ts",
    "tammy/v1/documents_pb.ts",
    "tammy/v1/events_pb.ts",
    "tammy/v1/financial_close_pb.ts",
    "tammy/v1/fixtures_pb.ts",
    "tammy/v1/identity_pb.ts",
    "tammy/v1/organisation_pb.ts",
    "tammy/v1/overview_pb.ts",
    "tammy/v1/reporting_capability_pb.ts",
    "tammy/v1/restore_pb.ts",
    "tammy/v1/sbr_pb.ts",
    "tammy/v1/system_pb.ts",
    "tammy/v1/tax_pb.ts",
    "tammy/v1/workspace_pb.ts",
  ]);
  assert.deepEqual(await relativeFiles("services/core/internal/gen"), [
    "tammy/v1/accounting.pb.go",
    "tammy/v1/audit.pb.go",
    "tammy/v1/backup.pb.go",
    "tammy/v1/banking.pb.go",
    "tammy/v1/common.pb.go",
    "tammy/v1/company_return_submission.pb.go",
    "tammy/v1/company_tax.pb.go",
    "tammy/v1/documents.pb.go",
    "tammy/v1/events.pb.go",
    "tammy/v1/financial_close.pb.go",
    "tammy/v1/fixtures.pb.go",
    "tammy/v1/identity.pb.go",
    "tammy/v1/organisation.pb.go",
    "tammy/v1/overview.pb.go",
    "tammy/v1/reporting_capability.pb.go",
    "tammy/v1/restore.pb.go",
    "tammy/v1/sbr.pb.go",
    "tammy/v1/system.pb.go",
    "tammy/v1/tammyv1connect/accounting.connect.go",
    "tammy/v1/tammyv1connect/audit.connect.go",
    "tammy/v1/tammyv1connect/banking.connect.go",
    "tammy/v1/tammyv1connect/company_return_submission.connect.go",
    "tammy/v1/tammyv1connect/company_tax.connect.go",
    "tammy/v1/tammyv1connect/documents.connect.go",
    "tammy/v1/tammyv1connect/financial_close.connect.go",
    "tammy/v1/tammyv1connect/identity.connect.go",
    "tammy/v1/tammyv1connect/organisation.connect.go",
    "tammy/v1/tammyv1connect/overview.connect.go",
    "tammy/v1/tammyv1connect/reporting_capability.connect.go",
    "tammy/v1/tammyv1connect/sbr.connect.go",
    "tammy/v1/tammyv1connect/system.connect.go",
    "tammy/v1/tammyv1connect/tax.connect.go",
    "tammy/v1/tammyv1connect/workspace.connect.go",
    "tammy/v1/tax.pb.go",
    "tammy/v1/workspace.pb.go",
  ]);

  const commonOutput = await readFile(
    "packages/connect-client/src/gen/tammy/v1/common_pb.ts",
    "utf8",
  );
  assert.match(
    commonOutput,
    /from "@buf\/bufbuild_protovalidate\.bufbuild_es\/buf\/validate\/validate_pb\.js";/,
  );
  assert.doesNotMatch(commonOutput, /from "\.\.\/\.\.\/buf\/validate\//);
});
