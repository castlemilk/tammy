import path from "node:path";

export const RELEASE_STATES = Object.freeze([
  "NOT_READY",
  "REPOSITORY_READY",
  "CANDIDATE_READY",
  "PRE_UPLOAD_READY",
  "UPLOADED",
  "PRE_SUBMIT_READY",
]);

const SECRET_KEY = /secret|token|password|credential|privatekey/i;
const SECRET_VALUE_PATTERNS = [
  /\b(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{16,}\b/,
  /\b(?:ghp|github_pat)_[A-Za-z0-9_]{16,}\b/,
  /\bAKIA[A-Z0-9]{16}\b/,
  /-----BEGIN [A-Z ]*PRIVATE KEY-----/,
  /\b(?:password|token|secret)=[^&\s]{8,}/i,
];
const VERSION = /^[0-9]+\.[0-9]+\.[0-9]+$/;
const BUILD = /^[1-9][0-9]*$/;
const SHA40 = /^[0-9a-f]{40}$/;
const SHA256 = /^[0-9a-f]{64}$/;
const TEAM_ID = /^[A-Z0-9]{10}$/;
const UTC_TIME = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/;
const BASE_ATTESTATION_KEYS = [
  "accountablePerson",
  "buildNumber",
  "confirmedAt",
  "evidenceReference",
  "kind",
  "outcome",
  "releaseVersion",
  "schemaVersion",
];
const SELLER_KEYS = [
  ...BASE_ATTESTATION_KEYS,
  "accountHolder",
  "activeAgreements",
  "appId",
  "appleDeveloperIdentifierId",
  "appStoreConnectId",
  "applicationGroup",
  "certificateClasses",
  "eligibilityBranch",
  "helperIdentifiers",
  "profilesReissued",
  "sellerName",
  "teamId",
];
const EXPECTED_HELPERS = [
  "com.tammy.desktop.helper",
  "com.tammy.desktop.helper.GPU",
  "com.tammy.desktop.helper.Plugin",
  "com.tammy.desktop.helper.Renderer",
];
const EXPECTED_CERTIFICATES = [
  "Apple Development",
  "Apple Distribution",
  "Mac Installer Distribution",
];
const ATTESTATION_OUTCOMES = new Map([
  ["company-controller", new Set(["confirmed"])],
  ["seller-eligibility", new Set(["eligible"])],
  ["content-rights", new Set(["owned"])],
  ["export-compliance", new Set(["exempt", "non-exempt"])],
  ["pricing-availability", new Set(["confirmed"])],
  ["privacy-answer", new Set(["no-data-collected-no-tracking"])],
  ["age-rating", new Set(["completed"])],
  ["processed-build", new Set(["selected"])],
  ["metadata-assets-entered", new Set(["entered"])],
  ["app-store-warning-review", new Set(["clear", "resolved"])],
]);
const LIFECYCLE_KINDS = new Set([
  "uploaded",
  "expired",
  "superseded",
  "submitted",
  "approved",
  "rejected",
]);
const EVENT_BASE_KEYS = [
  "buildNumber",
  "kind",
  "occurredAt",
  "operator",
  "releaseVersion",
  "schemaVersion",
];

const REPOSITORY_REQUIREMENTS = [
  ["metadata", "STORE_METADATA_NOT_FINAL", "Finalize the App Store metadata worksheet."],
  ["platformIdentity", "PLATFORM_IDENTITY_NOT_VERIFIED", "Verify the macOS platform identity."],
  ["policy", "PRIVACY_POLICY_NOT_FINAL", "Finalize the canonical privacy policy."],
  ["publicSite", "PUBLIC_SITE_NOT_RECORDED", "Publish and verify the Sites version."],
  ["schemas", "RELEASE_SCHEMAS_NOT_READY", "Validate the release schemas."],
  [
    "screenshotDefinitions",
    "SCREENSHOT_DEFINITIONS_NOT_READY",
    "Validate the screenshot definitions.",
  ],
  ["storeIdentity", "STORE_IDENTITY_NOT_READY", "Validate the canonical store identity."],
  ["tests", "REPOSITORY_TESTS_NOT_PASSED", "Run the repository release tests."],
];
const CANDIDATE_REQUIREMENTS = [
  ["sourceCommit", "CANDIDATE_SOURCE_COMMIT_MISSING", "Bind the candidate to a source commit."],
  ["sourceTree", "CANDIDATE_SOURCE_TREE_MISSING", "Bind the candidate to a source tree."],
  ["appSha256", "CANDIDATE_APP_HASH_MISSING", "Record the signed app hash."],
  ["packageSha256", "CANDIDATE_PACKAGE_HASH_MISSING", "Record the installer package hash."],
  ["buildNumberReserved", "CANDIDATE_BUILD_NOT_RESERVED", "Verify the build-number reservation."],
  ["signingProfilePassed", "CANDIDATE_SIGNING_NOT_VERIFIED", "Verify signing and provisioning."],
  ["publicUrlsMatch", "CANDIDATE_PUBLIC_URLS_MISMATCH", "Verify the embedded public URLs."],
  [
    "privacyEvidencePassed",
    "CANDIDATE_PRIVACY_EVIDENCE_MISSING",
    "Record candidate privacy evidence.",
  ],
  [
    "runtimeEgressEvidencePassed",
    "CANDIDATE_EGRESS_EVIDENCE_MISSING",
    "Record runtime egress evidence.",
  ],
  ["screenshotsLinked", "CANDIDATE_SCREENSHOTS_NOT_LINKED", "Link the validated screenshots."],
];
const CANDIDATE_KEYS = [
  "appSha256",
  "buildNumber",
  "buildNumberReserved",
  "packageSha256",
  "privacyEvidencePassed",
  "publicUrlsMatch",
  "releaseVersion",
  "runtimeEgressEvidencePassed",
  "screenshotsLinked",
  "signingProfilePassed",
  "sourceCommit",
  "sourceTree",
];
const PRE_UPLOAD_KINDS = [
  "company-controller",
  "seller-eligibility",
  "content-rights",
  "export-compliance",
  "pricing-availability",
  "privacy-answer",
];
const PRE_SUBMIT_KINDS = [
  "processed-build",
  "metadata-assets-entered",
  "age-rating",
  "app-store-warning-review",
];

function fail(code) {
  throw new Error(code);
}

function assertExactKeys(value, keys, code) {
  if (!value || typeof value !== "object" || Array.isArray(value)) fail(code);
  const actual = Object.keys(value).sort();
  const expected = [...keys].sort();
  if (actual.length !== expected.length || actual.some((key, index) => key !== expected[index])) {
    fail(code);
  }
}

function assertNoSecretKeys(value, code) {
  if (!value || typeof value !== "object") return;
  for (const [key, child] of Object.entries(value)) {
    if (SECRET_KEY.test(key)) fail(code);
    assertNoSecretKeys(child, code);
  }
}

function assertNoSecretMaterial(value, code) {
  if (typeof value === "string") {
    if (SECRET_VALUE_PATTERNS.some((pattern) => pattern.test(value))) fail(code);
    return;
  }
  if (!value || typeof value !== "object") return;
  for (const child of Object.values(value)) assertNoSecretMaterial(child, code);
}

function isUtcTime(value) {
  if (typeof value !== "string" || !UTC_TIME.test(value)) return false;
  try {
    return new Date(value).toISOString() === value;
  } catch {
    return false;
  }
}

function hasControlCharacters(value) {
  return [...value].some((character) => {
    const codePoint = character.codePointAt(0);
    return codePoint < 32 || codePoint === 127;
  });
}

function isPerson(value) {
  return (
    typeof value === "string" &&
    value.trim() === value &&
    value.length >= 2 &&
    value.length <= 100 &&
    !hasControlCharacters(value)
  );
}

function isSafeReference(value) {
  if (
    typeof value !== "string" ||
    value.trim() !== value ||
    value.length === 0 ||
    value.length > 512 ||
    hasControlCharacters(value) ||
    path.isAbsolute(value)
  ) {
    return false;
  }
  if (!/^[A-Za-z][A-Za-z0-9+.-]*:/.test(value)) return true;
  try {
    const url = new URL(value);
    return url.protocol === "https:" && !url.username && !url.password && !url.search && !url.hash;
  } catch {
    return false;
  }
}

function assertCommonRecord(record, timeKey, actorKey, code) {
  if (
    record.schemaVersion !== 1 ||
    !VERSION.test(record.releaseVersion) ||
    !BUILD.test(record.buildNumber) ||
    !isPerson(record[actorKey]) ||
    !isUtcTime(record[timeKey])
  ) {
    fail(code);
  }
}

function arraysEqual(actual, expected) {
  return (
    Array.isArray(actual) &&
    actual.length === expected.length &&
    actual.every((value, index) => value === expected[index])
  );
}

export function validateReleaseState(state) {
  if (typeof state === "string") {
    if (!RELEASE_STATES.includes(state)) fail("RELEASE_STATE_INVALID");
    return state;
  }
  assertExactKeys(state, ["blockers", "passed", "state"], "RELEASE_STATE_INVALID");
  validateReleaseState(state.state);
  if (!Array.isArray(state.passed) || !Array.isArray(state.blockers)) {
    fail("RELEASE_STATE_INVALID");
  }
  const passed = new Set(state.passed);
  if (passed.size !== state.passed.length || [...passed].some((item) => typeof item !== "string")) {
    fail("RELEASE_STATE_INVALID");
  }
  for (const currentBlocker of state.blockers) {
    assertExactKeys(currentBlocker, ["code", "owner", "remediation"], "RELEASE_STATE_INVALID");
    if (
      !/^[A-Z][A-Z0-9_]+$/.test(currentBlocker.code) ||
      !["repository", "candidate", "operator", "apple"].includes(currentBlocker.owner) ||
      typeof currentBlocker.remediation !== "string" ||
      currentBlocker.remediation.length === 0 ||
      passed.has(currentBlocker.code)
    ) {
      fail("RELEASE_STATE_INVALID");
    }
  }
  return state;
}

export function validateReleaseAttestation(attestation) {
  const code = "RELEASE_ATTESTATION_INVALID";
  assertNoSecretKeys(attestation, code);
  assertNoSecretMaterial(attestation, code);
  if (!attestation || !ATTESTATION_OUTCOMES.has(attestation.kind)) fail(code);
  const seller = attestation.kind === "seller-eligibility";
  const keys = seller
    ? [
        ...SELLER_KEYS,
        ...(attestation.eligibilityBranch === "written-apple-exception"
          ? ["writtenAppleExceptionReference"]
          : []),
      ]
    : BASE_ATTESTATION_KEYS;
  assertExactKeys(attestation, keys, seller ? "SELLER_ELIGIBILITY_INVALID" : code);
  assertCommonRecord(attestation, "confirmedAt", "accountablePerson", code);
  if (
    !isSafeReference(attestation.evidenceReference) ||
    !ATTESTATION_OUTCOMES.get(attestation.kind).has(attestation.outcome) ||
    attestation.evidenceReference.includes("OPERATOR_REQUIRED") ||
    attestation.accountablePerson.includes("OPERATOR_REQUIRED")
  ) {
    fail(code);
  }
  if (!seller) return attestation;

  const sellerCode = "SELLER_ELIGIBILITY_INVALID";
  const expectedPrefix = `${attestation.teamId}.com.tammy.desktop`;
  if (
    !["company-organization", "written-apple-exception"].includes(attestation.eligibilityBranch) ||
    !TEAM_ID.test(attestation.teamId) ||
    !isPerson(attestation.sellerName) ||
    !isPerson(attestation.accountHolder) ||
    attestation.activeAgreements !== true ||
    attestation.appId !== "com.tammy.desktop" ||
    attestation.appleDeveloperIdentifierId !== expectedPrefix ||
    !/^[1-9][0-9]{5,19}$/.test(attestation.appStoreConnectId) ||
    attestation.applicationGroup !== expectedPrefix ||
    !arraysEqual(attestation.helperIdentifiers, EXPECTED_HELPERS) ||
    !arraysEqual(attestation.certificateClasses, EXPECTED_CERTIFICATES) ||
    attestation.profilesReissued !== true
  ) {
    fail(sellerCode);
  }
  if (
    attestation.eligibilityBranch === "company-organization" &&
    (attestation.sellerName !== "Gamma Systems Pty Ltd" || attestation.teamId === "WFTX6CN23F")
  ) {
    fail(sellerCode);
  }
  if (attestation.eligibilityBranch === "written-apple-exception") {
    const reference = attestation.writtenAppleExceptionReference;
    if (
      !isSafeReference(reference) ||
      !/written[-_ ](?:apple[-_ ])?exception/i.test(reference) ||
      /publisher-controller/i.test(reference) ||
      attestation.teamId !== "WFTX6CN23F" ||
      attestation.sellerName !== "Ben Ebsworth" ||
      attestation.accountHolder !== "Ben Ebsworth"
    ) {
      fail(sellerCode);
    }
  }
  return attestation;
}

function eventKeys(event) {
  if (event.kind === "uploaded") {
    return [
      ...EVENT_BASE_KEYS,
      "appStoreConnectBuildId",
      "packageSha256",
      "productSourceCommit",
      "productSourceTree",
    ];
  }
  if (event.kind === "submitted") return [...EVENT_BASE_KEYS, "appStoreSubmissionReference"];
  if (event.kind === "approved" || event.kind === "rejected") {
    return [...EVENT_BASE_KEYS, "reviewReference", "submittedEventPath"];
  }
  if (event.kind === "expired") {
    const hasCandidate = "sourceReference" in event || "packageSha256" in event;
    return [
      ...EVENT_BASE_KEYS,
      "reason",
      ...(hasCandidate ? ["packageSha256", "sourceReference"] : []),
    ];
  }
  if (event.kind === "superseded") {
    const hasReplacement = "replacementVersion" in event || "replacementBuildNumber" in event;
    return [
      ...EVENT_BASE_KEYS,
      ...(hasReplacement ? ["replacementBuildNumber", "replacementVersion"] : []),
    ];
  }
  return [];
}

function submittedEventFilename(event) {
  return `events/${event.occurredAt.replaceAll(":", "-")}-submitted.json`;
}

export function validateReleaseLifecycleEvent(event, { priorEvents = [] } = {}) {
  const code = "RELEASE_LIFECYCLE_EVENT_INVALID";
  assertNoSecretKeys(event, code);
  assertNoSecretMaterial(event, code);
  if (!event || !LIFECYCLE_KINDS.has(event.kind)) fail(code);
  assertExactKeys(event, eventKeys(event), code);
  assertCommonRecord(event, "occurredAt", "operator", code);

  if (event.kind === "uploaded") {
    if (
      !SHA40.test(event.productSourceCommit) ||
      !SHA40.test(event.productSourceTree) ||
      !SHA256.test(event.packageSha256) ||
      !/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(event.appStoreConnectBuildId)
    ) {
      fail(code);
    }
  } else if (event.kind === "submitted") {
    if (!isSafeReference(event.appStoreSubmissionReference)) fail(code);
  } else if (event.kind === "approved" || event.kind === "rejected") {
    if (!isSafeReference(event.reviewReference)) fail(code);
    const submitted = priorEvents.find((prior) => {
      try {
        validateReleaseLifecycleEvent(prior);
      } catch {
        return false;
      }
      return (
        prior.kind === "submitted" &&
        prior.releaseVersion === event.releaseVersion &&
        prior.buildNumber === event.buildNumber &&
        prior.occurredAt < event.occurredAt &&
        submittedEventFilename(prior) === event.submittedEventPath
      );
    });
    if (!submitted) fail(code);
  } else if (event.kind === "expired") {
    if (
      !new Set(["certificate-expired", "profile-expired", "candidate-timeout"]).has(event.reason)
    ) {
      fail(code);
    }
    const hasSource = "sourceReference" in event;
    const hasPackage = "packageSha256" in event;
    if (hasSource !== hasPackage) fail(code);
    if (
      hasSource &&
      (!isSafeReference(event.sourceReference) || !SHA256.test(event.packageSha256))
    ) {
      fail(code);
    }
  } else if (event.kind === "superseded") {
    const hasVersion = "replacementVersion" in event;
    const hasBuild = "replacementBuildNumber" in event;
    if (hasVersion !== hasBuild) fail(code);
    if (
      hasVersion &&
      (!VERSION.test(event.replacementVersion) ||
        !BUILD.test(event.replacementBuildNumber) ||
        (event.replacementVersion === event.releaseVersion &&
          event.replacementBuildNumber === event.buildNumber))
    ) {
      fail(code);
    }
  }
  return event;
}

function candidateRequirementPassed(candidate, key, buildNumber) {
  if (!candidate || candidate.buildNumber !== buildNumber) return false;
  if (key === "sourceCommit" || key === "sourceTree") return SHA40.test(candidate[key]);
  if (key === "appSha256" || key === "packageSha256") return SHA256.test(candidate[key]);
  return candidate[key] === true;
}

function isCandidateEvidenceValid(candidate, releaseVersion, buildNumber) {
  try {
    assertNoSecretKeys(candidate, "MACOS_CANDIDATE_EVIDENCE_INVALID");
    assertNoSecretMaterial(candidate, "MACOS_CANDIDATE_EVIDENCE_INVALID");
    assertExactKeys(candidate, CANDIDATE_KEYS, "MACOS_CANDIDATE_EVIDENCE_INVALID");
    if (
      candidate.releaseVersion !== releaseVersion ||
      candidate.buildNumber !== buildNumber ||
      !SHA40.test(candidate.sourceCommit) ||
      !SHA40.test(candidate.sourceTree) ||
      !SHA256.test(candidate.appSha256) ||
      !SHA256.test(candidate.packageSha256) ||
      ![
        "signingProfilePassed",
        "publicUrlsMatch",
        "privacyEvidencePassed",
        "runtimeEgressEvidencePassed",
        "screenshotsLinked",
        "buildNumberReserved",
      ].every((key) => typeof candidate[key] === "boolean")
    ) {
      return false;
    }
    return true;
  } catch {
    return false;
  }
}

function blocker(code, owner, remediation) {
  return { code, owner, remediation };
}

function collectValidAttestations(attestations, releaseVersion, buildNumber) {
  const byKind = new Map();
  for (const candidateAttestation of Array.isArray(attestations) ? attestations : []) {
    try {
      validateReleaseAttestation(candidateAttestation);
      if (
        candidateAttestation.releaseVersion !== releaseVersion ||
        candidateAttestation.buildNumber !== buildNumber
      ) {
        continue;
      }
      const existing = byKind.get(candidateAttestation.kind) ?? [];
      existing.push(candidateAttestation);
      byKind.set(candidateAttestation.kind, existing);
    } catch {
      // Invalid or placeholder attestations can never satisfy readiness.
    }
  }
  return byKind;
}

function collectValidEvents(events, releaseVersion, buildNumber) {
  const valid = [];
  let previousTime = "";
  let sequenceInvalid = false;
  let uploadCount = 0;
  let uploadedSeen = false;
  let submittedSeen = false;
  let reviewed = false;
  let consumed = false;
  let terminalKind;
  for (const candidateEvent of Array.isArray(events) ? events : []) {
    try {
      validateReleaseLifecycleEvent(candidateEvent, { priorEvents: valid });
      if (
        candidateEvent.releaseVersion !== releaseVersion ||
        candidateEvent.buildNumber !== buildNumber
      ) {
        continue;
      }
      if (candidateEvent.occurredAt < previousTime) {
        sequenceInvalid = true;
        continue;
      }
      if (candidateEvent.kind === "uploaded") {
        uploadCount += 1;
        if (uploadedSeen || submittedSeen || consumed || reviewed) {
          sequenceInvalid = true;
          continue;
        }
        uploadedSeen = true;
      } else if (candidateEvent.kind === "submitted") {
        if (!uploadedSeen || submittedSeen || consumed || reviewed) {
          sequenceInvalid = true;
          continue;
        }
        submittedSeen = true;
        terminalKind = "submitted";
      } else if (candidateEvent.kind === "approved" || candidateEvent.kind === "rejected") {
        if (!submittedSeen || reviewed || consumed) {
          sequenceInvalid = true;
          continue;
        }
        reviewed = true;
        terminalKind = candidateEvent.kind;
      } else if (candidateEvent.kind === "expired" || candidateEvent.kind === "superseded") {
        if (submittedSeen || reviewed || consumed) {
          sequenceInvalid = true;
          continue;
        }
        consumed = true;
        terminalKind = candidateEvent.kind;
      }
      previousTime = candidateEvent.occurredAt;
      valid.push(candidateEvent);
    } catch {
      sequenceInvalid = true;
    }
  }
  return { events: valid, sequenceInvalid, terminalKind, uploadCount };
}

export function evaluateReleaseState(inputs) {
  const releaseVersion = inputs?.releaseVersion;
  const buildNumber = inputs?.buildNumber;
  const repository = inputs?.repository ?? {};
  const candidateEvidence = inputs?.candidate;
  const passed = [];
  const blockers = [];

  if (!VERSION.test(releaseVersion ?? "") || !BUILD.test(buildNumber ?? "")) {
    return {
      state: "NOT_READY",
      passed,
      blockers: [
        blocker(
          "RELEASE_IDENTITY_INVALID",
          "repository",
          "Provide a semantic release version and positive reserved build number.",
        ),
      ],
    };
  }

  let repositoryReady = true;
  for (const [key, code, remediation] of REPOSITORY_REQUIREMENTS) {
    if (repository[key] === true)
      passed.push(key.replace(/[A-Z]/g, (match) => `-${match.toLowerCase()}`));
    else {
      repositoryReady = false;
      blockers.push(blocker(code, "repository", remediation));
    }
  }

  const candidateEvidenceValid = isCandidateEvidenceValid(
    candidateEvidence,
    releaseVersion,
    buildNumber,
  );
  let candidateReady = repositoryReady && candidateEvidenceValid;
  if (repositoryReady) {
    if (!candidateEvidenceValid) {
      blockers.push(
        blocker(
          "CANDIDATE_EVIDENCE_INVALID",
          "candidate",
          "Provide exact version-and-build-bound candidate evidence without extra fields.",
        ),
      );
    } else {
      for (const [key, code, remediation] of CANDIDATE_REQUIREMENTS) {
        if (candidateRequirementPassed(candidateEvidence, key, buildNumber)) {
          passed.push(`candidate-${key.replace(/[A-Z]/g, (match) => `-${match.toLowerCase()}`)}`);
        } else {
          candidateReady = false;
          blockers.push(blocker(code, "candidate", remediation));
        }
      }
    }
  }

  const attestations = collectValidAttestations(inputs?.attestations, releaseVersion, buildNumber);
  let preUploadReady = candidateReady;
  if (candidateReady) {
    for (const kind of PRE_UPLOAD_KINDS) {
      if (attestations.get(kind)?.length === 1) passed.push(`attestation-${kind}`);
      else {
        preUploadReady = false;
        blockers.push(
          blocker(
            `ATTESTATION_${kind.toUpperCase().replaceAll("-", "_")}_MISSING`,
            "operator",
            `Record the accountable ${kind} attestation.`,
          ),
        );
      }
    }
  }

  const lifecycle = collectValidEvents(inputs?.events, releaseVersion, buildNumber);
  const { events } = lifecycle;
  const preSubmitAttestationsComplete = PRE_SUBMIT_KINDS.every(
    (kind) => attestations.get(kind)?.length === 1,
  );
  const submittedWithoutPrerequisites =
    ["submitted", "approved", "rejected"].includes(lifecycle.terminalKind) &&
    !preSubmitAttestationsComplete;
  const lifecycleSequenceInvalid = lifecycle.sequenceInvalid || submittedWithoutPrerequisites;
  const effectiveTerminalKind = submittedWithoutPrerequisites ? undefined : lifecycle.terminalKind;
  if (lifecycleSequenceInvalid) {
    blockers.push(
      blocker(
        "RELEASE_LIFECYCLE_SEQUENCE_INVALID",
        "repository",
        "Repair invalid or reordered lifecycle evidence without rewriting valid prior events.",
      ),
    );
  }
  if (lifecycle.uploadCount > 1) {
    blockers.push(
      blocker(
        "APP_STORE_UPLOAD_EVENT_AMBIGUOUS",
        "operator",
        "Retain one immutable uploaded event for the release build and investigate conflicts.",
      ),
    );
  }
  const exactUploads = events.filter(
    (event) =>
      event.kind === "uploaded" &&
      event.productSourceCommit === candidateEvidence?.sourceCommit &&
      event.productSourceTree === candidateEvidence?.sourceTree &&
      event.packageSha256 === candidateEvidence?.packageSha256,
  );
  const uploaded = preUploadReady && lifecycle.uploadCount === 1 && exactUploads.length === 1;
  if (preUploadReady && !uploaded) {
    if (lifecycle.uploadCount <= 1) {
      blockers.push(
        blocker(
          "APP_STORE_UPLOAD_NOT_RECORDED",
          "operator",
          "Upload the exact package and record App Store Connect's build identifier.",
        ),
      );
    }
  }
  if (uploaded) passed.push("app-store-upload");

  let preSubmitReady = uploaded;
  if (uploaded) {
    for (const kind of PRE_SUBMIT_KINDS) {
      if (attestations.get(kind)?.length === 1) passed.push(`attestation-${kind}`);
      else {
        preSubmitReady = false;
        blockers.push(
          blocker(
            `ATTESTATION_${kind.toUpperCase().replaceAll("-", "_")}_MISSING`,
            "operator",
            `Record the accountable ${kind} attestation.`,
          ),
        );
      }
    }
  }

  if (effectiveTerminalKind) {
    blockers.push(
      blocker(
        effectiveTerminalKind === "expired" || effectiveTerminalKind === "superseded"
          ? "BUILD_NUMBER_CONSUMED"
          : effectiveTerminalKind === "submitted"
            ? "APP_STORE_SUBMISSION_ALREADY_RECORDED"
            : "APP_STORE_REVIEW_OUTCOME_RECORDED",
        "operator",
        "This build has a terminal lifecycle event and is no longer in a readiness state.",
      ),
    );
  }
  const state =
    lifecycleSequenceInvalid || effectiveTerminalKind
      ? "NOT_READY"
      : preSubmitReady
        ? "PRE_SUBMIT_READY"
        : uploaded
          ? "UPLOADED"
          : preUploadReady
            ? "PRE_UPLOAD_READY"
            : candidateReady
              ? "CANDIDATE_READY"
              : repositoryReady
                ? "REPOSITORY_READY"
                : "NOT_READY";
  validateReleaseState(state);
  const passedSet = new Set(passed);
  const blockerCodes = new Set(blockers.map(({ code }) => code));
  if ([...passedSet].some((requirement) => blockerCodes.has(requirement))) {
    fail("RELEASE_STATE_INVALID");
  }
  return {
    state,
    passed: [...passedSet].sort(),
    blockers: blockers.sort((left, right) => left.code.localeCompare(right.code)),
  };
}
