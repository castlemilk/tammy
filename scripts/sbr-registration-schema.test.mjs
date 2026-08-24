import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
  createHash,
  createPrivateKey,
  createPublicKey,
  generateKeyPairSync,
  sign,
} from "node:crypto";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  MAX_COMPONENT_MANIFEST_BYTES,
  parseAndValidateSbrComponentManifest,
} from "./sbr-component-schema.mjs";
import {
  canonicalizeSbrProfile,
  MAX_PROFILE_BYTES,
  parseAndValidateSbrProfile,
  verifySbrProfileSignature,
} from "./sbr-profile-schema.mjs";
import {
  authenticateSbrEvteRegistration,
  canonicalizeSbrEndpointProfile,
  canonicalizeSbrRegistrationManifest,
  evaluateSbrRegistrationReadiness,
  MAX_SBR_EVIDENCE_BYTES,
  parseAndValidateSbrEndpointProfile,
  parseAndValidateSbrRegistrationManifest,
  verifySbrRegistrationSignature,
} from "./sbr-registration-schema.mjs";

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const NOW = new Date("2026-08-21T12:00:00Z");
const HASH_A = "a".repeat(64);
const HASH_B = "b".repeat(64);
const TEST_SEED = Buffer.from(
  "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60",
  "hex",
);
const TEST_PRIVATE_KEY = createPrivateKey({
  format: "der",
  key: Buffer.concat([Buffer.from("302e020100300506032b657004220420", "hex"), TEST_SEED]),
  type: "pkcs8",
});
const TEST_PUBLIC_KEY = createPublicKey(TEST_PRIVATE_KEY).export({ format: "pem", type: "spki" });

function sha256(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

function endpointProfile(overrides = {}) {
  return {
    schema_version: 1,
    environment: "EVTE",
    profile_id: "evte-profile-fixture",
    revision: 7,
    issued_at: "2026-01-01T00:00:00Z",
    expires_at: "2027-01-01T00:00:00Z",
    services: [
      {
        service_id: "au.gov.ato.sbr.bas",
        endpoint_id: "bas-submit-v1",
        endpoint_url: "https://bas.evte.invalid/sbr/v1",
        tls_server_name: "bas.evte.invalid",
        certificate_sha256: HASH_A,
      },
    ],
    ...overrides,
  };
}

function registrationManifest(overrides = {}) {
  return {
    schema_version: 1,
    environment: "EVTE",
    target: "darwin/arm64",
    product_id_scope: {
      product_identifier: "product.evte.invalid",
      service_id: "au.gov.ato.sbr.bas",
    },
    dsp_registration: {
      state: "APPROVED",
      external_reference: "DSP-EVTE-0001",
      decision_date: "2026-01-02",
      expires_at: "2027-01-01T00:00:00Z",
    },
    product_registration: {
      state: "APPROVED",
      external_reference: "PRODUCT-EVTE-0001",
      decision_date: "2026-01-03",
      expires_at: null,
    },
    osf_assessment: {
      category: "OSF-CATEGORY-A",
      state: "APPROVED",
      external_reference: "OSF-EVTE-0001",
      decision_date: "2026-01-04",
      revalidation_date: "2027-01-01",
    },
    component: {
      name: "tammy-sbr-helper",
      version: "0.1.0-fixture",
      component_manifest_sha256: HASH_A,
      licence_state: "APPROVED",
      target: "darwin/arm64",
    },
    services: [
      {
        service_id: "au.gov.ato.sbr.bas",
        taxonomy_version: "2026.1",
        release_version: "v1",
        artefact_sha256s: [HASH_A, HASH_B],
        enrolment_state: "APPROVED",
        conformance_state: "PASSED",
      },
    ],
    evte_access: {
      state: "APPROVED",
      external_reference: "EVTE-ACCESS-0001",
      issued_at: "2026-01-01T00:00:00Z",
      expires_at: "2027-01-01T00:00:00Z",
    },
    endpoint_profile: {
      id: "evte-profile-fixture",
      revision: 7,
      endpoint_profile_sha256: HASH_B,
      issued_at: "2026-01-01T00:00:00Z",
      expires_at: "2027-01-01T00:00:00Z",
    },
    review: {
      reviewer_identity: "reviewer@tammy.invalid",
      approved_at: "2026-01-05T00:00:00Z",
      revalidation_date: "2027-01-01",
    },
    ...overrides,
  };
}

test("parses and freezes the exact Product ID scope bound to one registration service", () => {
  const parsed = parseAndValidateSbrRegistrationManifest(encode(registrationManifest()), {
    now: NOW,
  });
  assert.deepEqual(parsed.manifest.product_id_scope, {
    product_identifier: "product.evte.invalid",
    service_id: "au.gov.ato.sbr.bas",
  });
  assert.equal(Object.isFrozen(parsed.manifest.product_id_scope), true);

  const base = registrationManifest();
  const cases = [
    ["FIELDS", Object.fromEntries(Object.entries(base).filter(([key]) => key !== "product_id_scope"))],
    ["PRODUCT_ID_SCOPE_FIELDS", { ...base, product_id_scope: { ...base.product_id_scope, extra: true } }],
    ["PRODUCT_IDENTIFIER", { ...base, product_id_scope: { ...base.product_id_scope, product_identifier: "" } }],
    ["PRODUCT_SERVICE_ID", { ...base, product_id_scope: { ...base.product_id_scope, service_id: "" } }],
    ["PRODUCT_SERVICE_MISMATCH", { ...base, product_id_scope: { ...base.product_id_scope, service_id: "other.service" } }],
  ];
  for (const [code, manifest] of cases) {
    assert.throws(
      () => parseAndValidateSbrRegistrationManifest(encode(manifest), { now: NOW }),
      (error) => error?.message === `SBR_REGISTRATION_INVALID:${code}`,
    );
  }
});

function componentManifest() {
  return {
    schema_version: 1,
    component_name: "tammy-sbr-helper",
    component_version: "0.1.0-fixture",
    target: "darwin/arm64",
    files: [{ path: "bin/helper", byte_length: 0, sha256: sha256(Buffer.alloc(0)) }],
  };
}

function profileFor({ componentHash, endpointHash, registrationHash }) {
  return {
    schema_version: 1,
    environment: "EVTE",
    target: "darwin/arm64",
    helper_sha256: "c".repeat(64),
    component_manifest_sha256: componentHash,
    registration_manifest_sha256: registrationHash,
    endpoint_profile_sha256: endpointHash,
    issued_at: "2026-01-01T00:00:00Z",
    expires_at: "2027-01-01T00:00:00Z",
  };
}

function encode(value) {
  return Buffer.from(JSON.stringify(value));
}

function revokedBytes(bytes) {
  const revoked = Proxy.revocable(bytes, {});
  revoked.revoke();
  return revoked.proxy;
}

function registrationWithCanonicalSize(targetBytes) {
  const services = Array.from({ length: 128 }, (_, index) => ({
    ...registrationManifest().services[0],
    service_id: `service-${String(index).padStart(3, "0")}`,
    taxonomy_version: "t",
    release_version: "r",
    artefact_sha256s: [String(index + 1).padStart(64, "0")],
  }));
  const manifest = registrationManifest({
    product_id_scope: {
      ...registrationManifest().product_id_scope,
      service_id: services[0].service_id,
    },
    services,
  });
  let size = Buffer.byteLength(JSON.stringify(manifest));
  let nextHash = services.length + 1;
  let serviceIndex = 0;
  while (size + 67 <= targetBytes) {
    const service = services[serviceIndex % services.length];
    if (service.artefact_sha256s.length < 128) {
      service.artefact_sha256s.push(String(nextHash).padStart(64, "0"));
      nextHash += 1;
      size += 67;
    }
    serviceIndex += 1;
    assert.ok(serviceIndex < 128 * 128 * 2, "registration fixture has enough bounded capacity");
  }
  let remaining = targetBytes - size;
  for (const service of services) {
    if (remaining === 0) break;
    const addition = Math.min(127, remaining);
    service.taxonomy_version += "x".repeat(addition);
    remaining -= addition;
  }
  assert.equal(remaining, 0);
  assert.equal(Buffer.byteLength(JSON.stringify(manifest)), targetBytes);
  return manifest;
}

function endpointProfileWithCanonicalSize(targetBytes) {
  const services = Array.from({ length: 128 }, (_, index) => ({
    ...endpointProfile().services[0],
    service_id: `service-${String(index).padStart(3, "0")}`,
    endpoint_id: `endpoint-${String(index).padStart(3, "0")}`,
    endpoint_url: `https://s${index}.evte.invalid/`,
    tls_server_name: `s${index}.evte.invalid`,
  }));
  const profile = endpointProfile({ services });
  let remaining = targetBytes - Buffer.byteLength(JSON.stringify(profile));
  assert.ok(remaining >= 0);
  for (const service of services) {
    if (remaining === 0) break;
    const capacity = 2_048 - Buffer.byteLength(service.endpoint_url);
    const addition = Math.min(capacity, remaining);
    service.endpoint_url += "x".repeat(addition);
    remaining -= addition;
  }
  assert.equal(remaining, 0);
  assert.equal(Buffer.byteLength(JSON.stringify(profile)), targetBytes);
  return profile;
}

function registrationSignature(manifest) {
  return `${sign(null, canonicalizeSbrRegistrationManifest(manifest), TEST_PRIVATE_KEY).toString("base64")}\n`;
}

function profileSignature(profile) {
  return `${sign(null, canonicalizeSbrProfile(profile, { now: NOW }), TEST_PRIVATE_KEY).toString("base64")}\n`;
}

function makeBoundEvidence() {
  const parsedEndpoint = parseAndValidateSbrEndpointProfile(encode(endpointProfile()), {
    now: NOW,
  });
  const parsedComponent = parseAndValidateSbrComponentManifest(encode(componentManifest()));
  const registration = registrationManifest({
    component: {
      ...registrationManifest().component,
      component_manifest_sha256: parsedComponent.sha256,
    },
    endpoint_profile: {
      ...registrationManifest().endpoint_profile,
      endpoint_profile_sha256: parsedEndpoint.sha256,
    },
  });
  const parsedRegistration = parseAndValidateSbrRegistrationManifest(encode(registration), {
    now: NOW,
  });
  const profile = profileFor({
    componentHash: parsedComponent.sha256,
    endpointHash: parsedEndpoint.sha256,
    registrationHash: parsedRegistration.sha256,
  });
  return {
    componentManifestBytes: encode(componentManifest()),
    endpointProfileBytes: encode(endpointProfile()),
    profileBytes: encode(profile),
    profileSignatureBytes: profileSignature(profile),
    publicKey: TEST_PUBLIC_KEY,
    registrationBytes: encode(registration),
    registrationSignatureBytes: registrationSignature(registration),
  };
}

function makeBoundEvidenceForServices({ endpointServices, registrationServices }) {
  const endpoint = endpointProfile({ services: endpointServices });
  const parsedEndpoint = parseAndValidateSbrEndpointProfile(encode(endpoint), { now: NOW });
  const parsedComponent = parseAndValidateSbrComponentManifest(encode(componentManifest()));
  const registration = registrationManifest({
    component: {
      ...registrationManifest().component,
      component_manifest_sha256: parsedComponent.sha256,
    },
    endpoint_profile: {
      ...registrationManifest().endpoint_profile,
      endpoint_profile_sha256: parsedEndpoint.sha256,
    },
    services: registrationServices,
  });
  const parsedRegistration = parseAndValidateSbrRegistrationManifest(encode(registration), {
    now: NOW,
  });
  const profile = profileFor({
    componentHash: parsedComponent.sha256,
    endpointHash: parsedEndpoint.sha256,
    registrationHash: parsedRegistration.sha256,
  });
  return {
    componentManifestBytes: encode(componentManifest()),
    endpointProfileBytes: encode(endpoint),
    profileBytes: encode(profile),
    profileSignatureBytes: profileSignature(profile),
    publicKey: TEST_PUBLIC_KEY,
    registrationBytes: encode(registration),
    registrationSignatureBytes: registrationSignature(registration),
  };
}

test("parses the exact registration schema and emits stable canonical bytes and hash", () => {
  const parsed = parseAndValidateSbrRegistrationManifest(encode(registrationManifest()), {
    now: NOW,
  });

  assert.deepEqual(parsed.manifest, registrationManifest());
  assert.equal(
    parsed.canonicalBytes.toString(),
    canonicalizeSbrRegistrationManifest(parsed.manifest).toString(),
  );
  assert.equal(parsed.sha256, sha256(parsed.canonicalBytes));
  assert.deepEqual(
    evaluateSbrRegistrationReadiness({
      manifest: parsed.manifest,
      now: NOW,
      phase: "POST_CONFORMANCE",
    }),
    {
      ready: true,
      code: "READY_POST_CONFORMANCE",
    },
  );
});

test("bounds canonical registration evidence for direct objects, parsed wrappers, and crypto/readiness", () => {
  const exact = registrationWithCanonicalSize(MAX_SBR_EVIDENCE_BYTES);
  const parsed = parseAndValidateSbrRegistrationManifest(encode(exact), { now: NOW });
  assert.equal(parsed.canonicalBytes.byteLength, MAX_SBR_EVIDENCE_BYTES);
  assert.equal(canonicalizeSbrRegistrationManifest(exact).byteLength, MAX_SBR_EVIDENCE_BYTES);
  assert.equal(canonicalizeSbrRegistrationManifest(parsed).byteLength, MAX_SBR_EVIDENCE_BYTES);

  const oversized = registrationWithCanonicalSize(MAX_SBR_EVIDENCE_BYTES + 1);
  for (const manifest of [oversized, { manifest: oversized }]) {
    assert.throws(
      () => canonicalizeSbrRegistrationManifest(manifest),
      (error) => error?.message === "SBR_REGISTRATION_INVALID:EVIDENCE_TOO_LARGE",
    );
    assert.throws(
      () =>
        evaluateSbrRegistrationReadiness({
          manifest,
          now: NOW,
          phase: "POST_CONFORMANCE",
        }),
      (error) => error?.message === "SBR_REGISTRATION_INVALID:EVIDENCE_TOO_LARGE",
    );
    assert.throws(
      () =>
        evaluateSbrRegistrationReadiness({
          manifest,
          now: new Date(Number.NaN),
          phase: "INVALID",
        }),
      (error) => error?.message === "SBR_REGISTRATION_INVALID:EVIDENCE_TOO_LARGE",
    );
    assert.throws(
      () =>
        verifySbrRegistrationSignature({
          manifest,
          publicKey: TEST_PUBLIC_KEY,
          signature: "not-base64",
        }),
      (error) => error?.message === "SBR_REGISTRATION_INVALID:EVIDENCE_TOO_LARGE",
    );
  }
});

test("bounds canonical endpoint evidence for direct objects and parsed wrappers", () => {
  const exact = endpointProfileWithCanonicalSize(MAX_SBR_EVIDENCE_BYTES);
  const parsed = parseAndValidateSbrEndpointProfile(encode(exact), { now: NOW });
  assert.equal(parsed.canonicalBytes.byteLength, MAX_SBR_EVIDENCE_BYTES);
  assert.equal(canonicalizeSbrEndpointProfile(exact).byteLength, MAX_SBR_EVIDENCE_BYTES);
  assert.equal(canonicalizeSbrEndpointProfile(parsed).byteLength, MAX_SBR_EVIDENCE_BYTES);

  const oversized = endpointProfileWithCanonicalSize(MAX_SBR_EVIDENCE_BYTES + 1);
  for (const profile of [oversized, { profile: oversized }]) {
    assert.throws(
      () => canonicalizeSbrEndpointProfile(profile),
      (error) => error?.message === "SBR_REGISTRATION_INVALID:EVIDENCE_TOO_LARGE",
    );
  }
});

test("pre-conformance readiness accepts an enrolled service that has not started conformance", () => {
  const manifest = registrationManifest({
    services: [{ ...registrationManifest().services[0], conformance_state: "NOT_STARTED" }],
  });
  const parsed = parseAndValidateSbrRegistrationManifest(encode(manifest), { now: NOW });

  assert.deepEqual(
    evaluateSbrRegistrationReadiness({
      manifest: parsed.manifest,
      now: NOW,
      phase: "PRE_CONFORMANCE",
    }),
    {
      ready: true,
      code: "READY_PRE_CONFORMANCE",
    },
  );
  assert.deepEqual(
    evaluateSbrRegistrationReadiness({
      manifest: parsed.manifest,
      now: NOW,
      phase: "POST_CONFORMANCE",
    }),
    {
      ready: false,
      code: "SERVICE_CONFORMANCE_NOT_PASSED",
    },
  );
  assert.equal(parsed.manifest.services[0].conformance_state, "NOT_STARTED");
});

test("snapshots trusted clock milliseconds without calling overridable Date methods", () => {
  class HostileDate extends Date {
    getTime() {
      throw new Error("sensitive getTime override");
    }

    toISOString() {
      throw new Error("sensitive toISOString override");
    }
  }
  const now = new HostileDate(Date.prototype.getTime.call(NOW));
  const parsed = parseAndValidateSbrRegistrationManifest(encode(registrationManifest()), { now });

  assert.deepEqual(
    evaluateSbrRegistrationReadiness({
      manifest: parsed,
      now,
      phase: "POST_CONFORMANCE",
    }),
    { ready: true, code: "READY_POST_CONFORMANCE" },
  );
  assert.deepEqual(
    authenticateSbrEvteRegistration({
      ...makeBoundEvidence(),
      now,
      phase: "POST_CONFORMANCE",
    }).readiness,
    { ready: false, code: "EVTE_TRUST_ROOT_UNREGISTERED" },
  );
});

test("uses captured Date intrinsics after ambient Date methods are monkeypatched", () => {
  const getTimeDescriptor = Object.getOwnPropertyDescriptor(Date.prototype, "getTime");
  const toISOStringDescriptor = Object.getOwnPropertyDescriptor(Date.prototype, "toISOString");
  const parseDescriptor = Object.getOwnPropertyDescriptor(Date, "parse");
  const evidence = makeBoundEvidence();
  try {
    Object.defineProperty(Date.prototype, "getTime", {
      configurable: true,
      value: () => {
        throw new Error("sensitive ambient getTime");
      },
      writable: true,
    });
    Object.defineProperty(Date.prototype, "toISOString", {
      configurable: true,
      value: () => "1900-01-01T00:00:00Z",
      writable: true,
    });
    Object.defineProperty(Date, "parse", {
      configurable: true,
      value: () => {
        throw new Error("sensitive ambient parse");
      },
      writable: true,
    });

    const registration = parseAndValidateSbrRegistrationManifest(encode(registrationManifest()), {
      now: NOW,
    });
    assert.doesNotThrow(() =>
      parseAndValidateSbrEndpointProfile(encode(endpointProfile()), { now: NOW }),
    );
    assert.deepEqual(
      evaluateSbrRegistrationReadiness({
        manifest: registration,
        now: NOW,
        phase: "POST_CONFORMANCE",
      }),
      { ready: true, code: "READY_POST_CONFORMANCE" },
    );
    assert.deepEqual(
      authenticateSbrEvteRegistration({
        ...evidence,
        now: NOW,
        phase: "POST_CONFORMANCE",
      }).readiness,
      { ready: false, code: "EVTE_TRUST_ROOT_UNREGISTERED" },
    );
  } finally {
    Object.defineProperty(Date.prototype, "getTime", getTimeDescriptor);
    Object.defineProperty(Date.prototype, "toISOString", toISOStringDescriptor);
    Object.defineProperty(Date, "parse", parseDescriptor);
  }
});

test("reports deterministic readiness failures without mutating signed input", () => {
  const base = registrationManifest();
  const cases = [
    [
      "DSP_REGISTRATION_NOT_APPROVED",
      {
        dsp_registration: {
          state: "NOT_STARTED",
          external_reference: null,
          decision_date: null,
          expires_at: null,
        },
      },
    ],
    [
      "DSP_REGISTRATION_EXPIRED",
      { dsp_registration: { ...base.dsp_registration, expires_at: "2026-08-21T11:59:59Z" } },
    ],
    [
      "PRODUCT_REGISTRATION_NOT_APPROVED",
      {
        product_registration: {
          state: "SUBMITTED",
          external_reference: "PRODUCT-EVTE-0001",
          decision_date: null,
          expires_at: null,
        },
      },
    ],
    [
      "OSF_ASSESSMENT_NOT_APPROVED",
      {
        osf_assessment: {
          ...base.osf_assessment,
          state: "IN_REVIEW",
          decision_date: null,
          revalidation_date: null,
        },
      },
    ],
    [
      "OSF_REVALIDATION_EXPIRED",
      { osf_assessment: { ...base.osf_assessment, revalidation_date: "2026-08-20" } },
    ],
    [
      "COMPONENT_LICENCE_NOT_APPROVED",
      { component: { ...base.component, licence_state: "REVIEW_REQUIRED" } },
    ],
    [
      "SERVICE_ENROLMENT_NOT_APPROVED",
      {
        services: [
          { ...base.services[0], enrolment_state: "SUBMITTED", conformance_state: "NOT_STARTED" },
        ],
      },
    ],
    [
      "EVTE_ACCESS_NOT_APPROVED",
      {
        evte_access: {
          state: "REQUESTED",
          external_reference: "EVTE-ACCESS-0001",
          issued_at: null,
          expires_at: null,
        },
      },
    ],
    [
      "EVTE_ACCESS_EXPIRED",
      { evte_access: { ...base.evte_access, expires_at: "2026-08-21T11:59:59Z" } },
    ],
    [
      "ENDPOINT_PROFILE_EXPIRED",
      { endpoint_profile: { ...base.endpoint_profile, expires_at: "2026-08-21T11:59:59Z" } },
    ],
    [
      "REVIEW_REVALIDATION_EXPIRED",
      { review: { ...base.review, revalidation_date: "2026-08-20" } },
    ],
  ];

  for (const [code, overrides] of cases) {
    const parsed = parseAndValidateSbrRegistrationManifest(
      encode(registrationManifest(overrides)),
      { now: NOW },
    );
    const before = JSON.stringify(parsed.manifest);
    assert.deepEqual(
      evaluateSbrRegistrationReadiness({
        manifest: parsed.manifest,
        now: NOW,
        phase: "POST_CONFORMANCE",
      }),
      { ready: false, code },
    );
    assert.equal(JSON.stringify(parsed.manifest), before);
  }
});

test("uses stable registration readiness precedence independent of object key order", () => {
  const base = registrationManifest();
  const failures = {
    component: { component: { ...base.component, licence_state: "REVIEW_REQUIRED" } },
    dsp: {
      dsp_registration: {
        state: "NOT_STARTED",
        external_reference: null,
        decision_date: null,
        expires_at: null,
      },
    },
    product: {
      product_registration: {
        state: "NOT_STARTED",
        external_reference: null,
        decision_date: null,
        expires_at: null,
      },
    },
    osf: {
      osf_assessment: {
        ...base.osf_assessment,
        state: "IN_REVIEW",
        decision_date: null,
        revalidation_date: null,
      },
    },
    access: {
      evte_access: {
        state: "REQUESTED",
        external_reference: "EVTE-ACCESS-0001",
        issued_at: null,
        expires_at: null,
      },
    },
    endpoint: {
      endpoint_profile: {
        ...base.endpoint_profile,
        expires_at: "2026-08-21T11:59:59Z",
      },
    },
    enrolment: {
      services: [
        {
          ...base.services[0],
          enrolment_state: "SUBMITTED",
          conformance_state: "NOT_STARTED",
        },
      ],
    },
    conformance: {
      services: [{ ...base.services[0], conformance_state: "RUNNING" }],
    },
    enrolmentAndConformance: {
      services: [
        {
          ...base.services[0],
          enrolment_state: "SUBMITTED",
          conformance_state: "NOT_STARTED",
        },
        {
          ...base.services[0],
          service_id: "z.service",
          conformance_state: "RUNNING",
        },
      ],
    },
  };
  const cases = [
    ["COMPONENT_LICENCE_NOT_APPROVED", failures.component, failures.dsp],
    ["DSP_REGISTRATION_NOT_APPROVED", failures.dsp, failures.product],
    ["PRODUCT_REGISTRATION_NOT_APPROVED", failures.product, failures.osf],
    ["OSF_ASSESSMENT_NOT_APPROVED", failures.osf, failures.access],
    ["EVTE_ACCESS_NOT_APPROVED", failures.access, failures.endpoint],
    ["ENDPOINT_PROFILE_EXPIRED", failures.endpoint, failures.enrolment],
    ["SERVICE_ENROLMENT_NOT_APPROVED", failures.enrolmentAndConformance, {}],
  ];

  for (const [code, first, second] of cases) {
    const manifest = registrationManifest({ ...first, ...second });
    for (const ordered of [manifest, Object.fromEntries(Object.entries(manifest).reverse())]) {
      const parsed = parseAndValidateSbrRegistrationManifest(encode(ordered), { now: NOW });
      assert.deepEqual(
        evaluateSbrRegistrationReadiness({
          manifest: parsed,
          now: NOW,
          phase: "POST_CONFORMANCE",
        }),
        { ready: false, code },
      );
    }
  }
});

test("rejects unknown, missing, duplicate, escaped-alias, malformed, and oversized registration input", () => {
  const valid = JSON.stringify(registrationManifest());
  const missing = registrationManifest();
  delete missing.target;
  const deeplyNested = `${'{"extra":'.repeat(33)}null${"}".repeat(33)}`;
  const invalidInputs = [
    encode({ ...registrationManifest(), extra: true }),
    encode(missing),
    valid.replace('{"schema_version":1', '{"schema_version":1,"schema_version":1'),
    valid.replace('"environment":"EVTE"', '"environment":"EVTE","environ\\u006dent":"EVTE"'),
    Buffer.from([0xc3, 0x28]),
    JSON.stringify({ ...registrationManifest(), target: "\ud800" }),
    deeplyNested,
  ];
  for (const raw of invalidInputs) {
    assert.throws(
      () => parseAndValidateSbrRegistrationManifest(raw, { now: NOW }),
      /SBR_REGISTRATION_INVALID/,
    );
  }
  assert.throws(
    () =>
      parseAndValidateSbrRegistrationManifest(Buffer.alloc(MAX_SBR_EVIDENCE_BYTES + 1, 0x20), {
        now: NOW,
      }),
    /SBR_REGISTRATION_INVALID:INPUT_TOO_LARGE/,
  );
});

test("rejects invalid registration types, enums, dates, hashes, and state transitions", () => {
  const base = registrationManifest();
  const invalid = [
    { schema_version: 2 },
    { environment: ["PRO", "DUCTION"].join("") },
    { target: "linux/arm64" },
    { dsp_registration: { ...base.dsp_registration, state: "UNKNOWN" } },
    {
      dsp_registration: {
        state: "NOT_STARTED",
        external_reference: "claim",
        decision_date: null,
        expires_at: null,
      },
    },
    {
      dsp_registration: {
        state: "SUBMITTED",
        external_reference: null,
        decision_date: null,
        expires_at: null,
      },
    },
    { dsp_registration: { ...base.dsp_registration, decision_date: null } },
    { product_registration: { ...base.product_registration, decision_date: "2026-02-30" } },
    { osf_assessment: { ...base.osf_assessment, category: "" } },
    { osf_assessment: { ...base.osf_assessment, state: "APPROVED", revalidation_date: null } },
    { osf_assessment: { ...base.osf_assessment, state: "APPROVED", external_reference: null } },
    { osf_assessment: { ...base.osf_assessment, state: "APPROVED", decision_date: null } },
    { component: { ...base.component, name: "../helper" } },
    { component: { ...base.component, version: "version with spaces" } },
    { component: { ...base.component, component_manifest_sha256: "A".repeat(64) } },
    { component: { ...base.component, target: "linux/arm64" } },
    { component: { ...base.component, licence_state: "LICENSED" } },
    { evte_access: { ...base.evte_access, issued_at: "2026-01-01T00:00:00.000Z" } },
    { evte_access: { ...base.evte_access, expires_at: "2025-01-01T00:00:00Z" } },
    { evte_access: { ...base.evte_access, state: "APPROVED", external_reference: null } },
    { evte_access: { ...base.evte_access, state: "APPROVED", issued_at: null } },
    { evte_access: { ...base.evte_access, state: "APPROVED", expires_at: null } },
    { endpoint_profile: { ...base.endpoint_profile, revision: 0 } },
    { endpoint_profile: { ...base.endpoint_profile, endpoint_profile_sha256: "a".repeat(63) } },
    { review: { ...base.review, reviewer_identity: "\n" } },
    { review: { ...base.review, approved_at: "2026-01-05T11:00:00+11:00" } },
  ];
  for (const overrides of invalid) {
    assert.throws(
      () =>
        parseAndValidateSbrRegistrationManifest(encode(registrationManifest(overrides)), {
          now: NOW,
        }),
      /SBR_REGISTRATION_INVALID/,
    );
  }
});

test("requires sorted unique services and sorted unique lowercase artefact hashes", () => {
  const baseService = registrationManifest().services[0];
  const second = { ...baseService, service_id: "z.service", artefact_sha256s: [HASH_A] };
  const invalidServices = [
    [],
    [second, baseService],
    [baseService, { ...baseService }],
    [{ ...baseService, artefact_sha256s: [] }],
    [{ ...baseService, artefact_sha256s: [HASH_B, HASH_A] }],
    [{ ...baseService, artefact_sha256s: [HASH_A, HASH_A] }],
    [{ ...baseService, artefact_sha256s: ["A".repeat(64)] }],
    [{ ...baseService, enrolment_state: "SUBMITTED", conformance_state: "RUNNING" }],
    [{ ...baseService, enrolment_state: "NOT_STARTED", conformance_state: "PASSED" }],
  ];
  for (const services of invalidServices) {
    assert.throws(
      () =>
        parseAndValidateSbrRegistrationManifest(encode(registrationManifest({ services })), {
          now: NOW,
        }),
      /SBR_REGISTRATION_INVALID/,
    );
  }
});

test("parses only the exact bounded EVTE endpoint profile and returns canonical bytes", () => {
  const parsed = parseAndValidateSbrEndpointProfile(encode(endpointProfile()), { now: NOW });
  assert.deepEqual(parsed.profile, endpointProfile());
  assert.equal(parsed.sha256, sha256(parsed.canonicalBytes));

  const valid = JSON.stringify(endpointProfile());
  const missing = endpointProfile();
  delete missing.profile_id;
  const baseService = endpointProfile().services[0];
  const invalidInputs = [
    encode({ ...endpointProfile(), extra: true }),
    encode(missing),
    valid.replace('{"schema_version":1', '{"schema_version":1,"schema_version":1'),
    encode({ ...endpointProfile(), environment: ["PRO", "DUCTION"].join("") }),
    encode({ ...endpointProfile(), revision: 0 }),
    encode({ ...endpointProfile(), issued_at: "2026-01-01T00:00:00.000Z" }),
    encode({ ...endpointProfile(), expires_at: "2025-01-01T00:00:00Z" }),
    encode({ ...endpointProfile(), services: [] }),
    encode({ ...endpointProfile(), services: [{ ...baseService, extra: true }] }),
    encode({
      ...endpointProfile(),
      services: [{ ...baseService, endpoint_url: "http://bas.evte.invalid/sbr" }],
    }),
    encode({
      ...endpointProfile(),
      services: [{ ...baseService, endpoint_url: "https://user:pass@bas.evte.invalid/sbr" }],
    }),
    encode({
      ...endpointProfile(),
      services: [{ ...baseService, endpoint_url: "https://bas.evte.invalid/sbr?x=y" }],
    }),
    encode({
      ...endpointProfile(),
      services: [{ ...baseService, endpoint_url: "https://bas.evte.invalid/sbr#fragment" }],
    }),
    encode({
      ...endpointProfile(),
      services: [{ ...baseService, endpoint_url: "https://bas.evte.invalid:443/sbr" }],
    }),
    encode({
      ...endpointProfile(),
      services: [{ ...baseService, endpoint_url: "https://bas.evte.invalid:8443/sbr" }],
    }),
    encode({
      ...endpointProfile(),
      services: [{ ...baseService, endpoint_url: "https://bas.evte.invalid/sbr%0d%0aheader" }],
    }),
    encode({
      ...endpointProfile(),
      services: [{ ...baseService, endpoint_url: "https://bas.evte.invalid/sbr%00tail" }],
    }),
    encode({
      ...endpointProfile(),
      services: [{ ...baseService, endpoint_url: "https://bas.evte.invalid/sbr%2f%2e%2e/x" }],
    }),
    encode({
      ...endpointProfile(),
      services: [{ ...baseService, endpoint_url: "https://bas.evte.invalid/sbr%2F%2E%2e/x" }],
    }),
    encode({
      ...endpointProfile(),
      services: [{ ...baseService, endpoint_url: "https://bas.evte.invalid/sbr%5c..%5Cx" }],
    }),
    encode({
      ...endpointProfile(),
      services: [{ ...baseService, endpoint_url: "https://bas.evte.invalid/sbr%252f%252e%252e/x" }],
    }),
    encode({
      ...endpointProfile(),
      services: [{ ...baseService, endpoint_url: "https:\\evil.invalid\\sbr" }],
    }),
    encode({
      ...endpointProfile(),
      services: [{ ...baseService, tls_server_name: "bas.evte.invalid:443" }],
    }),
    encode({
      ...endpointProfile(),
      services: [{ ...baseService, certificate_sha256: "A".repeat(64) }],
    }),
    encode({ ...endpointProfile(), services: [{ ...baseService }, { ...baseService }] }),
    encode({ ...endpointProfile(), services: [{ ...baseService, service_id: "z" }, baseService] }),
  ];
  for (const raw of invalidInputs) {
    assert.throws(
      () => parseAndValidateSbrEndpointProfile(raw, { now: NOW }),
      /SBR_REGISTRATION_INVALID/,
    );
  }
});

test("verifies strict detached Ed25519 registration signatures using only caller-pinned SPKI", () => {
  const manifest = registrationManifest();
  const signature = registrationSignature(manifest);
  assert.equal(
    verifySbrRegistrationSignature({ manifest, publicKey: TEST_PUBLIC_KEY, signature }),
    true,
  );

  const otherKey = generateKeyPairSync("ed25519").publicKey.export({ format: "pem", type: "spki" });
  const privatePem = TEST_PRIVATE_KEY.export({ format: "pem", type: "pkcs8" });
  for (const [publicKey, malformedSignature] of [
    [otherKey, signature],
    [TEST_PUBLIC_KEY, "not-base64"],
    [TEST_PUBLIC_KEY, `${signature}\n`],
    [privatePem, signature],
  ]) {
    assert.throws(
      () => verifySbrRegistrationSignature({ manifest, publicKey, signature: malformedSignature }),
      /SBR_REGISTRATION_INVALID/,
    );
  }
});

test("rejects prototype pollution and hostile accessors without exposing accessor errors", () => {
  const baseline = canonicalizeSbrRegistrationManifest(registrationManifest());
  const previous = Object.getOwnPropertyDescriptor(Array.prototype, "reduce");
  try {
    Object.defineProperty(Array.prototype, "reduce", {
      configurable: true,
      value: () => "polluted",
    });
    assert.deepEqual(canonicalizeSbrRegistrationManifest(registrationManifest()), baseline);
  } finally {
    Object.defineProperty(Array.prototype, "reduce", previous);
  }

  const hostile = registrationManifest();
  Object.defineProperty(hostile, "services", {
    enumerable: true,
    get: () => {
      throw new Error("sensitive accessor detail");
    },
  });
  assert.throws(
    () => canonicalizeSbrRegistrationManifest(hostile),
    (error) => error?.message === "SBR_REGISTRATION_INVALID:CANONICALIZATION",
  );

  const throwingArray = new Proxy([], {
    getOwnPropertyDescriptor() {
      throw new Error("sensitive array descriptor detail");
    },
  });
  const hostileRegistration = registrationManifest({ services: throwingArray });
  const hostileEndpoint = endpointProfile({ services: throwingArray });
  for (const operation of [
    () => canonicalizeSbrRegistrationManifest(hostileRegistration),
    () =>
      evaluateSbrRegistrationReadiness({
        manifest: hostileRegistration,
        now: NOW,
        phase: "POST_CONFORMANCE",
      }),
    () => canonicalizeSbrEndpointProfile(hostileEndpoint),
  ]) {
    assert.throws(
      operation,
      (error) =>
        error?.message === "SBR_REGISTRATION_INVALID:CANONICALIZATION" &&
        !error.message.includes("sensitive"),
    );
  }

  const revokedObject = Proxy.revocable({}, {});
  revokedObject.revoke();
  const revokedArray = Proxy.revocable([], {});
  revokedArray.revoke();
  const revokedObjectRegistration = registrationManifest({
    dsp_registration: revokedObject.proxy,
  });
  const revokedArrayRegistration = registrationManifest({ services: revokedArray.proxy });
  for (const manifest of [revokedObjectRegistration, revokedArrayRegistration]) {
    for (const operation of [
      () => canonicalizeSbrRegistrationManifest(manifest),
      () =>
        evaluateSbrRegistrationReadiness({
          manifest,
          now: NOW,
          phase: "POST_CONFORMANCE",
        }),
      () =>
        verifySbrRegistrationSignature({
          manifest,
          publicKey: TEST_PUBLIC_KEY,
          signature: "not-base64",
        }),
    ]) {
      assert.throws(
        operation,
        (error) => error?.message === "SBR_REGISTRATION_INVALID:CANONICALIZATION",
      );
    }
  }

  const revokedEndpointObject = Proxy.revocable({}, {});
  revokedEndpointObject.revoke();
  const revokedEndpointArray = Proxy.revocable([], {});
  revokedEndpointArray.revoke();
  for (const profile of [
    endpointProfile({ services: [revokedEndpointObject.proxy] }),
    endpointProfile({ services: revokedEndpointArray.proxy }),
  ]) {
    assert.throws(
      () => canonicalizeSbrEndpointProfile(profile),
      (error) => error?.message === "SBR_REGISTRATION_INVALID:CANONICALIZATION",
    );
  }

  const evidence = makeBoundEvidence();
  const revokedBytes = Proxy.revocable(evidence.registrationBytes, {});
  revokedBytes.revoke();
  assert.throws(
    () =>
      authenticateSbrEvteRegistration({
        ...evidence,
        now: NOW,
        phase: "POST_CONFORMANCE",
        registrationBytes: revokedBytes.proxy,
      }),
    (error) => error?.message === "SBR_REGISTRATION_INVALID:CANONICALIZATION",
  );
});

function assertDeeplyRedactedAndFrozen(value) {
  assert.equal(value instanceof Uint8Array, false);
  if (value === null || typeof value !== "object") {
    if (typeof value === "string") assert.equal(value.includes("https://"), false);
    return;
  }
  assert.equal(Object.isFrozen(value), true);
  for (const [key, child] of Object.entries(value)) {
    assert.equal(/(?:manifest|canonical|bytes|url)/i.test(key), false);
    assertDeeplyRedactedAndFrozen(child);
  }
}

test("rejects a caller-self-signed bundle at the code-owned unregistered trust root", () => {
  const evidence = makeBoundEvidence();
  const result = authenticateSbrEvteRegistration({
    ...evidence,
    now: NOW,
    phase: "POST_CONFORMANCE",
  });
  const registration = parseAndValidateSbrRegistrationManifest(evidence.registrationBytes, {
    now: NOW,
  });
  const endpoint = parseAndValidateSbrEndpointProfile(evidence.endpointProfileBytes, { now: NOW });
  const component = parseAndValidateSbrComponentManifest(evidence.componentManifestBytes);

  assert.deepEqual(result, {
    readiness: { ready: false, code: "EVTE_TRUST_ROOT_UNREGISTERED" },
    fingerprints: {
      component_sha256: component.sha256,
      endpoint_sha256: endpoint.sha256,
      registration_sha256: registration.sha256,
    },
    metadata: {
      environment: "EVTE",
      target: "darwin/arm64",
      endpoint_id: "evte-profile-fixture",
      endpoint_revision: 7,
    },
  });
  assertDeeplyRedactedAndFrozen(result);
  assert.throws(() => {
    result.readiness.code = "READY_POST_CONFORMANCE";
  }, TypeError);
});

test("normalizes revoked proxies for every high-level evidence byte field", () => {
  const evidence = makeBoundEvidence();
  const cases = [
    ["registrationBytes", /^SBR_REGISTRATION_INVALID:CANONICALIZATION$/],
    ["endpointProfileBytes", /^SBR_REGISTRATION_INVALID:CANONICALIZATION$/],
    ["componentManifestBytes", /^SBR_COMPONENT_INVALID:CANONICALIZATION$/],
    ["profileBytes", /^SBR_PROFILE_INVALID:CANONICALIZATION$/],
    ["registrationSignatureBytes", /^SBR_REGISTRATION_INVALID:CANONICALIZATION$/],
    ["profileSignatureBytes", /^SBR_PROFILE_INVALID:CANONICALIZATION$/],
  ];

  for (const [field, expected] of cases) {
    const input = { ...evidence, [field]: revokedBytes(Buffer.from(evidence[field])) };
    assert.throws(
      () =>
        authenticateSbrEvteRegistration({
          ...input,
          now: NOW,
          phase: "POST_CONFORMANCE",
        }),
      (error) =>
        expected.test(error?.message) && !/(?:getPrototypeOf|revoked|native)/i.test(error.message),
    );
  }
});

test("normalizes incompatible and revoked byte proxies across schema boundaries", () => {
  const proxyBytes = (bytes) => new Proxy(bytes, {});
  assert.throws(
    () => parseAndValidateSbrRegistrationManifest(proxyBytes(encode(registrationManifest()))),
    (error) => error?.message === "SBR_REGISTRATION_INVALID:CANONICALIZATION",
  );
  assert.throws(
    () => parseAndValidateSbrComponentManifest(revokedBytes(encode(componentManifest()))),
    (error) => error?.message === "SBR_COMPONENT_INVALID:CANONICALIZATION",
  );
  assert.throws(
    () => parseAndValidateSbrComponentManifest(proxyBytes(encode(componentManifest()))),
    (error) => error?.message === "SBR_COMPONENT_INVALID:CANONICALIZATION",
  );
  const profileBytes = encode(
    profileFor({ componentHash: HASH_A, endpointHash: HASH_A, registrationHash: HASH_A }),
  );
  assert.throws(
    () => parseAndValidateSbrProfile(revokedBytes(profileBytes), { now: NOW }),
    (error) => error?.message === "SBR_PROFILE_INVALID:CANONICALIZATION",
  );
  assert.throws(
    () => parseAndValidateSbrProfile(proxyBytes(profileBytes), { now: NOW }),
    (error) => error?.message === "SBR_PROFILE_INVALID:CANONICALIZATION",
  );

  const manifest = registrationManifest();
  const signature = registrationSignature(manifest);
  for (const input of [
    { publicKey: revokedBytes(Buffer.from(TEST_PUBLIC_KEY)), signature },
    { publicKey: TEST_PUBLIC_KEY, signature: revokedBytes(Buffer.from(signature)) },
  ]) {
    assert.throws(
      () => verifySbrRegistrationSignature({ manifest, ...input }),
      (error) => error?.message === "SBR_REGISTRATION_INVALID:CANONICALIZATION",
    );
  }

  const profile = profileFor({
    componentHash: HASH_A,
    endpointHash: HASH_A,
    registrationHash: HASH_A,
  });
  const signedProfile = profileSignature(profile);
  for (const input of [
    { publicKey: revokedBytes(Buffer.from(TEST_PUBLIC_KEY)), signature: signedProfile },
    { publicKey: TEST_PUBLIC_KEY, signature: revokedBytes(Buffer.from(signedProfile)) },
  ]) {
    assert.throws(
      () => verifySbrProfileSignature({ now: NOW, profile, ...input }),
      (error) => error?.message === "SBR_PROFILE_INVALID:CANONICALIZATION",
    );
  }
});

test("rejects oversized bytes and strings before any Buffer copy across schema boundaries", () => {
  const probe = String.raw`
    import { readFileSync } from "node:fs";
    import { pathToFileURL } from "node:url";

    const originalFrom = Buffer.from;
    const copiedTargets = new Set();
    Buffer.from = function trackedFrom(value, ...rest) {
      if (targets.has(value)) copiedTargets.add(value);
      return Reflect.apply(originalFrom, Buffer, [value, ...rest]);
    };
    const targets = new Set();
    const root = process.cwd();
    const registrationModule = await import(pathToFileURL(root + "/scripts/sbr-registration-schema.mjs"));
    const profileModule = await import(pathToFileURL(root + "/scripts/sbr-profile-schema.mjs"));
    const componentModule = await import(pathToFileURL(root + "/scripts/sbr-component-schema.mjs"));
    const registration = JSON.parse(readFileSync(root + "/docs/development/sbr-registration-manifest.example.json", "utf8"));
    const profile = JSON.parse(readFileSync(root + "/test/fixtures/sbr/sbr-profile-v1.example.json", "utf8"));
    const publicKey = readFileSync(root + "/config/sbr/simulator/profile-public-key.pem", "utf8");
    const validSignatureShape = "A".repeat(86) + "==\n";

    function rejectsBeforeCopy(name, value, operation, expected) {
      targets.add(value);
      let caught;
      try { operation(); } catch (error) { caught = error; }
      if (caught?.message !== expected) {
        throw new Error(name + ": unexpected error " + caught?.message);
      }
      if (copiedTargets.has(value)) throw new Error(name + ": copied before size rejection");
    }

    function shadowByteLength(value, byteLength) {
      Object.defineProperty(value, "byteLength", { configurable: true, value });
    }

    const oversizedRegistration = new Uint8Array(registrationModule.MAX_SBR_EVIDENCE_BYTES + 1);
    rejectsBeforeCopy("registration bytes", oversizedRegistration, () =>
      registrationModule.parseAndValidateSbrRegistrationManifest(oversizedRegistration),
      "SBR_REGISTRATION_INVALID:INPUT_TOO_LARGE");
    const oversizedRegistrationBuffer = Buffer.alloc(registrationModule.MAX_SBR_EVIDENCE_BYTES + 1);
    rejectsBeforeCopy("registration Buffer", oversizedRegistrationBuffer, () =>
      registrationModule.parseAndValidateSbrRegistrationManifest(oversizedRegistrationBuffer),
      "SBR_REGISTRATION_INVALID:INPUT_TOO_LARGE");
    const understatedRegistration = new Uint8Array(registrationModule.MAX_SBR_EVIDENCE_BYTES + 1);
    shadowByteLength(understatedRegistration, 1);
    rejectsBeforeCopy("understated registration bytes", understatedRegistration, () =>
      registrationModule.parseAndValidateSbrRegistrationManifest(understatedRegistration),
      "SBR_REGISTRATION_INVALID:INPUT_TOO_LARGE");
    const understatedRegistrationBuffer = Buffer.alloc(registrationModule.MAX_SBR_EVIDENCE_BYTES + 1);
    shadowByteLength(understatedRegistrationBuffer, 1);
    rejectsBeforeCopy("understated registration Buffer", understatedRegistrationBuffer, () =>
      registrationModule.parseAndValidateSbrRegistrationManifest(understatedRegistrationBuffer),
      "SBR_REGISTRATION_INVALID:INPUT_TOO_LARGE");
    const oversizedProfile = new Uint8Array(profileModule.MAX_PROFILE_BYTES + 1);
    rejectsBeforeCopy("profile bytes", oversizedProfile, () =>
      profileModule.parseAndValidateSbrProfile(oversizedProfile),
      "SBR_PROFILE_INVALID:PROFILE_TOO_LARGE");
    const oversizedProfileBuffer = Buffer.alloc(profileModule.MAX_PROFILE_BYTES + 1);
    rejectsBeforeCopy("profile Buffer", oversizedProfileBuffer, () =>
      profileModule.parseAndValidateSbrProfile(oversizedProfileBuffer),
      "SBR_PROFILE_INVALID:PROFILE_TOO_LARGE");
    const understatedProfile = new Uint8Array(profileModule.MAX_PROFILE_BYTES + 1);
    shadowByteLength(understatedProfile, 1);
    rejectsBeforeCopy("understated profile bytes", understatedProfile, () =>
      profileModule.parseAndValidateSbrProfile(understatedProfile),
      "SBR_PROFILE_INVALID:PROFILE_TOO_LARGE");
    const understatedProfileBuffer = Buffer.alloc(profileModule.MAX_PROFILE_BYTES + 1);
    shadowByteLength(understatedProfileBuffer, 1);
    rejectsBeforeCopy("understated profile Buffer", understatedProfileBuffer, () =>
      profileModule.parseAndValidateSbrProfile(understatedProfileBuffer),
      "SBR_PROFILE_INVALID:PROFILE_TOO_LARGE");
    const oversizedComponent = new Uint8Array(componentModule.MAX_COMPONENT_MANIFEST_BYTES + 1);
    rejectsBeforeCopy("component bytes", oversizedComponent, () =>
      componentModule.parseAndValidateSbrComponentManifest(oversizedComponent),
      "SBR_COMPONENT_INVALID:MANIFEST_TOO_LARGE");
    const oversizedComponentBuffer = Buffer.alloc(componentModule.MAX_COMPONENT_MANIFEST_BYTES + 1);
    rejectsBeforeCopy("component Buffer", oversizedComponentBuffer, () =>
      componentModule.parseAndValidateSbrComponentManifest(oversizedComponentBuffer),
      "SBR_COMPONENT_INVALID:MANIFEST_TOO_LARGE");
    const understatedComponent = new Uint8Array(componentModule.MAX_COMPONENT_MANIFEST_BYTES + 1);
    shadowByteLength(understatedComponent, 1);
    rejectsBeforeCopy("understated component bytes", understatedComponent, () =>
      componentModule.parseAndValidateSbrComponentManifest(understatedComponent),
      "SBR_COMPONENT_INVALID:MANIFEST_TOO_LARGE");
    const understatedComponentBuffer = Buffer.alloc(componentModule.MAX_COMPONENT_MANIFEST_BYTES + 1);
    shadowByteLength(understatedComponentBuffer, 1);
    rejectsBeforeCopy("understated component Buffer", understatedComponentBuffer, () =>
      componentModule.parseAndValidateSbrComponentManifest(understatedComponentBuffer),
      "SBR_COMPONENT_INVALID:MANIFEST_TOO_LARGE");

    const oversizedRegistrationKey = "K".repeat(4 * 1024 + 1);
    rejectsBeforeCopy("registration key", oversizedRegistrationKey, () =>
      registrationModule.verifySbrRegistrationSignature({ manifest: registration, publicKey: oversizedRegistrationKey, signature: validSignatureShape }),
      "SBR_REGISTRATION_INVALID:PUBLIC_KEY_TOO_LARGE");
    const oversizedRegistrationKeyBytes = new Uint8Array(4 * 1024 + 1);
    rejectsBeforeCopy("registration key bytes", oversizedRegistrationKeyBytes, () =>
      registrationModule.verifySbrRegistrationSignature({ manifest: registration, publicKey: oversizedRegistrationKeyBytes, signature: validSignatureShape }),
      "SBR_REGISTRATION_INVALID:PUBLIC_KEY_TOO_LARGE");
    const oversizedRegistrationSignature = "S".repeat(129);
    rejectsBeforeCopy("registration signature", oversizedRegistrationSignature, () =>
      registrationModule.verifySbrRegistrationSignature({ manifest: registration, publicKey, signature: oversizedRegistrationSignature }),
      "SBR_REGISTRATION_INVALID:SIGNATURE_TOO_LARGE");
    const oversizedRegistrationSignatureBytes = Buffer.alloc(129);
    rejectsBeforeCopy("registration signature bytes", oversizedRegistrationSignatureBytes, () =>
      registrationModule.verifySbrRegistrationSignature({ manifest: registration, publicKey, signature: oversizedRegistrationSignatureBytes }),
      "SBR_REGISTRATION_INVALID:SIGNATURE_TOO_LARGE");
    const oversizedProfileKey = "K".repeat(4 * 1024 + 1);
    rejectsBeforeCopy("profile key", oversizedProfileKey, () =>
      profileModule.verifySbrProfileSignature({ now: new Date("2026-08-21T12:00:00Z"), profile, publicKey: oversizedProfileKey, signature: validSignatureShape }),
      "SBR_PROFILE_INVALID:PUBLIC_KEY_TOO_LARGE");
    const oversizedProfileKeyBytes = Buffer.alloc(4 * 1024 + 1);
    rejectsBeforeCopy("profile key bytes", oversizedProfileKeyBytes, () =>
      profileModule.verifySbrProfileSignature({ now: new Date("2026-08-21T12:00:00Z"), profile, publicKey: oversizedProfileKeyBytes, signature: validSignatureShape }),
      "SBR_PROFILE_INVALID:PUBLIC_KEY_TOO_LARGE");
    const oversizedProfileSignature = "S".repeat(129);
    rejectsBeforeCopy("profile signature", oversizedProfileSignature, () =>
      profileModule.verifySbrProfileSignature({ now: new Date("2026-08-21T12:00:00Z"), profile, publicKey, signature: oversizedProfileSignature }),
      "SBR_PROFILE_INVALID:SIGNATURE_TOO_LARGE");
    const oversizedProfileSignatureBytes = new Uint8Array(129);
    rejectsBeforeCopy("profile signature bytes", oversizedProfileSignatureBytes, () =>
      profileModule.verifySbrProfileSignature({ now: new Date("2026-08-21T12:00:00Z"), profile, publicKey, signature: oversizedProfileSignatureBytes }),
      "SBR_PROFILE_INVALID:SIGNATURE_TOO_LARGE");
  `;
  const result = spawnSync(process.execPath, ["--input-type=module", "--eval", probe], {
    cwd: repositoryRoot,
    encoding: "utf8",
  });
  assert.equal(result.status, 0, `${result.stderr}\n${result.stdout}`);
});

test("accepts exact raw byte boundaries before structural validation", () => {
  const exactRegistration = Buffer.concat([
    encode(registrationManifest()),
    Buffer.alloc(MAX_SBR_EVIDENCE_BYTES - encode(registrationManifest()).byteLength, 0x20),
  ]);
  const exactProfileBase = encode(
    profileFor({ componentHash: HASH_A, endpointHash: HASH_A, registrationHash: HASH_A }),
  );
  const exactProfile = Buffer.concat([
    exactProfileBase,
    Buffer.alloc(MAX_PROFILE_BYTES - exactProfileBase.byteLength, 0x20),
  ]);
  const exactComponentBase = encode(componentManifest());
  const exactComponent = Buffer.concat([
    exactComponentBase,
    Buffer.alloc(MAX_COMPONENT_MANIFEST_BYTES - exactComponentBase.byteLength, 0x20),
  ]);

  for (const [value, maximumBytes, parse] of [
    [
      exactRegistration,
      MAX_SBR_EVIDENCE_BYTES,
      (input) => parseAndValidateSbrRegistrationManifest(input),
    ],
    [exactProfile, MAX_PROFILE_BYTES, (input) => parseAndValidateSbrProfile(input, { now: NOW })],
    [exactComponent, MAX_COMPONENT_MANIFEST_BYTES, parseAndValidateSbrComponentManifest],
  ]) {
    Object.defineProperty(value, "byteLength", { configurable: true, value: 1 });
    assert.doesNotThrow(() => parse(value));
    Object.defineProperty(value, "byteLength", {
      configurable: true,
      value: maximumBytes + 1,
    });
    assert.doesNotThrow(() => parse(value));
  }
});

test("rejects every registration/profile/component/endpoint cross-hash or metadata mismatch", () => {
  const evidence = makeBoundEvidence();
  const mutations = [
    {
      registrationBytes: encode({
        ...JSON.parse(evidence.registrationBytes),
        endpoint_profile: {
          ...JSON.parse(evidence.registrationBytes).endpoint_profile,
          endpoint_profile_sha256: HASH_A,
        },
      }),
    },
    {
      registrationBytes: encode({
        ...JSON.parse(evidence.registrationBytes),
        component: {
          ...JSON.parse(evidence.registrationBytes).component,
          component_manifest_sha256: HASH_A,
        },
      }),
    },
    {
      profileBytes: encode({
        ...JSON.parse(evidence.profileBytes),
        registration_manifest_sha256: HASH_A,
      }),
    },
    {
      profileBytes: encode({
        ...JSON.parse(evidence.profileBytes),
        endpoint_profile_sha256: HASH_A,
      }),
    },
    {
      profileBytes: encode({
        ...JSON.parse(evidence.profileBytes),
        component_manifest_sha256: HASH_A,
      }),
    },
    {
      registrationBytes: encode({
        ...JSON.parse(evidence.registrationBytes),
        endpoint_profile: {
          ...JSON.parse(evidence.registrationBytes).endpoint_profile,
          id: "other-profile",
        },
      }),
    },
    {
      registrationBytes: encode({
        ...JSON.parse(evidence.registrationBytes),
        component: { ...JSON.parse(evidence.registrationBytes).component, version: "0.2.0" },
      }),
    },
  ];
  for (const mutation of mutations) {
    const input = { ...evidence, ...mutation };
    if (mutation.registrationBytes)
      input.registrationSignatureBytes = registrationSignature(
        JSON.parse(mutation.registrationBytes),
      );
    if (mutation.profileBytes)
      input.profileSignatureBytes = profileSignature(JSON.parse(mutation.profileBytes));
    assert.throws(
      () => authenticateSbrEvteRegistration({ ...input, now: NOW, phase: "POST_CONFORMANCE" }),
      /SBR_REGISTRATION_INVALID/,
    );
  }
});

test("cross-binding requires exact sorted registration and endpoint service sets", () => {
  const registrationService = registrationManifest().services[0];
  const endpointService = endpointProfile().services[0];
  const secondRegistrationService = { ...registrationService, service_id: "z.service" };
  const secondEndpointService = {
    ...endpointService,
    service_id: "z.service",
    endpoint_id: "z-endpoint",
    endpoint_url: "https://z.evte.invalid/sbr/v1",
    tls_server_name: "z.evte.invalid",
  };
  const cases = [
    {
      code: "SERVICE_SET_MISMATCH",
      registrationServices: [registrationService],
      endpointServices: [endpointService, secondEndpointService],
    },
    {
      code: "SERVICE_SET_MISMATCH",
      registrationServices: [registrationService, secondRegistrationService],
      endpointServices: [endpointService],
    },
    {
      code: "PRODUCT_SERVICE_MISMATCH",
      registrationServices: [registrationService],
      endpointServices: [secondEndpointService],
    },
  ];

  for (const services of cases) {
    const evidence = makeBoundEvidenceForServices(services);
    assert.throws(
      () =>
        authenticateSbrEvteRegistration({
          ...evidence,
          now: NOW,
          phase: "POST_CONFORMANCE",
        }),
      (error) => error?.message === `SBR_REGISTRATION_INVALID:${services.code}`,
    );
  }
});

test("keeps checked-in examples unsigned, placeholder-only, schema-valid, and not ready", async () => {
  const registrationBytes = await readFile(
    path.join(repositoryRoot, "docs", "development", "sbr-registration-manifest.example.json"),
  );
  const endpointBytes = await readFile(
    path.join(repositoryRoot, "docs", "development", "sbr-endpoint-profile.example.json"),
  );
  const registration = parseAndValidateSbrRegistrationManifest(registrationBytes, { now: NOW });
  const endpoint = parseAndValidateSbrEndpointProfile(endpointBytes, { now: NOW });

  assert.deepEqual(
    evaluateSbrRegistrationReadiness({
      manifest: registration.manifest,
      now: NOW,
      phase: "PRE_CONFORMANCE",
    }),
    {
      ready: false,
      code: "COMPONENT_LICENCE_NOT_APPROVED",
    },
  );
  assert.equal(
    endpoint.profile.services.every((service) =>
      new URL(service.endpoint_url).hostname.endsWith(".invalid"),
    ),
    true,
  );
  assert.equal(registrationBytes.toString().includes("signature"), false);
  assert.equal(endpointBytes.toString().includes("signature"), false);
});
