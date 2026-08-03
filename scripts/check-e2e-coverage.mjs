import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { fromBinary } from "@bufbuild/protobuf";
import { FileDescriptorSetSchema } from "@bufbuild/protobuf/wkt";
import { parseDocument } from "yaml";

const REQUIRED_ROLES = ["workspace_admin", "business_preparer", "business_lodger", "auditor"];
const REVIEWED_EXCEPTION = "not_applicable_pre_workspace_system_query";
const PRE_WORKSPACE_SYSTEM_QUERY = "tammy.v1.SystemService.GetDiagnostics";

function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function isStringArray(value) {
  return Array.isArray(value) && value.every((entry) => typeof entry === "string");
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
      (scenario) => isRecord(scenario) && isStringArray(scenario.cases),
    )
  ) {
    return false;
  }
  if (
    !Object.values(coverage.rpcs).every(
      (rpc) =>
        isRecord(rpc) &&
        typeof rpc.preload === "string" &&
        isStringArray(rpc.cases) &&
        isStringArray(rpc.projections) &&
        (rpc.roles === REVIEWED_EXCEPTION || isRecord(rpc.roles)),
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
  return Object.values(coverage.transitions).every(
    (transition) => isRecord(transition) && isStringArray(transition.cases),
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
  const descriptorSet = fromBinary(FileDescriptorSetSchema, bytes);
  return descriptorSet.file
    .filter((file) => file.package?.startsWith("tammy."))
    .flatMap((file) =>
      file.service.flatMap((service) =>
        service.method.map((method) => `${file.package}.${service.name}.${method.name}`),
      ),
    )
    .sort();
}

export function checkE2ECoverage({ coverage, descriptorRpcs, preloadMethods, transitionIds }) {
  if (
    !hasValidManifestShape(coverage) ||
    !isStringArray(descriptorRpcs) ||
    !isStringArray(preloadMethods) ||
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
  const scenarioCases = Object.values(coverage.scenarios).flatMap((scenario) => scenario.cases);
  for (const transitionCoverage of Object.values(coverage.transitions)) {
    for (const scenarioCase of transitionCoverage.cases) {
      if (!scenarioCases.includes(scenarioCase)) {
        throw new Error("E2E_COVERAGE_CASE_MISSING");
      }
    }
  }
  for (const rpcCoverage of Object.values(coverage.rpcs)) {
    for (const field of ["roles", "list", "idempotency"]) {
      if (typeof rpcCoverage[field] === "string" && rpcCoverage[field] !== REVIEWED_EXCEPTION) {
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
    if (!preloadMethods.includes(rpcCoverage.preload)) {
      throw new Error("E2E_COVERAGE_PRELOAD_MISSING");
    }
    for (const scenarioCase of rpcCoverage.cases) {
      if (!scenarioCases.includes(scenarioCase)) {
        throw new Error("E2E_COVERAGE_CASE_MISSING");
      }
    }
  }
}

export async function runE2ECoverage({ descriptorPath, root = process.cwd() }) {
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
    transitionIds: Object.keys(transitionIndex.transitions),
  });
}

export function coverageDescriptorPath(args) {
  if (args.length === 2 && args[0] === "--descriptors") return path.resolve(args[1]);
  throw new Error("E2E_COVERAGE_DESCRIPTORS_REQUIRED");
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await runE2ECoverage({ descriptorPath: coverageDescriptorPath(process.argv.slice(2)) });
}
