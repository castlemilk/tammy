import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { fromBinary } from "@bufbuild/protobuf";
import { FileDescriptorSetSchema } from "@bufbuild/protobuf/wkt";
import { parseDocument } from "yaml";
import { E2E_IDEMPOTENCY_MODES } from "./e2e-coverage-vocabulary.mjs";

const REQUIRED_ROLES = ["workspace_admin", "business_preparer", "business_lodger", "auditor"];
const REVIEWED_EXCEPTION = "not_applicable_pre_workspace_system_query";
const PRE_WORKSPACE_SYSTEM_QUERY = "tammy.v1.SystemService.GetDiagnostics";
const PRODUCTION_STAGE = "production";
const DECLARED_FUTURE_STAGE = "declared_future";
const CASE_ID_PATTERN = /^[a-z0-9]+(?:-[a-z0-9]+)*(?:\/[a-z0-9]+(?:-[a-z0-9]+)*)+$/;
const PRELOAD_NAME_PATTERN = /^[a-z][A-Za-z0-9]*$/;
const ROUTE_PATTERN = /^\/[a-z0-9]+(?:-[a-z0-9]+)*(?:\/[a-z0-9]+(?:-[a-z0-9]+)*)*$/;
const OUTCOME_CODE_PATTERN = /^[A-Z][A-Z0-9_]*$/;
const STABLE_TOKEN_PATTERN = /^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$/;
const TOP_LEVEL_KEYS = ["schemaVersion", "scenarios", "rpcs", "transitions"];
const SCENARIO_KEYS = ["cases", "futureCases"];
const RPC_KEYS = [
  "stage",
  "preload",
  "cases",
  "futureCases",
  "projections",
  "roles",
  "routes",
  "principalFailures",
  "list",
  "idempotency",
];
const TRANSITION_KEYS = ["stage", "cases", "futureCases"];

function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function isStringArray(value) {
  return Array.isArray(value) && value.every((entry) => typeof entry === "string");
}

function matchesPattern(value, pattern) {
  return typeof value === "string" && pattern.test(value);
}

function isUniqueStringArray(value, validator, requireNonEmpty = false) {
  return (
    Array.isArray(value) &&
    (!requireNonEmpty || value.length > 0) &&
    value.every(validator) &&
    new Set(value).size === value.length
  );
}

function hasExactKeys(value, keys) {
  return (
    isRecord(value) &&
    Object.keys(value).length === keys.length &&
    keys.every((key) => Object.hasOwn(value, key))
  );
}

function hasOnlyKeys(value, keys) {
  return isRecord(value) && Object.keys(value).every((key) => keys.includes(key));
}

function coverageStage(entry, name, rowKind) {
  if (entry.stage === PRODUCTION_STAGE || entry.stage === DECLARED_FUTURE_STAGE) {
    return entry.stage;
  }
  if (entry.stage === undefined && rowKind === "rpc" && name === PRE_WORKSPACE_SYSTEM_QUERY) {
    return PRODUCTION_STAGE;
  }
  return undefined;
}

function hasCompleteListMetadata(value) {
  return (
    hasExactKeys(value, ["states"]) &&
    isUniqueStringArray(value.states, (state) => matchesPattern(state, STABLE_TOKEN_PATTERN), true)
  );
}

function hasCompleteIdempotencyMetadata(value) {
  return (
    hasExactKeys(value, ["mode", "outcomes"]) &&
    E2E_IDEMPOTENCY_MODES.includes(value.mode) &&
    isUniqueStringArray(
      value.outcomes,
      (outcome) => matchesPattern(outcome, STABLE_TOKEN_PATTERN),
      true,
    )
  );
}

function hasCompleteRoutes(value) {
  return isUniqueStringArray(value, (route) => matchesPattern(route, ROUTE_PATTERN), true);
}

function hasCompletePrincipalFailures(value) {
  return isUniqueStringArray(
    value,
    (outcome) => matchesPattern(outcome, OUTCOME_CODE_PATTERN),
    true,
  );
}

function hasCompleteBusinessRpcMetadata(rpc) {
  return (
    isUniqueStringArray(
      rpc.projections,
      (projection) => matchesPattern(projection, STABLE_TOKEN_PATTERN),
      true,
    ) &&
    hasCompleteRoutes(rpc.routes) &&
    hasCompletePrincipalFailures(rpc.principalFailures) &&
    isRecord(rpc.roles) &&
    Object.values(rpc.roles).every((outcome) => matchesPattern(outcome, STABLE_TOKEN_PATTERN)) &&
    hasCompleteListMetadata(rpc.list) &&
    hasCompleteIdempotencyMetadata(rpc.idempotency)
  );
}

function hasValidStageCases(stage, entry) {
  if (stage === PRODUCTION_STAGE) {
    return (
      entry.cases.length > 0 && (entry.futureCases === undefined || entry.futureCases.length === 0)
    );
  }
  return (
    entry.cases.length === 0 && Array.isArray(entry.futureCases) && entry.futureCases.length > 0
  );
}

function hasValidScenarioShape(scenarios) {
  if (
    !Object.values(scenarios).every(
      (scenario) =>
        hasOnlyKeys(scenario, SCENARIO_KEYS) &&
        isUniqueStringArray(scenario.cases, (caseId) => matchesPattern(caseId, CASE_ID_PATTERN)) &&
        (scenario.futureCases === undefined ||
          isUniqueStringArray(scenario.futureCases, (caseId) =>
            matchesPattern(caseId, CASE_ID_PATTERN),
          )),
    )
  ) {
    return false;
  }
  const allCaseIds = Object.values(scenarios).flatMap((scenario) => [
    ...scenario.cases,
    ...(scenario.futureCases ?? []),
  ]);
  return new Set(allCaseIds).size === allCaseIds.length;
}

function hasValidManifestShape(coverage) {
  if (
    !hasExactKeys(coverage, TOP_LEVEL_KEYS) ||
    coverage.schemaVersion !== 1 ||
    !isRecord(coverage.scenarios) ||
    !isRecord(coverage.rpcs) ||
    !isRecord(coverage.transitions)
  ) {
    return false;
  }
  if (!hasValidScenarioShape(coverage.scenarios)) {
    return false;
  }
  if (
    !Object.entries(coverage.rpcs).every(([rpcName, rpc]) => {
      if (!hasOnlyKeys(rpc, RPC_KEYS)) return false;
      const stage = coverageStage(rpc, rpcName, "rpc");
      if (
        stage === undefined ||
        !matchesPattern(rpc.preload, PRELOAD_NAME_PATTERN) ||
        !isUniqueStringArray(rpc.cases, (caseId) => matchesPattern(caseId, CASE_ID_PATTERN)) ||
        (rpc.futureCases !== undefined &&
          !isUniqueStringArray(rpc.futureCases, (caseId) =>
            matchesPattern(caseId, CASE_ID_PATTERN),
          )) ||
        !hasValidStageCases(stage, rpc)
      ) {
        return false;
      }
      if (rpcName === PRE_WORKSPACE_SYSTEM_QUERY) {
        return (
          isUniqueStringArray(
            rpc.projections,
            (projection) => matchesPattern(projection, STABLE_TOKEN_PATTERN),
            true,
          ) &&
          (rpc.routes === undefined || hasCompleteRoutes(rpc.routes)) &&
          (rpc.principalFailures === undefined ||
            hasCompletePrincipalFailures(rpc.principalFailures)) &&
          rpc.roles === REVIEWED_EXCEPTION &&
          rpc.list === REVIEWED_EXCEPTION &&
          rpc.idempotency === REVIEWED_EXCEPTION
        );
      }
      return hasCompleteBusinessRpcMetadata(rpc);
    })
  ) {
    return false;
  }
  return Object.entries(coverage.transitions).every(([transitionId, transition]) => {
    if (!hasOnlyKeys(transition, TRANSITION_KEYS)) return false;
    const stage = coverageStage(transition, transitionId, "transition");
    return (
      stage !== undefined &&
      isUniqueStringArray(transition.cases, (caseId) => matchesPattern(caseId, CASE_ID_PATTERN)) &&
      (transition.futureCases === undefined ||
        isUniqueStringArray(transition.futureCases, (caseId) =>
          matchesPattern(caseId, CASE_ID_PATTERN),
        )) &&
      hasValidStageCases(stage, transition)
    );
  });
}

export function parseCoverageManifest(source) {
  const document = parseDocument(source, { uniqueKeys: true });
  if (document.errors.some((error) => error.code === "DUPLICATE_KEY")) {
    throw new Error("E2E_COVERAGE_YAML_DUPLICATE_KEY");
  }
  if (document.errors.length > 0) {
    throw new Error("E2E_COVERAGE_MANIFEST_INVALID");
  }
  return document.toJS();
}

export function descriptorRpcNames(bytes) {
  let descriptorSet;
  try {
    descriptorSet = fromBinary(FileDescriptorSetSchema, bytes);
  } catch (cause) {
    throw new Error("E2E_COVERAGE_DESCRIPTOR_INVALID", { cause });
  }
  return descriptorSet.file
    .filter((file) => file.package?.startsWith("tammy."))
    .flatMap((file) =>
      file.service.flatMap((service) =>
        service.method.map((method) => `${file.package}.${service.name}.${method.name}`),
      ),
    )
    .sort();
}

export function checkE2ECoverage({
  coverage,
  descriptorRpcs,
  preloadMethods,
  requireProduction = false,
  transitionIds,
}) {
  if (
    !hasValidManifestShape(coverage) ||
    !isStringArray(descriptorRpcs) ||
    !isStringArray(preloadMethods) ||
    typeof requireProduction !== "boolean" ||
    !isStringArray(transitionIds)
  ) {
    throw new Error("E2E_COVERAGE_MANIFEST_INVALID");
  }
  for (const rpc of descriptorRpcs) {
    if (!Object.hasOwn(coverage.rpcs, rpc)) {
      throw new Error("E2E_COVERAGE_RPC_MISSING");
    }
  }
  for (const rpc of Object.keys(coverage.rpcs)) {
    if (!descriptorRpcs.includes(rpc)) {
      throw new Error("E2E_COVERAGE_RPC_UNKNOWN");
    }
  }
  for (const transitionId of transitionIds) {
    if (!Object.hasOwn(coverage.transitions, transitionId)) {
      throw new Error("E2E_COVERAGE_TRANSITION_MISSING");
    }
  }
  for (const transitionId of Object.keys(coverage.transitions)) {
    if (!transitionIds.includes(transitionId)) {
      throw new Error("E2E_COVERAGE_TRANSITION_UNKNOWN");
    }
  }
  if (
    requireProduction &&
    (Object.entries(coverage.rpcs).some(
      ([rpcName, rpcCoverage]) =>
        coverageStage(rpcCoverage, rpcName, "rpc") === DECLARED_FUTURE_STAGE,
    ) ||
      Object.entries(coverage.transitions).some(
        ([transitionId, transitionCoverage]) =>
          coverageStage(transitionCoverage, transitionId, "transition") === DECLARED_FUTURE_STAGE,
      ))
  ) {
    throw new Error("E2E_COVERAGE_FUTURE_PROMOTION_REQUIRED");
  }
  const scenarioCases = Object.values(coverage.scenarios).flatMap((scenario) => scenario.cases);
  const scenarioFutureCases = Object.values(coverage.scenarios).flatMap(
    (scenario) => scenario.futureCases ?? [],
  );
  for (const transitionCoverage of Object.values(coverage.transitions)) {
    for (const scenarioCase of transitionCoverage.cases) {
      if (!scenarioCases.includes(scenarioCase)) {
        throw new Error("E2E_COVERAGE_CASE_MISSING");
      }
    }
    for (const futureCase of transitionCoverage.futureCases ?? []) {
      if (!scenarioFutureCases.includes(futureCase)) {
        throw new Error("E2E_COVERAGE_FUTURE_CASE_MISSING");
      }
    }
  }
  for (const [rpcName, rpcCoverage] of Object.entries(coverage.rpcs)) {
    for (const field of ["roles", "list", "idempotency"]) {
      if (
        typeof rpcCoverage[field] === "string" &&
        (rpcCoverage[field] !== REVIEWED_EXCEPTION || rpcName !== PRE_WORKSPACE_SYSTEM_QUERY)
      ) {
        throw new Error("E2E_COVERAGE_MANIFEST_INVALID");
      }
    }
    if (rpcCoverage.roles !== REVIEWED_EXCEPTION) {
      for (const role of REQUIRED_ROLES) {
        if (!Object.hasOwn(rpcCoverage.roles, role)) {
          throw new Error("E2E_COVERAGE_ROLE_MISSING");
        }
      }
      for (const role of Object.keys(rpcCoverage.roles)) {
        if (!REQUIRED_ROLES.includes(role)) {
          throw new Error("E2E_COVERAGE_ROLE_EXTRA");
        }
      }
    }
    if (coverageStage(rpcCoverage, rpcName, "rpc") === DECLARED_FUTURE_STAGE) {
      if (preloadMethods.includes(rpcCoverage.preload)) {
        throw new Error("E2E_COVERAGE_FUTURE_PROMOTION_REQUIRED");
      }
    } else if (!preloadMethods.includes(rpcCoverage.preload)) {
      throw new Error("E2E_COVERAGE_PRELOAD_MISSING");
    }
    for (const scenarioCase of rpcCoverage.cases) {
      if (!scenarioCases.includes(scenarioCase)) {
        throw new Error("E2E_COVERAGE_CASE_MISSING");
      }
    }
    for (const futureCase of rpcCoverage.futureCases ?? []) {
      if (!scenarioFutureCases.includes(futureCase)) {
        throw new Error("E2E_COVERAGE_FUTURE_CASE_MISSING");
      }
    }
  }
}

export async function runE2ECoverage({
  descriptorPath,
  requireProduction = false,
  root = process.cwd(),
}) {
  const [descriptorBytes, coverageSource, transitionsSource, preloadSource] = await Promise.all([
    readFile(descriptorPath),
    readFile(path.join(root, "test/e2e/coverage.yaml"), "utf8"),
    readFile(path.join(root, "test/e2e/transitions.yaml"), "utf8"),
    readFile(path.join(root, "apps/desktop/src/shared/preload-methods.json"), "utf8"),
  ]);
  const coverage = parseCoverageManifest(coverageSource);
  const transitionIndex = parseCoverageManifest(transitionsSource);
  let preloadMethods;
  try {
    preloadMethods = JSON.parse(preloadSource);
  } catch {
    throw new Error("E2E_COVERAGE_MANIFEST_INVALID");
  }
  if (!isRecord(transitionIndex?.transitions) || !Array.isArray(preloadMethods)) {
    throw new Error("E2E_COVERAGE_MANIFEST_INVALID");
  }
  checkE2ECoverage({
    coverage,
    descriptorRpcs: descriptorRpcNames(descriptorBytes),
    preloadMethods,
    requireProduction,
    transitionIds: Object.keys(transitionIndex.transitions),
  });
}

export function coverageCliOptions(args) {
  let descriptorPath;
  let requireProduction = false;
  for (let index = 0; index < args.length; index += 1) {
    const argument = args[index];
    if (argument === "--require-production" && !requireProduction) {
      requireProduction = true;
      continue;
    }
    const descriptorArgument = args[index + 1];
    if (
      argument === "--descriptors" &&
      descriptorPath === undefined &&
      descriptorArgument &&
      !descriptorArgument.startsWith("-")
    ) {
      descriptorPath = path.resolve(descriptorArgument);
      index += 1;
      continue;
    }
    throw new Error("E2E_COVERAGE_DESCRIPTORS_REQUIRED");
  }
  if (descriptorPath === undefined) throw new Error("E2E_COVERAGE_DESCRIPTORS_REQUIRED");
  return { descriptorPath, requireProduction };
}

export function coverageDescriptorPath(args) {
  return coverageCliOptions(args).descriptorPath;
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await runE2ECoverage(coverageCliOptions(process.argv.slice(2)));
}
