import { lstat, readFile, realpath } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  authenticateSbrEvteRegistration,
  MAX_SBR_EVIDENCE_BYTES,
} from "./sbr-registration-schema.mjs";

const REQUIRED_EXTERNAL_ITEMS = Object.freeze([
  "DSP_REGISTRATION",
  "PRODUCT_REGISTRATION",
  "OSF_ASSESSMENT",
  "ATO_COMPONENT_LICENCE_AND_TARGET",
  "EVTE_ACCESS",
  "SIGNED_ENDPOINT_PROFILE",
  "SERVICE_ENROLMENT",
  "SERVICE_CONFORMANCE",
  "INDEPENDENT_REVIEW",
  "EXPIRY_AND_REVALIDATION",
  "REDACTED_EVIDENCE_EXPORT",
]);
const INSTALLED_INPUTS = Object.freeze([
  ["PROFILE_MANIFEST", "sbr-profile-v1.json", "profileBytes"],
  ["PROFILE_SIGNATURE", "sbr-profile-v1.sig", "profileSignatureBytes"],
  ["COMPONENT_MANIFEST", "sbr-component-v1.json", "componentManifestBytes"],
  ["REGISTRATION_MANIFEST", "sbr-registration-v1.json", "registrationBytes"],
  ["REGISTRATION_SIGNATURE", "sbr-registration-v1.sig", "registrationSignatureBytes"],
  ["ENDPOINT_PROFILE", "sbr-endpoint-profile-v1.json", "endpointProfileBytes"],
]);
const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

export function createSbrRegistrationHandoffReport() {
  return {
    schema: "tammy-sbr-registration-check-v1",
    environment: "EVTE",
    status: "BLOCKED",
    code: "EVTE_SIGNED_INPUTS_REQUIRED",
    required_external_items: [...REQUIRED_EXTERNAL_ITEMS],
    production_sbr: "UNAVAILABLE",
    bas_submission: "UNAVAILABLE",
  };
}

function invalidInstalledReport() {
  return {
    ...createSbrRegistrationHandoffReport(),
    code: "INSTALLED_INPUT_UNSAFE",
    installed_input_state: "INVALID",
  };
}

async function readExactInstalledInputs(root) {
  if (!path.isAbsolute(root)) throw new Error("SBR_REGISTRATION_CHECK_ROOT_INVALID");
  const directory = path.join(root, "config", "sbr", "evte");
  try {
    const resolvedDirectory = await realpath(directory);
    if (resolvedDirectory !== path.resolve(directory)) return { invalid: true };
  } catch (error) {
    if (error?.code !== "ENOENT") return { invalid: true };
  }

  const missing = [];
  const values = {};
  for (const [label, filename, inputName] of INSTALLED_INPUTS) {
    const location = path.join(directory, filename);
    try {
      const metadata = await lstat(location);
      if (
        !metadata.isFile() ||
        metadata.isSymbolicLink() ||
        metadata.size < 1 ||
        metadata.size > MAX_SBR_EVIDENCE_BYTES ||
        (metadata.mode & 0o022) !== 0
      ) {
        return { invalid: true };
      }
      const resolved = await realpath(location);
      if (resolved !== location) return { invalid: true };
      const bytes = await readFile(location);
      if (bytes.byteLength !== metadata.size) return { invalid: true };
      values[inputName] = bytes;
    } catch (error) {
      if (error?.code === "ENOENT") missing.push(label);
      else return { invalid: true };
    }
  }
  return missing.length > 0 ? { missing } : { values };
}

export async function checkInstalledSbrRegistration({
  authenticate = authenticateSbrEvteRegistration,
  repositoryRoot: root = repositoryRoot,
} = {}) {
  const installed = await readExactInstalledInputs(root);
  if (installed.invalid) return invalidInstalledReport();
  if (installed.missing) {
    return {
      ...createSbrRegistrationHandoffReport(),
      installed_input_state: "MISSING",
      missing_installed_inputs: installed.missing,
    };
  }
  let preflight;
  try {
    preflight = authenticate({ ...installed.values, phase: "PRE_CONFORMANCE" });
  } catch {
    return {
      ...createSbrRegistrationHandoffReport(),
      code: "INSTALLED_INPUT_INVALID",
      installed_input_state: "INVALID",
    };
  }
  const ready = preflight.readiness.ready === true;
  return {
    schema: "tammy-sbr-registration-check-v1",
    environment: "EVTE",
    status: ready ? "READY_FOR_EVTE" : "BLOCKED",
    code: preflight.readiness.code,
    installed_input_state: ready ? "AUTHENTICATED_READY" : "AUTHENTICATED_BLOCKED",
    fingerprints: preflight.fingerprints,
    metadata: preflight.metadata,
    production_sbr: "UNAVAILABLE",
    bas_submission: "UNAVAILABLE",
  };
}

async function main() {
  const arguments_ = process.argv.slice(2);
  const doctor = arguments_.length === 1 && arguments_[0] === "--doctor-preflight";
  if (arguments_.length !== 0 && !doctor) {
    throw new Error("SBR_REGISTRATION_CHECK_ARGUMENTS_INVALID");
  }
  const report = await checkInstalledSbrRegistration();
  const output = `${JSON.stringify(report)}\n`;
  if (Buffer.byteLength(output) >= 4096) throw new Error("SBR_REGISTRATION_CHECK_OUTPUT_INVALID");
  process.stdout.write(output);
  process.exitCode = doctor || report.status === "READY_FOR_EVTE" ? 0 : 1;
}

if (process.argv[1] && fileURLToPath(import.meta.url) === path.resolve(process.argv[1])) {
  main().catch((error) => {
    process.stderr.write(
      `${error instanceof Error ? error.message : "SBR_REGISTRATION_CHECK_FAILED"}\n`,
    );
    process.exitCode = 1;
  });
}
