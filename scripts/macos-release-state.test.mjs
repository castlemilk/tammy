import assert from "node:assert/strict";
import { readFile, readdir } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  RELEASE_STATES,
  evaluateReleaseState,
  validateReleaseAttestation,
  validateReleaseLifecycleEvent,
  validateReleaseState,
} from "./macos-release-state.mjs";

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const releaseVersion = "0.1.0";
const buildNumber = "42";
const sourceCommit = "a".repeat(40);
const sourceTree = "b".repeat(40);
const packageSha256 = "c".repeat(64);

const repositoryRequirements = {
  storeIdentity: true,
  publicSite: true,
  metadata: true,
  platformIdentity: true,
  policy: true,
  schemas: true,
  tests: true,
};

const candidate = {
  sourceCommit,
  sourceTree,
  buildNumber,
  appSha256: "d".repeat(64),
  packageSha256,
  signingProfilePassed: true,
  publicUrlsMatch: true,
  privacyEvidencePassed: true,
  runtimeEgressEvidencePassed: true,
  screenshotsLinked: true,
};

const outcomes = {
  "company-controller": "confirmed",
  "content-rights": "owned",
  "export-compliance": "exempt",
  "pricing-availability": "confirmed",
  "privacy-answer": "no-data-collected-no-tracking",
  "age-rating": "completed",
  "processed-build": "selected",
  "metadata-assets-entered": "entered",
  "app-store-warning-review": "clear",
};

function attestation(kind, overrides = {}) {
  return {
    schemaVersion: 1,
    kind,
    releaseVersion,
    buildNumber,
    accountablePerson: "Ben Ebsworth",
    confirmedAt: "2026-08-30T10:00:00.000Z",
    evidenceReference:
      kind === "company-controller"
        ? "../../../../../authority/publisher-controller.json"
        : `apple/${kind}.png`,
    outcome: outcomes[kind],
    ...overrides,
  };
}

function sellerAttestation(overrides = {}) {
  return {
    ...attestation("seller-eligibility", { outcome: "eligible" }),
    eligibilityBranch: "company-organization",
    teamId: "ABCDEFGHIJ",
    sellerName: "Gamma Systems Pty Ltd",
    accountHolder: "Ben Ebsworth",
    activeAgreements: true,
    appId: "com.tammy.desktop",
    appleDeveloperIdentifierId: "ABCDEFGHIJ.com.tammy.desktop",
    appStoreConnectId: "1234567890",
    applicationGroup: "ABCDEFGHIJ.com.tammy.desktop",
    helperIdentifiers: [
      "com.tammy.desktop.helper",
      "com.tammy.desktop.helper.GPU",
      "com.tammy.desktop.helper.Plugin",
      "com.tammy.desktop.helper.Renderer",
    ],
    certificateClasses: [
      "Apple Development",
      "Apple Distribution",
      "Mac Installer Distribution",
    ],
    profilesReissued: true,
    ...overrides,
  };
}

function uploadedEvent(overrides = {}) {
  return {
    schemaVersion: 1,
    kind: "uploaded",
    releaseVersion,
    buildNumber,
    operator: "Ben Ebsworth",
    occurredAt: "2026-08-30T11:00:00.000Z",
    productSourceCommit: sourceCommit,
    productSourceTree: sourceTree,
    packageSha256,
    appStoreConnectBuildId: "1234567890",
    ...overrides,
  };
}

function releaseInputs(overrides = {}) {
  return {
    releaseVersion,
    buildNumber,
    repository: repositoryRequirements,
    candidate,
    attestations: [],
    events: [],
    ...overrides,
  };
}

const preUploadAttestations = [
  attestation("company-controller"),
  sellerAttestation(),
  attestation("content-rights"),
  attestation("export-compliance"),
  attestation("pricing-availability"),
  attestation("privacy-answer"),
];

const preSubmitAttestations = [
  attestation("processed-build"),
  attestation("metadata-assets-entered"),
  attestation("age-rating"),
  attestation("app-store-warning-review"),
];

test("exposes only the monotonic readiness states", () => {
  assert.deepEqual(RELEASE_STATES, [
    "NOT_READY",
    "REPOSITORY_READY",
    "CANDIDATE_READY",
    "PRE_UPLOAD_READY",
    "UPLOADED",
    "PRE_SUBMIT_READY",
  ]);
  for (const state of RELEASE_STATES) assert.equal(validateReleaseState(state), state);
  for (const lifecycleKind of [
    "uploaded",
    "expired",
    "submitted",
    "approved",
    "rejected",
    "superseded",
  ]) {
    assert.throws(() => validateReleaseState(lifecycleKind), /RELEASE_STATE_INVALID/);
  }
  assert.throws(
    () => validateReleaseState({
      state: "NOT_READY",
      passed: ["PUBLIC_SITE_NOT_RECORDED"],
      blockers: [{
        code: "PUBLIC_SITE_NOT_RECORDED",
        owner: "repository",
        remediation: "Publish the site.",
      }],
    }),
    /RELEASE_STATE_INVALID/,
  );
});

test("documents readiness, attestation, and lifecycle records in the release schema", async () => {
  const schema = JSON.parse(
    await readFile(
      path.join(repositoryRoot, "apps/desktop/release/macos/release-state.schema.json"),
      "utf8",
    ),
  );
  assert.deepEqual(schema.$defs.readinessState.enum, RELEASE_STATES);
  assert.deepEqual(schema.$defs.attestationKind.enum, [
    "company-controller",
    "seller-eligibility",
    "content-rights",
    "export-compliance",
    "pricing-availability",
    "privacy-answer",
    "age-rating",
    "processed-build",
    "metadata-assets-entered",
    "app-store-warning-review",
  ]);
  assert.deepEqual(schema.$defs.lifecycleKind.enum, [
    "uploaded",
    "expired",
    "superseded",
    "submitted",
    "approved",
    "rejected",
  ]);
  assert.deepEqual(schema.oneOf, [
    { "$ref": "#/$defs/releaseStateRecord" },
    { "$ref": "#/$defs/releaseAttestation" },
    { "$ref": "#/$defs/lifecycleEvent" },
  ]);
  assert.equal(schema.$defs.releaseAttestation.unevaluatedProperties, false);
  assert.equal(schema.$defs.lifecycleEvent.unevaluatedProperties, false);
});

test("derives readiness without requiring signing credentials for repository readiness", () => {
  const repositoryReady = evaluateReleaseState(releaseInputs({ candidate: null }));
  assert.equal(repositoryReady.state, "REPOSITORY_READY");
  assert.equal(JSON.stringify(repositoryReady).includes("credential"), false);

  for (const requirement of Object.keys(repositoryRequirements)) {
    const result = evaluateReleaseState(
      releaseInputs({
        repository: { ...repositoryRequirements, [requirement]: false },
        candidate: null,
      }),
    );
    assert.equal(result.state, "NOT_READY", requirement);
    assert.equal(result.blockers.some(({ owner }) => owner === "repository"), true);
  }
});

test("requires every exact candidate evidence boundary before candidate readiness", () => {
  assert.equal(evaluateReleaseState(releaseInputs()).state, "CANDIDATE_READY");
  for (const requirement of [
    "sourceCommit",
    "sourceTree",
    "appSha256",
    "packageSha256",
    "signingProfilePassed",
    "publicUrlsMatch",
    "privacyEvidencePassed",
    "runtimeEgressEvidencePassed",
    "screenshotsLinked",
  ]) {
    const changed = { ...candidate, [requirement]: requirement.endsWith("Passed") || requirement.endsWith("Match") || requirement.endsWith("Linked") ? false : "" };
    assert.equal(
      evaluateReleaseState(releaseInputs({ candidate: changed })).state,
      "REPOSITORY_READY",
      requirement,
    );
  }
});

test("requires all accountable pre-upload attestations and no App Store build ID", () => {
  assert.equal(
    evaluateReleaseState(releaseInputs({ attestations: preUploadAttestations })).state,
    "PRE_UPLOAD_READY",
  );
  for (const missing of preUploadAttestations.map(({ kind }) => kind)) {
    const result = evaluateReleaseState(
      releaseInputs({
        attestations: preUploadAttestations.filter(({ kind }) => kind !== missing),
      }),
    );
    assert.equal(result.state, "CANDIDATE_READY", missing);
  }
});

test("derives uploaded only from an exact candidate-bound uploaded event", () => {
  const ready = releaseInputs({
    attestations: preUploadAttestations,
    events: [uploadedEvent()],
  });
  assert.equal(evaluateReleaseState(ready).state, "UPLOADED");

  for (const event of [
    uploadedEvent({ packageSha256: "e".repeat(64) }),
    uploadedEvent({ productSourceCommit: "e".repeat(40) }),
    uploadedEvent({ productSourceTree: "e".repeat(40) }),
    uploadedEvent({ appStoreConnectBuildId: "" }),
  ]) {
    assert.equal(
      evaluateReleaseState({ ...ready, events: [event] }).state,
      "PRE_UPLOAD_READY",
    );
  }
});

test("requires processed build and declaration assets before pre-submit readiness", () => {
  const inputs = releaseInputs({
    attestations: [...preUploadAttestations, ...preSubmitAttestations],
    events: [uploadedEvent()],
  });
  assert.equal(evaluateReleaseState(inputs).state, "PRE_SUBMIT_READY");
  for (const missing of preSubmitAttestations.map(({ kind }) => kind)) {
    const result = evaluateReleaseState({
      ...inputs,
      attestations: inputs.attestations.filter(({ kind }) => kind !== missing),
    });
    assert.equal(result.state, "UPLOADED", missing);
  }
});

test("validates exact redacted common attestation schemas and kind-specific outcomes", () => {
  for (const [kind, outcome] of Object.entries(outcomes)) {
    assert.deepEqual(validateReleaseAttestation(attestation(kind)), attestation(kind));
    assert.throws(
      () => validateReleaseAttestation(attestation(kind, {
        outcome: outcome === "confirmed" ? "owned" : "confirmed",
      })),
      /ATTESTATION_INVALID/,
    );
    assert.equal(outcome, attestation(kind).outcome);
  }
  assert.deepEqual(
    validateReleaseAttestation(attestation("export-compliance", { outcome: "non-exempt" })),
    attestation("export-compliance", { outcome: "non-exempt" }),
  );
  assert.deepEqual(
    validateReleaseAttestation(attestation("app-store-warning-review", { outcome: "resolved" })),
    attestation("app-store-warning-review", { outcome: "resolved" }),
  );
});

test("rejects unknown fields, secrets, free-form blobs, unsafe references, and release drift", () => {
  const valid = attestation("company-controller");
  for (const invalid of [
    { ...valid, notes: "free-form" },
    { ...valid, apiToken: "redacted" },
    { ...valid, privateKeyReference: "redacted" },
    { ...valid, evidenceReference: "/absolute/path" },
    { ...valid, evidenceReference: "C:\\private\\evidence.json" },
    { ...valid, evidenceReference: "https://user:password@example.com/evidence" },
    { ...valid, evidenceReference: "https://example.com/evidence?token=value" },
    { ...valid, releaseVersion: "0.2" },
    { ...valid, buildNumber: "042" },
    { ...valid, confirmedAt: "not-a-time" },
  ]) {
    assert.throws(() => validateReleaseAttestation(invalid), /ATTESTATION_INVALID/);
  }
});

test("validates both seller eligibility branches and rejects the individual team as company-owned", () => {
  assert.deepEqual(validateReleaseAttestation(sellerAttestation()), sellerAttestation());
  assert.throws(
    () => validateReleaseAttestation(sellerAttestation({ teamId: "WFTX6CN23F" })),
    /SELLER_ELIGIBILITY_INVALID/,
  );
  assert.throws(
    () => validateReleaseAttestation(sellerAttestation({ sellerName: "Ben Ebsworth" })),
    /SELLER_ELIGIBILITY_INVALID/,
  );

  const exception = sellerAttestation({
    eligibilityBranch: "written-apple-exception",
    teamId: "WFTX6CN23F",
    sellerName: "Ben Ebsworth",
    appleDeveloperIdentifierId: "WFTX6CN23F.com.tammy.desktop",
    applicationGroup: "WFTX6CN23F.com.tammy.desktop",
    writtenAppleExceptionReference: "apple/written-exception.pdf",
  });
  assert.deepEqual(validateReleaseAttestation(exception), exception);
  assert.throws(
    () => validateReleaseAttestation({ ...exception, writtenAppleExceptionReference: undefined }),
    /SELLER_ELIGIBILITY_INVALID/,
  );
  assert.throws(
    () => validateReleaseAttestation({
      ...exception,
      writtenAppleExceptionReference: "../../../../../authority/publisher-controller.json",
    }),
    /SELLER_ELIGIBILITY_INVALID/,
  );
});

test("validates strict immutable lifecycle events", () => {
  const submitted = {
    schemaVersion: 1,
    kind: "submitted",
    releaseVersion,
    buildNumber,
    operator: "Ben Ebsworth",
    occurredAt: "2026-08-30T12:00:00.000Z",
    appStoreSubmissionReference: "apple/submission-123.json",
  };
  const approved = {
    schemaVersion: 1,
    kind: "approved",
    releaseVersion,
    buildNumber,
    operator: "Ben Ebsworth",
    occurredAt: "2026-08-30T13:00:00.000Z",
    reviewReference: "apple/review-123.json",
    submittedEventPath: "events/2026-08-30T12-00-00.000Z-submitted.json",
  };
  const expired = {
    schemaVersion: 1,
    kind: "expired",
    releaseVersion,
    buildNumber,
    operator: "Ben Ebsworth",
    occurredAt: "2026-08-30T12:00:00.000Z",
    reason: "candidate-timeout",
    sourceReference: "candidate/evidence.json",
    packageSha256,
  };
  const superseded = {
    schemaVersion: 1,
    kind: "superseded",
    releaseVersion,
    buildNumber,
    operator: "Ben Ebsworth",
    occurredAt: "2026-08-30T12:00:00.000Z",
    replacementVersion: "0.1.1",
    replacementBuildNumber: "43",
  };

  for (const event of [uploadedEvent(), submitted, expired, superseded]) {
    assert.deepEqual(validateReleaseLifecycleEvent(event), event);
  }
  assert.deepEqual(
    validateReleaseLifecycleEvent(approved, { priorEvents: [submitted] }),
    approved,
  );
  assert.throws(
    () => validateReleaseLifecycleEvent(approved, { priorEvents: [] }),
    /LIFECYCLE_EVENT_INVALID/,
  );
  assert.throws(
    () => validateReleaseLifecycleEvent(approved, {
      priorEvents: [{ ...submitted, apiToken: "redacted" }],
    }),
    /LIFECYCLE_EVENT_INVALID/,
  );
  for (const invalid of [
    { ...uploadedEvent(), token: "redacted" },
    { ...expired, reason: "other" },
    { ...superseded, replacementVersion: releaseVersion, replacementBuildNumber: buildNumber },
    { ...submitted, appStoreSubmissionReference: "/absolute/path" },
  ]) {
    assert.throws(() => validateReleaseLifecycleEvent(invalid), /LIFECYCLE_EVENT_INVALID/);
  }
});

test("sorts redacted blockers and never lets lifecycle events skip readiness prerequisites", () => {
  const result = evaluateReleaseState(
    releaseInputs({
      repository: { ...repositoryRequirements, metadata: false, publicSite: false },
      candidate: null,
      events: [uploadedEvent()],
    }),
  );
  assert.equal(result.state, "NOT_READY");
  assert.deepEqual(
    result.blockers.map(({ code }) => code),
    [...result.blockers.map(({ code }) => code)].sort(),
  );
  assert.equal(JSON.stringify(result).includes(packageSha256), false);
  assert.equal(JSON.stringify(result).includes("TAMMY_MACOS_"), false);
});

test("duplicate upload events cannot advance readiness", () => {
  const result = evaluateReleaseState(releaseInputs({
    attestations: preUploadAttestations,
    events: [
      uploadedEvent(),
      uploadedEvent({
        occurredAt: "2026-08-30T11:01:00.000Z",
        appStoreConnectBuildId: "1234567891",
      }),
    ],
  }));
  assert.equal(result.state, "PRE_UPLOAD_READY");
  assert.equal(
    result.blockers.some(({ code }) => code === "APP_STORE_UPLOAD_EVENT_AMBIGUOUS"),
    true,
  );
});

test("reordered lifecycle events are an explicit non-passing sequence", () => {
  const result = evaluateReleaseState(releaseInputs({
    attestations: preUploadAttestations,
    events: [
      {
        schemaVersion: 1,
        kind: "submitted",
        releaseVersion,
        buildNumber,
        operator: "Ben Ebsworth",
        occurredAt: "2026-08-30T12:00:00.000Z",
        appStoreSubmissionReference: "apple/submission-123.json",
      },
      uploadedEvent(),
    ],
  }));
  assert.equal(result.state, "PRE_UPLOAD_READY");
  assert.equal(
    result.blockers.some(({ code }) => code === "RELEASE_LIFECYCLE_SEQUENCE_INVALID"),
    true,
  );
});

test("attestation templates are explicitly non-passing operator guidance", async () => {
  const templateRoot = path.join(
    repositoryRoot,
    "docs/release/records/macos/0.1.0/attestation-templates",
  );
  const templates = (await readdir(templateRoot)).filter((name) => name.endsWith(".example.json"));
  assert.equal(templates.length, 10);
  for (const name of templates) {
    const source = await readFile(path.join(templateRoot, name), "utf8");
    assert.match(source, /OPERATOR_REQUIRED/);
    assert.throws(() => validateReleaseAttestation(JSON.parse(source)), /ATTESTATION_INVALID/);
  }
  const guidance = await readFile(
    path.join(repositoryRoot, "docs/release/records/macos/0.1.0/README.md"),
    "utf8",
  );
  assert.match(guidance, /agents cannot self-attest/i);
  assert.match(guidance, /Apple (?:screen|document)/i);
  assert.match(guidance, /\.example\.json/);
});
