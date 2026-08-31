import assert from "node:assert/strict";
import { execFile as nodeExecFile } from "node:child_process";
import { mkdir, mkdtemp, readdir, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import Ajv2020 from "ajv/dist/2020.js";

import {
  evaluateReleaseState,
  inspectReleaseRecordDurability,
  RELEASE_STATES,
  validateReleaseAttestation,
  validateReleaseLifecycleEvent,
  validateReleaseState,
} from "./macos-release-state.mjs";

const execFile = promisify(nodeExecFile);

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const releaseVersion = "0.1.0";
const buildNumber = "42";
const sourceCommit = "a".repeat(40);
const sourceTree = "b".repeat(40);
const packageSha256 = "c".repeat(64);
const secretShapedValue = "TOKEN=abcdefgh";

const repositoryRequirements = {
  storeIdentity: true,
  publicSite: true,
  metadata: true,
  platformIdentity: true,
  policy: true,
  schemas: true,
  screenshotDefinitions: true,
  tests: true,
};

const candidate = {
  releaseVersion,
  sourceCommit,
  sourceTree,
  buildNumber,
  appSha256: "d".repeat(64),
  packageSha256,
  buildNumberReserved: true,
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
    certificateClasses: ["Apple Development", "Apple Distribution", "Mac Installer Distribution"],
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
  assert.deepEqual(validateReleaseState({ state: "NOT_READY", passed: [], blockers: [] }), {
    state: "NOT_READY",
    passed: [],
    blockers: [],
  });
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
    () =>
      validateReleaseState({
        state: "NOT_READY",
        passed: ["PUBLIC_SITE_NOT_RECORDED"],
        blockers: [
          {
            code: "PUBLIC_SITE_NOT_RECORDED",
            owner: "repository",
            remediation: "Publish the site.",
          },
        ],
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
    { $ref: "#/$defs/releaseStateRecord" },
    { $ref: "#/$defs/releaseAttestation" },
    { $ref: "#/$defs/lifecycleEvent" },
  ]);
  assert.equal(schema.$defs.releaseAttestation.unevaluatedProperties, false);
  assert.equal(schema.$defs.lifecycleEvent.unevaluatedProperties, false);
  assert.equal(schema.$defs.releaseAttestation.oneOf.length, 10);
  assert.equal(schema.$defs.lifecycleEvent.oneOf.length, 6);
  assert.equal(schema.$defs.sellerEligibilityAttestation.oneOf.length, 2);
  assert.equal(
    schema.$defs.companyControllerAttestation.allOf[1].properties.outcome.const,
    "confirmed",
  );
  assert.deepEqual(schema.$defs.exportComplianceAttestation.allOf[1].properties.outcome.enum, [
    "exempt",
    "non-exempt",
  ]);
  assert.equal(schema.$defs.expiredEvent.allOf[1].required.includes("reason"), true);
  assert.deepEqual(schema.$defs.supersededEvent.allOf[1].dependentRequired, {
    replacementVersion: ["replacementBuildNumber"],
    replacementBuildNumber: ["replacementVersion"],
  });
  assert.equal(schema.$defs.redactedReference.oneOf.length, 2);
  assert.deepEqual(schema.$defs.releaseStateRecord.required, ["state", "passed", "blockers"]);

  const validate = new Ajv2020({ strict: true }).compile(schema);
  for (const valid of [
    { state: "NOT_READY", passed: [], blockers: [] },
    attestation("company-controller"),
    sellerAttestation(),
    uploadedEvent(),
  ]) {
    assert.equal(validate(valid), true, JSON.stringify(validate.errors));
  }
  for (const invalid of [
    attestation("company-controller", {
      accountablePerson: "sk_live_12345678901234567890",
    }),
    uploadedEvent({ operator: "sk_live_12345678901234567890" }),
    uploadedEvent({ appStoreConnectBuildId: "sk_live_12345678901234567890" }),
    attestation("company-controller", { accountablePerson: secretShapedValue }),
    sellerAttestation({ appleDeveloperIdentifierId: secretShapedValue }),
    sellerAttestation({ applicationGroup: secretShapedValue }),
    attestation("content-rights", { outcome: "confirmed" }),
    { ...attestation("company-controller"), sellerName: "Gamma Systems Pty Ltd" },
  ]) {
    assert.equal(validate(invalid), false, JSON.stringify(invalid));
  }
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
    assert.equal(
      result.blockers.some(({ owner }) => owner === "repository"),
      true,
    );
  }
});

test("requires every exact candidate evidence boundary before candidate readiness", () => {
  assert.equal(evaluateReleaseState(releaseInputs()).state, "CANDIDATE_READY");
  for (const requirement of [
    "sourceCommit",
    "sourceTree",
    "appSha256",
    "packageSha256",
    "buildNumberReserved",
    "signingProfilePassed",
    "publicUrlsMatch",
    "privacyEvidencePassed",
    "runtimeEgressEvidencePassed",
    "screenshotsLinked",
  ]) {
    const changed = {
      ...candidate,
      [requirement]:
        requirement.endsWith("Passed") ||
        requirement.endsWith("Match") ||
        requirement.endsWith("Linked")
          ? false
          : "",
    };
    assert.equal(
      evaluateReleaseState(releaseInputs({ candidate: changed })).state,
      "REPOSITORY_READY",
      requirement,
    );
  }
  for (const changed of [
    { ...candidate, releaseVersion: "0.2.0" },
    { ...candidate, extra: true },
    { ...candidate, apiToken: "redacted" },
  ]) {
    assert.equal(
      evaluateReleaseState(releaseInputs({ candidate: changed })).state,
      "REPOSITORY_READY",
    );
  }
});

test("requires candidate, attestation, and lifecycle facts to be durable on the trusted remote", () => {
  const candidateLocal = evaluateReleaseState(
    releaseInputs({
      durability: { attestationKinds: [], candidate: false, eventKinds: [] },
    }),
  );
  assert.equal(candidateLocal.state, "REPOSITORY_READY");
  assert.equal(
    candidateLocal.blockers.some(({ code }) => code === "CANDIDATE_RECORD_NOT_DURABLE"),
    true,
  );

  const attestationsLocal = evaluateReleaseState(
    releaseInputs({
      attestations: preUploadAttestations,
      durability: { attestationKinds: [], candidate: true, eventKinds: [] },
    }),
  );
  assert.equal(attestationsLocal.state, "CANDIDATE_READY");
  assert.equal(
    attestationsLocal.blockers.some(
      ({ code }) => code === "ATTESTATION_CONTENT_RIGHTS_RECORD_NOT_DURABLE",
    ),
    true,
  );

  const uploadLocal = evaluateReleaseState(
    releaseInputs({
      attestations: preUploadAttestations,
      durability: {
        attestationKinds: preUploadAttestations.map(({ kind }) => kind),
        candidate: true,
        eventKinds: [],
      },
      events: [uploadedEvent()],
    }),
  );
  assert.equal(uploadLocal.state, "PRE_UPLOAD_READY");
  assert.equal(
    uploadLocal.blockers.some(({ code }) => code === "APP_STORE_UPLOAD_RECORD_NOT_DURABLE"),
    true,
  );
});

test("derives candidate durability from clean records reachable on the trusted Git remote", async () => {
  const temporaryRoot = await mkdtemp(path.join(os.tmpdir(), "tammy-release-durability-"));
  const local = path.join(temporaryRoot, "local");
  const remote = path.join(temporaryRoot, "remote.git");
  const buildRoot = path.join(local, "docs/release/records/macos/0.1.0/build-42");
  const evidenceRoot = path.join(
    buildRoot,
    "evidence/candidate/018f3d8c-7b2a-7abc-8def-1234567890ab",
  );
  const eventsRoot = path.join(buildRoot, "events");
  const runGit = async (cwd, arguments_) =>
    execFile("/usr/bin/git", arguments_, { cwd, encoding: "utf8" });
  try {
    await mkdir(evidenceRoot, { recursive: true });
    await mkdir(eventsRoot, { recursive: true });
    await writeFile(path.join(evidenceRoot, "candidate.json"), `${JSON.stringify(candidate)}\n`);
    for (const name of [
      "metadata-snapshot.json",
      "privacy-evidence.json",
      "runtime-egress.json",
      "screenshots.json",
    ]) {
      await writeFile(path.join(evidenceRoot, name), "{}\n");
    }
    await writeFile(path.join(evidenceRoot, "summary.md"), "# Candidate\n");
    await writeFile(
      path.join(eventsRoot, "2026-08-30T10-00-00.000Z-candidate-built.json"),
      `${JSON.stringify({
        appSha256: candidate.appSha256,
        buildNumber,
        kind: "candidate-built",
        marketingVersion: releaseVersion,
        packageSha256,
        productSourceCommit: sourceCommit,
        productSourceTree: sourceTree,
      })}\n`,
    );
    await runGit(temporaryRoot, ["init", "--bare", remote]);
    await runGit(temporaryRoot, ["init", "-b", "main", local]);
    await runGit(local, ["config", "user.name", "Tammy Tests"]);
    await runGit(local, ["config", "user.email", "tests@tammy.invalid"]);

    assert.equal(
      (await inspectReleaseRecordDurability({ repositoryRoot: local, buildRoot })).candidate,
      false,
    );
    await runGit(local, ["add", "."]);
    await runGit(local, ["commit", "-m", "candidate"]);
    await runGit(local, ["remote", "add", "origin", remote]);
    assert.equal(
      (await inspectReleaseRecordDurability({ repositoryRoot: local, buildRoot })).candidate,
      false,
    );

    await runGit(local, ["push", "-u", "origin", "main"]);
    assert.equal(
      (await inspectReleaseRecordDurability({ repositoryRoot: local, buildRoot })).candidate,
      true,
    );

    const unexpectedEvidence = path.join(evidenceRoot, "operator-notes.txt");
    await writeFile(unexpectedEvidence, "not part of the exact evidence schema\n");
    assert.equal(
      (await inspectReleaseRecordDurability({ repositoryRoot: local, buildRoot })).candidate,
      false,
    );
    await rm(unexpectedEvidence);

    await writeFile(path.join(evidenceRoot, "summary.md"), "# Updated candidate\n");
    assert.equal(
      (await inspectReleaseRecordDurability({ repositoryRoot: local, buildRoot })).candidate,
      false,
    );
    await runGit(local, ["add", "."]);
    await runGit(local, ["commit", "-m", "update candidate summary"]);
    assert.equal(
      (await inspectReleaseRecordDurability({ repositoryRoot: local, buildRoot })).candidate,
      false,
    );
    await runGit(local, ["push", "origin", "main"]);
    assert.equal(
      (await inspectReleaseRecordDurability({ repositoryRoot: local, buildRoot })).candidate,
      true,
    );
  } finally {
    await rm(temporaryRoot, { force: true, recursive: true });
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
  ]) {
    assert.equal(evaluateReleaseState({ ...ready, events: [event] }).state, "PRE_UPLOAD_READY");
  }
  assert.equal(
    evaluateReleaseState({
      ...ready,
      events: [uploadedEvent({ appStoreConnectBuildId: "" })],
    }).state,
    "NOT_READY",
  );
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
      () =>
        validateReleaseAttestation(
          attestation(kind, {
            outcome: outcome === "confirmed" ? "owned" : "confirmed",
          }),
        ),
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
    { ...valid, evidenceReference: "sk_live_12345678901234567890" },
    { ...valid, accountablePerson: secretShapedValue },
    { ...valid, releaseVersion: "0.2" },
    { ...valid, buildNumber: "042" },
    { ...valid, confirmedAt: "not-a-time" },
  ]) {
    assert.throws(() => validateReleaseAttestation(invalid), /ATTESTATION_INVALID/);
  }
  for (const field of ["appleDeveloperIdentifierId", "applicationGroup"]) {
    assert.throws(
      () => validateReleaseAttestation(sellerAttestation({ [field]: secretShapedValue })),
      /ATTESTATION_INVALID/,
    );
  }
});

test("validates seller eligibility from the verified membership branch, not historical Team ID", () => {
  assert.deepEqual(validateReleaseAttestation(sellerAttestation()), sellerAttestation());
  const convertedMembership = sellerAttestation({
    teamId: "WFTX6CN23F",
    appleDeveloperIdentifierId: "WFTX6CN23F.com.tammy.desktop",
    applicationGroup: "WFTX6CN23F.com.tammy.desktop",
  });
  assert.deepEqual(validateReleaseAttestation(convertedMembership), convertedMembership);
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
    () =>
      validateReleaseAttestation({
        ...exception,
        writtenAppleExceptionReference: "../../../../../authority/publisher-controller.json",
      }),
    /SELLER_ELIGIBILITY_INVALID/,
  );
  assert.throws(
    () =>
      validateReleaseAttestation({
        ...exception,
        teamId: "ZZZZZZZZZZ",
        sellerName: "Other Person",
        accountHolder: "Other Person",
        appleDeveloperIdentifierId: "ZZZZZZZZZZ.com.tammy.desktop",
        applicationGroup: "ZZZZZZZZZZ.com.tammy.desktop",
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
  assert.deepEqual(validateReleaseLifecycleEvent(approved, { priorEvents: [submitted] }), approved);
  assert.throws(
    () => validateReleaseLifecycleEvent(approved, { priorEvents: [] }),
    /LIFECYCLE_EVENT_INVALID/,
  );
  assert.throws(
    () =>
      validateReleaseLifecycleEvent(approved, {
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
  const result = evaluateReleaseState(
    releaseInputs({
      attestations: preUploadAttestations,
      events: [
        uploadedEvent(),
        uploadedEvent({
          occurredAt: "2026-08-30T11:01:00.000Z",
          appStoreConnectBuildId: "1234567891",
        }),
      ],
    }),
  );
  assert.equal(result.state, "NOT_READY");
  assert.equal(
    result.blockers.some(({ code }) => code === "APP_STORE_UPLOAD_EVENT_AMBIGUOUS"),
    true,
  );
});

test("a conflicting second upload is ambiguous even when it targets different bytes", () => {
  const result = evaluateReleaseState(
    releaseInputs({
      attestations: preUploadAttestations,
      events: [
        uploadedEvent(),
        uploadedEvent({
          occurredAt: "2026-08-30T11:01:00.000Z",
          appStoreConnectBuildId: "1234567891",
          packageSha256: "f".repeat(64),
        }),
      ],
    }),
  );
  assert.equal(result.state, "NOT_READY");
  assert.equal(
    result.blockers.some(({ code }) => code === "APP_STORE_UPLOAD_EVENT_AMBIGUOUS"),
    true,
  );
});

test("reordered lifecycle events are an explicit non-passing sequence", () => {
  const result = evaluateReleaseState(
    releaseInputs({
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
    }),
  );
  assert.equal(result.state, "NOT_READY");
  assert.equal(
    result.blockers.some(({ code }) => code === "RELEASE_LIFECYCLE_SEQUENCE_INVALID"),
    true,
  );
});

test("terminal lifecycle events consume readiness and submitted cannot precede upload", () => {
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
  const submitted = {
    schemaVersion: 1,
    kind: "submitted",
    releaseVersion,
    buildNumber,
    operator: "Ben Ebsworth",
    occurredAt: "2026-08-30T12:00:00.000Z",
    appStoreSubmissionReference: "apple/submission-123.json",
  };
  for (const terminalEvent of [expired, superseded, submitted]) {
    const result = evaluateReleaseState(
      releaseInputs({
        attestations: [...preUploadAttestations, ...preSubmitAttestations],
        events: [uploadedEvent(), terminalEvent],
      }),
    );
    assert.equal(result.state, "NOT_READY", terminalEvent.kind);
    assert.equal(
      result.blockers.some(({ code }) =>
        ["BUILD_NUMBER_CONSUMED", "APP_STORE_SUBMISSION_ALREADY_RECORDED"].includes(code),
      ),
      true,
      terminalEvent.kind,
    );
  }

  const submittedBeforeUpload = evaluateReleaseState(
    releaseInputs({
      attestations: preUploadAttestations,
      events: [{ ...submitted, occurredAt: "2026-08-30T10:00:00.000Z" }, uploadedEvent()],
    }),
  );
  assert.equal(submittedBeforeUpload.state, "NOT_READY");
  assert.equal(
    submittedBeforeUpload.blockers.some(
      ({ code }) => code === "RELEASE_LIFECYCLE_SEQUENCE_INVALID",
    ),
    true,
  );

  const submissionWithoutDeclarations = evaluateReleaseState(
    releaseInputs({
      attestations: preUploadAttestations,
      events: [uploadedEvent(), submitted],
    }),
  );
  assert.equal(submissionWithoutDeclarations.state, "NOT_READY");
  assert.equal(
    submissionWithoutDeclarations.blockers.some(
      ({ code }) => code === "RELEASE_LIFECYCLE_SEQUENCE_INVALID",
    ),
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
