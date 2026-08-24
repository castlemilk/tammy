import path from "node:path";
import { fileURLToPath } from "node:url";

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

function main() {
  if (process.argv.length !== 2) {
    throw new Error("SBR_REGISTRATION_CHECK_ARGUMENTS_INVALID");
  }
  process.stdout.write(`${JSON.stringify(createSbrRegistrationHandoffReport())}\n`);
  process.exitCode = 1;
}

if (process.argv[1] && fileURLToPath(import.meta.url) === path.resolve(process.argv[1])) {
  try {
    main();
  } catch (error) {
    process.stderr.write(
      `${error instanceof Error ? error.message : "SBR_REGISTRATION_CHECK_FAILED"}\n`,
    );
    process.exitCode = 1;
  }
}
