import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { parseCoverageManifest } from "./check-e2e-coverage.mjs";
import {
  COMPANY_EOFY_DECLARED_FUTURE_RPCS,
  SBR_DECLARED_FUTURE_RPCS,
  SLICE_ONE_RPC_POLICY,
} from "./slice-one-coverage-policy.mjs";

const SYSTEM_RPC = "tammy.v1.SystemService.GetDiagnostics";
const ALL_ROLES_ALLOWED = ["workspace_admin", "business_preparer", "business_lodger", "auditor"];
const CURRENT_WORKFLOW_CASE = "walkthrough/current-accounting-workflows";
const EXPOSED_BUSINESS_RPCS = [
  "tammy.v1.WorkspaceService.CreateWorkspace",
  "tammy.v1.WorkspaceService.ConfirmRecovery",
  "tammy.v1.WorkspaceService.UnlockWorkspace",
  "tammy.v1.IdentityService.SignIn",
  "tammy.v1.OrganisationService.CreateOrganisation",
  "tammy.v1.AccountingService.CreateAccount",
  "tammy.v1.AccountingService.PostManualJournal",
  "tammy.v1.AccountingService.ListAccounts",
  "tammy.v1.AccountingService.GetJournal",
  "tammy.v1.AccountingService.ListJournals",
  "tammy.v1.AccountingService.GetTrialBalance",
  "tammy.v1.OverviewService.GetAttentionSummary",
  "tammy.v1.BankingService.ImportBankStatement",
  "tammy.v1.BankingService.ListBankStatementLines",
  "tammy.v1.BankingService.MatchBankStatementLine",
  "tammy.v1.BankingService.CompleteBankReconciliation",
  "tammy.v1.BankingService.GetBankingSummary",
  "tammy.v1.DocumentService.IngestDocument",
  "tammy.v1.DocumentService.ListDocuments",
  "tammy.v1.DocumentService.GetDocument",
  "tammy.v1.DocumentService.SaveDocumentReview",
  "tammy.v1.TaxService.CreateBasDraft",
  "tammy.v1.TaxService.GetCurrentBasDraft",
];

test("coverage declares the exact normative policy for all 118 non-system RPCs", async () => {
  const coverage = parseCoverageManifest(await readFile("test/e2e/coverage.yaml", "utf8"));
  const actualRpcNames = Object.keys(coverage.rpcs).filter((rpcName) => rpcName !== SYSTEM_RPC);

  assert.equal(Object.keys(SLICE_ONE_RPC_POLICY).length, 118);
  assert.deepEqual(actualRpcNames.sort(), Object.keys(SLICE_ONE_RPC_POLICY).sort());

  for (const [rpcName, expected] of Object.entries(SLICE_ONE_RPC_POLICY)) {
    const actual = coverage.rpcs[rpcName];
    assert.deepEqual(
      {
        preload: actual.preload,
        projections: actual.projections,
        routes: actual.routes,
        roles: actual.roles,
        principalFailures: actual.principalFailures,
        list: actual.list,
        idempotency: actual.idempotency,
      },
      expected,
      rpcName,
    );
  }
});

test("company EOFY close, return preparation, and submission catalogue exactly 30 declared-future RPCs", async () => {
  const expected = [
    "tammy.v1.FinancialCloseService.CreateFinancialClose",
    "tammy.v1.FinancialCloseService.GetFinancialClose",
    "tammy.v1.FinancialCloseService.ListCloseChecks",
    "tammy.v1.FinancialCloseService.ResolveCloseWarning",
    "tammy.v1.FinancialCloseService.FreezeFinancialClose",
    "tammy.v1.FinancialCloseService.ReopenFinancialClose",
    "tammy.v1.FinancialCloseService.StartFinancialCloseCorrection",
    "tammy.v1.FinancialCloseService.GetFinancialStatements",
    "tammy.v1.CompanyTaxService.GetCompanyTaxProfile",
    "tammy.v1.CompanyTaxService.SetCompanyTaxProfile",
    "tammy.v1.CompanyTaxService.CreateCompanyReturn",
    "tammy.v1.CompanyTaxService.GetCompanyReturn",
    "tammy.v1.CompanyTaxService.ListCompanyReturnFacts",
    "tammy.v1.CompanyTaxService.SetCompanyReturnInput",
    "tammy.v1.CompanyTaxService.UpsertTaxAdjustment",
    "tammy.v1.CompanyTaxService.RemoveTaxAdjustment",
    "tammy.v1.CompanyTaxService.UpsertTaxElection",
    "tammy.v1.CompanyTaxService.RemoveTaxElection",
    "tammy.v1.CompanyTaxService.ValidateCompanyReturn",
    "tammy.v1.CompanyTaxService.AcknowledgeReturnWarning",
    "tammy.v1.CompanyTaxService.DeclareCompanyReturn",
    "tammy.v1.CompanyTaxService.WithdrawCompanyReturnDeclaration",
    "tammy.v1.CompanyTaxService.ExportCompanyReturnPack",
    "tammy.v1.CompanyTaxService.CreateCompanyReturnReplacement",
    "tammy.v1.CompanyTaxService.CreateCompanyReturnAmendment",
    "tammy.v1.CompanyReturnSubmissionService.PreLodgeCompanyReturn",
    "tammy.v1.CompanyReturnSubmissionService.LodgeCompanyReturn",
    "tammy.v1.CompanyReturnSubmissionService.GetCompanyReturnSubmission",
    "tammy.v1.CompanyReturnSubmissionService.RefreshCompanyReturnStatus",
    "tammy.v1.CompanyReturnSubmissionService.ReconcileUnknownCompanyReturnSubmission",
  ];
  assert.deepEqual(COMPANY_EOFY_DECLARED_FUTURE_RPCS, expected);

  const coverage = parseCoverageManifest(await readFile("test/e2e/coverage.yaml", "utf8"));
  const preloadMethods = JSON.parse(
    await readFile("apps/desktop/src/shared/preload-methods.json", "utf8"),
  );
  assert.deepEqual(coverage.scenarios["E2E-18"].futureCases, [
    "company-eofy/contracts",
    "company-eofy/lifecycle",
    "company-eofy/permissions",
    "company-eofy/financial-close",
    "company-eofy/company-return",
    "company-eofy/submission",
  ]);
  assert.deepEqual(
    Object.keys(coverage.rpcs).filter((rpcName) => rpcName.startsWith("tammy.v1.FinancialCloseService.") || rpcName.startsWith("tammy.v1.CompanyTaxService.") || rpcName.startsWith("tammy.v1.CompanyReturnSubmissionService.")),
    expected,
  );
  for (const rpcName of expected) {
    const rpc = coverage.rpcs[rpcName];
    assert.equal(rpc.stage, "declared_future", rpcName);
    assert.deepEqual(rpc.cases, [], rpcName);
    const isClose = rpcName.includes("FinancialCloseService");
    const isSubmission = rpcName.includes("CompanyReturnSubmissionService");
    assert.deepEqual(rpc.futureCases, ["company-eofy/contracts", "company-eofy/permissions", isClose ? "company-eofy/financial-close" : isSubmission ? "company-eofy/submission" : "company-eofy/company-return"], rpcName);
    assert.deepEqual(rpc.routes, [isClose ? "/eofy-company-tax/close" : isSubmission ? "/eofy-company-tax/lodge" : "/eofy-company-tax/return"], rpcName);
    assert.ok(!preloadMethods.includes(rpc.preload), rpcName);
  }
});

test("packaged SBR readiness promotes all nine exposed and fail-closed RPCs", async () => {
  const proven = [
    "tammy.v1.SbrService.GetSbrReadiness",
    "tammy.v1.SbrService.ImportMachineCredential",
    "tammy.v1.SbrService.GetMachineCredentialStatus",
    "tammy.v1.SbrService.UnlockMachineCredential",
    "tammy.v1.SbrService.ReplaceMachineCredential",
    "tammy.v1.SbrService.RemoveMachineCredential",
    "tammy.v1.SbrService.ImportSbrProductId",
    "tammy.v1.SbrService.RemoveSbrProductId",
    "tammy.v1.SbrService.RunSbrReadinessFixture",
  ].sort();
  const future = [];
  assert.deepEqual([...SBR_DECLARED_FUTURE_RPCS].sort(), future);

  const coverage = parseCoverageManifest(await readFile("test/e2e/coverage.yaml", "utf8"));
  assert.deepEqual(coverage.scenarios["E2E-17"].cases, ["sbr-readiness-simulator"]);
  assert.equal(coverage.scenarios["E2E-17"].futureCases, undefined);
  assert.deepEqual(
    Object.keys(coverage.rpcs)
      .filter((rpcName) => rpcName.startsWith("tammy.v1.SbrService."))
      .sort(),
    [...proven, ...future].sort(),
  );
  for (const rpcName of proven) {
    const rpc = coverage.rpcs[rpcName];
    assert.equal(rpc.stage, "production", rpcName);
    assert.deepEqual(rpc.cases, ["sbr-readiness-simulator"], rpcName);
    assert.equal(rpc.futureCases, undefined, rpcName);
    assert.deepEqual(rpc.routes, ["/settings/sbr"], rpcName);
  }
  assert.equal(
    coverage.rpcs["tammy.v1.SbrService.RunSbrReadinessFixture"].boundary,
    "simulator_only",
  );
  for (const rpcName of future) {
    const rpc = coverage.rpcs[rpcName];
    assert.equal(rpc.stage, "declared_future", rpcName);
    assert.deepEqual(rpc.cases, [], rpcName);
    assert.deepEqual(rpc.futureCases, ["sbr/registration-readiness"], rpcName);
    assert.deepEqual(rpc.routes, ["/settings/sbr"], rpcName);
  }
});

test("SBR credential inspection permits only administrators and business lodgers", () => {
  const allowedInspectionRoles = {
    workspace_admin: "planned_allowed",
    business_preparer: "planned_permission_denied",
    business_lodger: "planned_allowed",
    auditor: "planned_permission_denied",
  };
  assert.deepEqual(
    SLICE_ONE_RPC_POLICY["tammy.v1.SbrService.GetMachineCredentialStatus"].roles,
    allowedInspectionRoles,
  );

  const adminOnly = {
    workspace_admin: "planned_allowed",
    business_preparer: "planned_permission_denied",
    business_lodger: "planned_permission_denied",
    auditor: "planned_permission_denied",
  };
  for (const rpcName of SBR_DECLARED_FUTURE_RPCS.filter(
    (name) => name !== "tammy.v1.SbrService.GetMachineCredentialStatus",
  )) {
    assert.deepEqual(SLICE_ONE_RPC_POLICY[rpcName].roles, adminOnly, rpcName);
  }

  for (const rpcName of [
    "tammy.v1.SbrService.ImportMachineCredential",
    "tammy.v1.SbrService.UnlockMachineCredential",
    "tammy.v1.SbrService.ReplaceMachineCredential",
    "tammy.v1.SbrService.RemoveMachineCredential",
    "tammy.v1.SbrService.ImportSbrProductId",
    "tammy.v1.SbrService.RemoveSbrProductId",
  ]) {
    const failures = SLICE_ONE_RPC_POLICY[rpcName].principalFailures;
    assert.ok(failures.includes("FACTOR_ASSERTION_REQUIRED"), rpcName);
    assert.ok(failures.includes("FACTOR_ASSERTION_STALE"), rpcName);
  }
});

test("reporting capability is promoted only with the packaged pre-setup evidence", async () => {
  const coverage = parseCoverageManifest(await readFile("test/e2e/coverage.yaml", "utf8"));
  const reporting = coverage.rpcs["tammy.v1.ReportingCapabilityService.GetReportingCapability"];

  assert.equal(reporting.stage, "production");
  assert.deepEqual(reporting.cases, ["reporting/capability-registry"]);
  assert.equal(reporting.futureCases, undefined);
  assert.deepEqual(coverage.scenarios["E2E-00"].cases, [
    "foundation/offline-ready",
    "reporting/capability-registry",
    CURRENT_WORKFLOW_CASE,
  ]);
  assert.ok(!coverage.scenarios["E2E-00"].futureCases.includes("reporting/capability-registry"));
});

test("every exposed business preload has production packaged evidence", async () => {
  const coverage = parseCoverageManifest(await readFile("test/e2e/coverage.yaml", "utf8"));
  const missing = EXPOSED_BUSINESS_RPCS.filter((rpcName) => coverage.rpcs[rpcName] === undefined);
  const declaredFuture = EXPOSED_BUSINESS_RPCS.filter(
    (rpcName) => coverage.rpcs[rpcName]?.stage === "declared_future",
  );

  assert.deepEqual({ missing, declaredFuture }, { missing: [], declaredFuture: [] });
  assert.ok(coverage.scenarios["E2E-00"].cases.includes(CURRENT_WORKFLOW_CASE));
  for (const rpcName of EXPOSED_BUSINESS_RPCS) {
    assert.deepEqual(coverage.rpcs[rpcName].cases, [CURRENT_WORKFLOW_CASE], rpcName);
    assert.equal(coverage.rpcs[rpcName].futureCases, undefined, rpcName);
  }
});

test("current workflow policy names real routes and implemented command semantics", () => {
  assert.deepEqual(SLICE_ONE_RPC_POLICY["tammy.v1.OrganisationService.CreateOrganisation"].routes, [
    "/setup/workspace",
  ]);

  for (const rpcName of [
    "tammy.v1.BankingService.MatchBankStatementLine",
    "tammy.v1.BankingService.CompleteBankReconciliation",
    "tammy.v1.DocumentService.SaveDocumentReview",
  ]) {
    const policy = SLICE_ONE_RPC_POLICY[rpcName];
    assert.deepEqual(policy.idempotency, {
      mode: "session_action",
      outcomes: ["state_predicate_election", "terminal_state_after_success"],
    });
    assert.ok(!policy.principalFailures.includes("IDEMPOTENCY_CONFLICT"), rpcName);
  }
});

test("Slice 1 policy does not export a duplicate idempotency-mode vocabulary", async () => {
  const policyModule = await import("./slice-one-coverage-policy.mjs");

  assert.equal(Object.hasOwn(policyModule, "SLICE_ONE_IDEMPOTENCY_MODES"), false);
});

test("coverage conflict failures match every public request concurrency field", async () => {
  const protoPaths = [
    "proto/tammy/v1/workspace.proto",
    "proto/tammy/v1/identity.proto",
    "proto/tammy/v1/organisation.proto",
    "proto/tammy/v1/accounting.proto",
    "proto/tammy/v1/banking.proto",
    "proto/tammy/v1/documents.proto",
    "proto/tammy/v1/tax.proto",
    "proto/tammy/v1/audit.proto",
    "proto/tammy/v1/financial_close.proto",
    "proto/tammy/v1/company_tax.proto",
    "proto/tammy/v1/company_return_submission.proto",
  ];

  for (const protoPath of protoPaths) {
    const source = await readFile(protoPath, "utf8");
    const packageName = source.match(/^package ([a-z0-9.]+);$/m)?.[1];
    const requestBodies = new Map(
      [...source.matchAll(/^message (\w+Request) \{([\s\S]*?)^\}$/gm)].map((match) => [
        match[1],
        match[2],
      ]),
    );

    for (const serviceMatch of source.matchAll(/^service (\w+) \{([\s\S]*?)^\}$/gm)) {
      for (const rpcMatch of serviceMatch[2].matchAll(/^\s+rpc (\w+)\((\w+Request)\)/gm)) {
        const rpcName = `${packageName}.${serviceMatch[1]}.${rpcMatch[1]}`;
        const request = requestBodies.get(rpcMatch[2]);
        const failures = SLICE_ONE_RPC_POLICY[rpcName].principalFailures;

        if (/\b(?:expected_version|expected_return_version|expected_predecessor_version|expected_latest_version)\s*=/.test(request)) {
          assert.ok(failures.includes("STALE_VERSION"), rpcName);
        } else {
          assert.ok(!failures.includes("STALE_VERSION"), rpcName);
        }
        if (/\bexpected_financial_revision\s*=/.test(request)) {
          assert.ok(failures.includes("SOURCE_CONFLICT"), rpcName);
          assert.ok(!failures.includes("STALE_VERSION"), rpcName);
        }
      }
    }
  }
});

test("session-action authentication failures match request presence", async () => {
  const sources = {
    banking: await readFile("proto/tammy/v1/banking.proto", "utf8"),
    documents: await readFile("proto/tammy/v1/documents.proto", "utf8"),
    workspace: await readFile("proto/tammy/v1/workspace.proto", "utf8"),
    identity: await readFile("proto/tammy/v1/identity.proto", "utf8"),
  };
  const expectations = [
    ["tammy.v1.WorkspaceService.LockWorkspace", "workspace", "LockWorkspaceRequest"],
    [
      "tammy.v1.WorkspaceService.ForgetRememberedWorkspace",
      "workspace",
      "ForgetRememberedWorkspaceRequest",
    ],
    ["tammy.v1.IdentityService.SignOut", "identity", "SignOutRequest"],
    ["tammy.v1.BankingService.MatchBankStatementLine", "banking", "MatchBankStatementLineRequest"],
    [
      "tammy.v1.BankingService.CompleteBankReconciliation",
      "banking",
      "CompleteBankReconciliationRequest",
    ],
    ["tammy.v1.DocumentService.SaveDocumentReview", "documents", "SaveDocumentReviewRequest"],
  ];

  assert.deepEqual(
    Object.entries(SLICE_ONE_RPC_POLICY)
      .filter(([, policy]) => policy.idempotency.mode === "session_action")
      .map(([rpcName]) => rpcName)
      .sort(),
    expectations.map(([rpcName]) => rpcName).sort(),
  );

  for (const [rpcName, sourceName, requestName] of expectations) {
    const request = sources[sourceName].match(
      new RegExp(`^message ${requestName} \\{([\\s\\S]*?)^\\}`, "m"),
    )?.[1];
    const authenticationRequired =
      /AuthenticationContext authentication = \d+ \[\(buf\.validate\.field\)\.required = true\]/.test(
        request,
      ) ||
      /CommandContext command_context = \d+ \[\(buf\.validate\.field\)\.required = true\]/.test(
        request,
      );
    assert.equal(
      SLICE_ONE_RPC_POLICY[rpcName].principalFailures.includes("AUTHENTICATION_REQUIRED"),
      authenticationRequired,
      rpcName,
    );
  }
});

test("RPCs allowed to all four defined roles never claim permission denial", () => {
  for (const [rpcName, policy] of Object.entries(SLICE_ONE_RPC_POLICY)) {
    const allRolesAllowed = ALL_ROLES_ALLOWED.every(
      (role) => policy.roles[role] === "planned_allowed",
    );
    if (allRolesAllowed) {
      assert.ok(!policy.principalFailures.includes("PERMISSION_DENIED"), rpcName);
    }
  }
});

test("SetAccountStatus distinguishes system and control account failures", () => {
  const failures =
    SLICE_ONE_RPC_POLICY["tammy.v1.AccountingService.SetAccountStatus"].principalFailures;

  assert.ok(failures.includes("SYSTEM_ACCOUNT"));
  assert.ok(failures.includes("CONTROL_ACCOUNT"));
});

test("projection helper keeps TOTP initialisms in one snake-case token", async () => {
  const { coverageProjectionName } = await import("./slice-one-coverage-policy.mjs");

  assert.equal(typeof coverageProjectionName, "function");
  assert.deepEqual(
    ["EnrolTOTP", "ConfirmTOTP", "AssertTOTP", "DisableTOTP"].map(coverageProjectionName),
    ["enrol_totp_result", "confirm_totp_result", "assert_totp_result", "disable_totp_result"],
  );
});
