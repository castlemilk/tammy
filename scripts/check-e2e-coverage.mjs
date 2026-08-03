import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { fromBinary } from "@bufbuild/protobuf";
import { FileDescriptorSetSchema } from "@bufbuild/protobuf/wkt";
import { parseDocument } from "yaml";

const REQUIRED_ROLES = ["workspace_admin", "business_preparer", "business_lodger", "auditor"];
const REVIEWED_EXCEPTION = "not_applicable_pre_workspace_system_query";
const PRE_WORKSPACE_SYSTEM_QUERY = "tammy.v1.SystemService.GetDiagnostics";
const PRODUCTION_STAGE = "production";
const DECLARED_FUTURE_STAGE = "declared_future";

function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function isStringArray(value) {
  return Array.isArray(value) && value.every((entry) => typeof entry === "string");
}

function coverageStage(entry, name) {
  if (entry.stage === PRODUCTION_STAGE || entry.stage === DECLARED_FUTURE_STAGE) {
    return entry.stage;
  }
  if (entry.stage === undefined && name === PRE_WORKSPACE_SYSTEM_QUERY) return PRODUCTION_STAGE;
  return undefined;
}

function hasCompleteFutureRpcMetadata(rpc) {
  return (
    rpc.cases.length === 0 &&
    isStringArray(rpc.futureCases) &&
    rpc.futureCases.length > 0 &&
    isStringArray(rpc.routes) &&
    rpc.routes.length > 0 &&
    isStringArray(rpc.principalFailures) &&
    rpc.principalFailures.length > 0 &&
    isRecord(rpc.list) &&
    isRecord(rpc.idempotency)
  );
}

function hasValidManifestShape(coverage) {
  if (
    !isRecord(coverage) ||
    coverage.schemaVersion !== 1 ||
    !isRecord(coverage.scenarios) ||
    !isRecord(coverage.rpcs) ||
    !isRecord(coverage.transitions)
  ) {
    return false;
  }
  if (
    !Object.values(coverage.scenarios).every(
      (scenario) =>
        isRecord(scenario) &&
        isStringArray(scenario.cases) &&
        (scenario.futureCases === undefined || isStringArray(scenario.futureCases)),
    )
  ) {
    return false;
  }
  if (
    !Object.entries(coverage.rpcs).every(
      ([rpcName, rpc]) =>
        isRecord(rpc) &&
        coverageStage(rpc, rpcName) !== undefined &&
        typeof rpc.preload === "string" &&
        isStringArray(rpc.cases) &&
        (rpc.futureCases === undefined || isStringArray(rpc.futureCases)) &&
        isStringArray(rpc.projections) &&
        (rpc.roles === REVIEWED_EXCEPTION || isRecord(rpc.roles)) &&
        (coverageStage(rpc, rpcName) === PRODUCTION_STAGE
          ? rpc.futureCases === undefined || rpc.futureCases.length === 0
          : hasCompleteFutureRpcMetadata(rpc)),
    )
  ) {
    return false;
  }
  if (Object.hasOwn(coverage.rpcs, PRE_WORKSPACE_SYSTEM_QUERY)) {
    const systemQuery = coverage.rpcs[PRE_WORKSPACE_SYSTEM_QUERY];
    if (
      systemQuery.roles !== REVIEWED_EXCEPTION ||
      systemQuery.list !== REVIEWED_EXCEPTION ||
      systemQuery.idempotency !== REVIEWED_EXCEPTION
    ) {
      return false;
    }
  }
  return Object.entries(coverage.transitions).every(
    ([transitionId, transition]) =>
      isRecord(transition) &&
      coverageStage(transition, transitionId) !== undefined &&
      isStringArray(transition.cases) &&
      (transition.futureCases === undefined || isStringArray(transition.futureCases)) &&
      (coverageStage(transition, transitionId) === PRODUCTION_STAGE
        ? transition.futureCases === undefined || transition.futureCases.length === 0
        : transition.cases.length === 0 && transition.futureCases.length > 0),
  );
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
      ([rpcName, rpcCoverage]) => coverageStage(rpcCoverage, rpcName) === DECLARED_FUTURE_STAGE,
    ) ||
      Object.entries(coverage.transitions).some(
        ([transitionId, transitionCoverage]) =>
          coverageStage(transitionCoverage, transitionId) === DECLARED_FUTURE_STAGE,
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
    if (coverageStage(rpcCoverage, rpcName) === DECLARED_FUTURE_STAGE) {
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
    if (argument === "--descriptors" && descriptorPath === undefined && args[index + 1]) {
      descriptorPath = path.resolve(args[index + 1]);
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
