import { createHash, createPublicKey, verify } from "node:crypto";
import { runInNewContext } from "node:vm";

import { parseAndValidateSbrComponentManifest } from "./sbr-component-schema.mjs";
import { parseAndValidateSbrProfile, verifySbrProfileSignature } from "./sbr-profile-schema.mjs";

export const MAX_SBR_EVIDENCE_BYTES = 256 * 1024;
export const MAX_SBR_SERVICES = 128;
export const MAX_SBR_ARTEFACT_HASHES = 128;

const MAX_JSON_NESTING_DEPTH = 32;
const MAX_IDENTIFIER_BYTES = 128;
const MAX_COMPONENT_IDENTIFIER_BYTES = 64;
const MAX_ENDPOINT_URL_BYTES = 2_048;
const MAX_SIGNATURE_BYTES = 128;
const MAX_PUBLIC_KEY_BYTES = 4 * 1024;
const LOWERCASE_SHA256 = /^[0-9a-f]{64}$/;
const COMPONENT_IDENTIFIER = /^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$/;
const UTC_TIMESTAMP =
  /^(\d{4})-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])T([01]\d|2[0-3]):([0-5]\d):([0-5]\d)Z$/;
const ISO_DATE = /^(\d{4})-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])$/;
const STRICT_BASE64_SIGNATURE = /^[A-Za-z0-9+/]{86}==\n$/;
const CANONICAL_PUBLIC_KEY_PEM =
  /^-----BEGIN PUBLIC KEY-----\n(?:[A-Za-z0-9+/]{64}\n)*[A-Za-z0-9+/]{4,64}={0,2}\n-----END PUBLIC KEY-----\n$/;
const HOSTNAME_LABEL = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;

// UNREGISTERED placeholder trust root. Its randomly generated Ed25519 private half was discarded
// immediately and was never logged, written, or committed. This public key cannot enable EVTE;
// registration must replace it with the externally issued release trust root and flip the state.
const UNREGISTERED_EVTE_PUBLIC_KEY = `-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEA3YWWiH31fK93Oeb8+iuUjcrsh7+IFPz7NJsY3j2Z6og=
-----END PUBLIC KEY-----
`;
const EVTE_TRUST_ROOT_REGISTERED = false;

const REGISTRATION_KEYS = [
  "schema_version",
  "environment",
  "target",
  "product_id_scope",
  "dsp_registration",
  "product_registration",
  "osf_assessment",
  "component",
  "services",
  "evte_access",
  "endpoint_profile",
  "review",
];
const PRODUCT_ID_SCOPE_KEYS = ["product_identifier", "service_id"];
const REGISTRATION_STATE_KEYS = ["state", "external_reference", "decision_date", "expires_at"];
const OSF_KEYS = ["category", "state", "external_reference", "decision_date", "revalidation_date"];
const COMPONENT_KEYS = ["name", "version", "component_manifest_sha256", "licence_state", "target"];
const REGISTRATION_SERVICE_KEYS = [
  "service_id",
  "taxonomy_version",
  "release_version",
  "artefact_sha256s",
  "enrolment_state",
  "conformance_state",
];
const EVTE_ACCESS_KEYS = ["state", "external_reference", "issued_at", "expires_at"];
const ENDPOINT_REFERENCE_KEYS = [
  "id",
  "revision",
  "endpoint_profile_sha256",
  "issued_at",
  "expires_at",
];
const REVIEW_KEYS = ["reviewer_identity", "approved_at", "revalidation_date"];
const ENDPOINT_PROFILE_KEYS = [
  "schema_version",
  "environment",
  "profile_id",
  "revision",
  "issued_at",
  "expires_at",
  "services",
];
const ENDPOINT_SERVICE_KEYS = [
  "service_id",
  "endpoint_id",
  "endpoint_url",
  "tls_server_name",
  "certificate_sha256",
];

const isolatedJsonStringify = runInNewContext("(value) => JSON.stringify(value)");
const TrustedDate = Date;
const trustedDateGetTime = Date.prototype.getTime;
const trustedDateToISOString = Date.prototype.toISOString;
const trustedDateParse = Date.parse;
const trustedReflectApply = Reflect.apply;
const TrustedBuffer = Buffer;
const trustedBufferFrom = Buffer.from;
const trustedBufferByteLength = Buffer.byteLength;
const TypedArrayPrototype = Object.getPrototypeOf(Uint8Array.prototype);
const trustedTypedArrayByteLength = Object.getOwnPropertyDescriptor(
  TypedArrayPrototype,
  "byteLength",
).get;

function invalid(reason) {
  return new Error(`SBR_REGISTRATION_INVALID:${reason}`);
}

function copyByteInput(value, inputError, maximumBytes, tooLargeError) {
  let byteLength;
  try {
    if (!(value instanceof Uint8Array)) {
      throw invalid(inputError);
    }
    if (!ArrayBuffer.isView(value)) throw invalid("CANONICALIZATION");
    byteLength = trustedReflectApply(trustedTypedArrayByteLength, value, []);
  } catch (error) {
    if (error instanceof Error && error.message.startsWith("SBR_REGISTRATION_INVALID:")) {
      throw error;
    }
    throw invalid("CANONICALIZATION");
  }
  if (byteLength > maximumBytes) throw invalid(tooLargeError);
  try {
    const bytes = trustedReflectApply(trustedBufferFrom, TrustedBuffer, [value]);
    if (trustedReflectApply(trustedTypedArrayByteLength, bytes, []) !== byteLength) {
      throw invalid("CANONICALIZATION");
    }
    return bytes;
  } catch (error) {
    if (error instanceof Error && error.message.startsWith("SBR_REGISTRATION_INVALID:")) {
      throw error;
    }
    throw invalid("CANONICALIZATION");
  }
}

function copyBoundedString(value, maximumBytes, tooLargeError) {
  let byteLength;
  try {
    byteLength = trustedReflectApply(trustedBufferByteLength, TrustedBuffer, [value, "utf8"]);
  } catch {
    throw invalid("CANONICALIZATION");
  }
  if (byteLength > maximumBytes) throw invalid(tooLargeError);
  try {
    const bytes = trustedReflectApply(trustedBufferFrom, TrustedBuffer, [value]);
    if (bytes.byteLength !== byteLength) throw invalid("CANONICALIZATION");
    return bytes;
  } catch {
    throw invalid("CANONICALIZATION");
  }
}

function assertClock(now) {
  let milliseconds;
  try {
    milliseconds = trustedReflectApply(trustedDateGetTime, now, []);
  } catch {
    throw invalid("CLOCK");
  }
  if (!Number.isFinite(milliseconds)) {
    throw invalid("CLOCK");
  }
  return milliseconds;
}

function parseTrustedDate(value, errorCode) {
  let milliseconds;
  try {
    milliseconds = trustedReflectApply(trustedDateParse, TrustedDate, [value]);
  } catch {
    throw invalid(errorCode);
  }
  if (!Number.isFinite(milliseconds)) throw invalid(errorCode);
  return milliseconds;
}

function trustedIsoString(milliseconds, errorCode) {
  try {
    const date = new TrustedDate(milliseconds);
    return trustedReflectApply(trustedDateToISOString, date, []);
  } catch {
    throw invalid(errorCode);
  }
}

function assertUnicodeScalarString(value) {
  for (let index = 0; index < value.length; index += 1) {
    const codeUnit = value.charCodeAt(index);
    if (codeUnit >= 0xd800 && codeUnit <= 0xdbff) {
      const next = value.charCodeAt(index + 1);
      if (!Number.isInteger(next) || next < 0xdc00 || next > 0xdfff) {
        throw invalid("UNICODE");
      }
      index += 1;
    } else if (codeUnit >= 0xdc00 && codeUnit <= 0xdfff) {
      throw invalid("UNICODE");
    }
  }
}

// Validate the JSON token stream before JSON.parse can erase duplicate escaped keys.
function assertJsonStructure(raw) {
  let cursor = 0;

  function skipWhitespace() {
    while (cursor < raw.length && /[\t\n\r ]/.test(raw[cursor])) cursor += 1;
  }

  function parseString() {
    if (raw[cursor] !== '"') throw invalid("JSON");
    const start = cursor;
    cursor += 1;
    while (cursor < raw.length) {
      const character = raw[cursor];
      if (character === '"') {
        cursor += 1;
        let decoded;
        try {
          decoded = JSON.parse(raw.slice(start, cursor));
        } catch {
          throw invalid("JSON");
        }
        assertUnicodeScalarString(decoded);
        return decoded;
      }
      if (character === "\\") {
        cursor += 1;
        const escapeCode = raw[cursor];
        if (escapeCode === "u") {
          if (!/^[0-9a-fA-F]{4}$/.test(raw.slice(cursor + 1, cursor + 5))) {
            throw invalid("JSON");
          }
          cursor += 5;
          continue;
        }
        if (!['"', "\\", "/", "b", "f", "n", "r", "t"].includes(escapeCode)) {
          throw invalid("JSON");
        }
        cursor += 1;
        continue;
      }
      if (character.charCodeAt(0) < 0x20) throw invalid("JSON");
      cursor += 1;
    }
    throw invalid("JSON");
  }

  function parsePrimitive() {
    const start = cursor;
    while (cursor < raw.length && !/[\t\n\r ,\]}]/.test(raw[cursor])) cursor += 1;
    try {
      const value = JSON.parse(raw.slice(start, cursor));
      if (value !== null && typeof value === "object") throw invalid("JSON");
    } catch (error) {
      if (error instanceof Error && error.message.startsWith("SBR_REGISTRATION_INVALID:")) {
        throw error;
      }
      throw invalid("JSON");
    }
  }

  function parseArray(depth) {
    cursor += 1;
    skipWhitespace();
    if (raw[cursor] === "]") {
      cursor += 1;
      return;
    }
    while (cursor < raw.length) {
      parseValue(depth);
      skipWhitespace();
      if (raw[cursor] === "]") {
        cursor += 1;
        return;
      }
      if (raw[cursor] !== ",") throw invalid("JSON");
      cursor += 1;
      skipWhitespace();
    }
    throw invalid("JSON");
  }

  function parseObject(depth) {
    cursor += 1;
    const keys = new Set();
    skipWhitespace();
    if (raw[cursor] === "}") {
      cursor += 1;
      return;
    }
    while (cursor < raw.length) {
      const key = parseString();
      if (keys.has(key)) throw invalid("DUPLICATE_KEY");
      keys.add(key);
      skipWhitespace();
      if (raw[cursor] !== ":") throw invalid("JSON");
      cursor += 1;
      parseValue(depth);
      skipWhitespace();
      if (raw[cursor] === "}") {
        cursor += 1;
        return;
      }
      if (raw[cursor] !== ",") throw invalid("JSON");
      cursor += 1;
      skipWhitespace();
    }
    throw invalid("JSON");
  }

  function parseValue(depth = 0) {
    skipWhitespace();
    if (raw[cursor] === "{") {
      if (depth >= MAX_JSON_NESTING_DEPTH) throw invalid("JSON_DEPTH");
      parseObject(depth + 1);
    } else if (raw[cursor] === "[") {
      if (depth >= MAX_JSON_NESTING_DEPTH) throw invalid("JSON_DEPTH");
      parseArray(depth + 1);
    } else if (raw[cursor] === '"') {
      parseString();
    } else {
      parsePrimitive();
    }
  }

  parseValue();
  skipWhitespace();
  if (cursor !== raw.length) throw invalid("TRAILING_DATA");
}

function decodeRawJson(input) {
  if (typeof input === "string") {
    if (
      trustedReflectApply(trustedBufferByteLength, TrustedBuffer, [input, "utf8"]) >
      MAX_SBR_EVIDENCE_BYTES
    ) {
      throw invalid("INPUT_TOO_LARGE");
    }
    return input;
  }
  const bytes = copyByteInput(input, "INPUT", MAX_SBR_EVIDENCE_BYTES, "INPUT_TOO_LARGE");
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    throw invalid("UTF8");
  }
}

function exactObject(value, expectedKeys, errorCode) {
  let isArray;
  try {
    isArray = Array.isArray(value);
  } catch {
    throw invalid("CANONICALIZATION");
  }
  if (value === null || typeof value !== "object" || isArray) {
    throw invalid(errorCode);
  }
  let ownKeys;
  try {
    ownKeys = Reflect.ownKeys(value);
  } catch {
    throw invalid("CANONICALIZATION");
  }
  if (ownKeys.length !== expectedKeys.length || ownKeys.some((key) => typeof key !== "string")) {
    throw invalid(errorCode);
  }
  const snapshot = {};
  for (const key of expectedKeys) {
    let descriptor;
    try {
      descriptor = Object.getOwnPropertyDescriptor(value, key);
    } catch {
      throw invalid("CANONICALIZATION");
    }
    if (!descriptor?.enumerable) throw invalid(errorCode);
    if (!("value" in descriptor)) throw invalid("CANONICALIZATION");
    Object.defineProperty(snapshot, key, { enumerable: true, value: descriptor.value });
  }
  return snapshot;
}

function exactArray(value, minimum, maximum, errorCode) {
  let isArray;
  let lengthDescriptor;
  try {
    isArray = Array.isArray(value);
    if (isArray) lengthDescriptor = Object.getOwnPropertyDescriptor(value, "length");
  } catch {
    throw invalid("CANONICALIZATION");
  }
  if (!isArray) throw invalid(errorCode);
  if (!lengthDescriptor || !("value" in lengthDescriptor)) throw invalid("CANONICALIZATION");
  const length = lengthDescriptor.value;
  if (!Number.isSafeInteger(length) || length < minimum || length > maximum) {
    throw invalid(errorCode);
  }
  let keys;
  try {
    keys = Reflect.ownKeys(value);
  } catch {
    throw invalid("CANONICALIZATION");
  }
  if (keys.length !== length + 1) throw invalid("CANONICALIZATION");
  const values = new Array(length);
  for (let index = 0; index < length; index += 1) {
    let descriptor;
    try {
      descriptor = Object.getOwnPropertyDescriptor(value, String(index));
    } catch {
      throw invalid("CANONICALIZATION");
    }
    if (!descriptor?.enumerable || !("value" in descriptor)) throw invalid("CANONICALIZATION");
    Object.defineProperty(values, String(index), {
      configurable: true,
      enumerable: true,
      value: descriptor.value,
      writable: true,
    });
  }
  return values;
}

function validateOpaque(value, errorCode) {
  if (typeof value !== "string") throw invalid(errorCode);
  assertUnicodeScalarString(value);
  if (
    value.length === 0 ||
    trustedReflectApply(trustedBufferByteLength, TrustedBuffer, [value, "utf8"]) >
      MAX_IDENTIFIER_BYTES ||
    value.charCodeAt(0) <= 0x20 ||
    value.charCodeAt(value.length - 1) <= 0x20
  ) {
    throw invalid(errorCode);
  }
  for (const character of value) {
    const codePoint = character.codePointAt(0);
    if (codePoint < 0x20 || codePoint > 0x7e) throw invalid(errorCode);
  }
  return value;
}

function validateNullableOpaque(value, errorCode) {
  if (value === null) return null;
  return validateOpaque(value, errorCode);
}

function validateComponentIdentifier(value, errorCode) {
  if (typeof value !== "string" || !COMPONENT_IDENTIFIER.test(value)) {
    throw invalid(errorCode);
  }
  if (
    trustedReflectApply(trustedBufferByteLength, TrustedBuffer, [value, "utf8"]) >
    MAX_COMPONENT_IDENTIFIER_BYTES
  ) {
    throw invalid(errorCode);
  }
  return value;
}

function validateHash(value, errorCode) {
  if (typeof value !== "string" || !LOWERCASE_SHA256.test(value)) throw invalid(errorCode);
  return value;
}

function validateTimestamp(value, errorCode) {
  if (typeof value !== "string" || !UTC_TIMESTAMP.test(value)) throw invalid(errorCode);
  const milliseconds = parseTrustedDate(value, errorCode);
  if (trustedIsoString(milliseconds, errorCode).replace(".000Z", "Z") !== value) {
    throw invalid(errorCode);
  }
  return value;
}

function validateNullableTimestamp(value, errorCode) {
  if (value === null) return null;
  return validateTimestamp(value, errorCode);
}

function validateDate(value, errorCode) {
  if (typeof value !== "string" || !ISO_DATE.test(value)) throw invalid(errorCode);
  const milliseconds = parseTrustedDate(`${value}T00:00:00Z`, errorCode);
  if (trustedIsoString(milliseconds, errorCode).slice(0, 10) !== value) {
    throw invalid(errorCode);
  }
  return value;
}

function validateNullableDate(value, errorCode) {
  if (value === null) return null;
  return validateDate(value, errorCode);
}

function compareUtf8(left, right) {
  return Buffer.compare(
    trustedReflectApply(trustedBufferFrom, TrustedBuffer, [left, "utf8"]),
    trustedReflectApply(trustedBufferFrom, TrustedBuffer, [right, "utf8"]),
  );
}

function validateApproval(value, field) {
  const source = exactObject(value, REGISTRATION_STATE_KEYS, `${field}_FIELDS`);
  if (!["NOT_STARTED", "SUBMITTED", "APPROVED"].includes(source.state)) {
    throw invalid(`${field}_STATE`);
  }
  const externalReference = validateNullableOpaque(source.external_reference, `${field}_REFERENCE`);
  const decisionDate = validateNullableDate(source.decision_date, `${field}_DECISION_DATE`);
  const expiresAt = validateNullableTimestamp(source.expires_at, `${field}_EXPIRES_AT`);
  if (
    (source.state === "NOT_STARTED" &&
      (externalReference !== null || decisionDate !== null || expiresAt !== null)) ||
    (source.state === "SUBMITTED" &&
      (externalReference === null || decisionDate !== null || expiresAt !== null)) ||
    (source.state === "APPROVED" && (externalReference === null || decisionDate === null))
  ) {
    throw invalid(`${field}_TRANSITION`);
  }
  return Object.freeze({
    state: source.state,
    external_reference: externalReference,
    decision_date: decisionDate,
    expires_at: expiresAt,
  });
}

function validateOsfAssessment(value) {
  const source = exactObject(value, OSF_KEYS, "OSF_FIELDS");
  validateOpaque(source.category, "OSF_CATEGORY");
  if (!["NOT_STARTED", "IN_REVIEW", "APPROVED"].includes(source.state)) {
    throw invalid("OSF_STATE");
  }
  const reference = validateNullableOpaque(source.external_reference, "OSF_REFERENCE");
  const decisionDate = validateNullableDate(source.decision_date, "OSF_DECISION_DATE");
  const revalidationDate = validateNullableDate(source.revalidation_date, "OSF_REVALIDATION_DATE");
  if (
    (source.state === "NOT_STARTED" &&
      (reference !== null || decisionDate !== null || revalidationDate !== null)) ||
    (source.state === "IN_REVIEW" &&
      (reference === null || decisionDate !== null || revalidationDate !== null)) ||
    (source.state === "APPROVED" &&
      (reference === null || decisionDate === null || revalidationDate === null))
  ) {
    throw invalid("OSF_TRANSITION");
  }
  return Object.freeze({
    category: source.category,
    state: source.state,
    external_reference: reference,
    decision_date: decisionDate,
    revalidation_date: revalidationDate,
  });
}

function validateComponent(value) {
  const source = exactObject(value, COMPONENT_KEYS, "COMPONENT_FIELDS");
  validateComponentIdentifier(source.name, "COMPONENT_NAME");
  validateComponentIdentifier(source.version, "COMPONENT_VERSION");
  validateHash(source.component_manifest_sha256, "COMPONENT_HASH");
  if (!["NOT_OBTAINED", "REVIEW_REQUIRED", "APPROVED"].includes(source.licence_state)) {
    throw invalid("COMPONENT_LICENCE_STATE");
  }
  if (source.target !== "darwin/arm64") throw invalid("COMPONENT_TARGET");
  return Object.freeze({ ...source });
}

function validateHashArray(value) {
  const source = exactArray(value, 1, MAX_SBR_ARTEFACT_HASHES, "ARTEFACT_HASHES");
  const result = new Array(source.length);
  let previous;
  for (let index = 0; index < source.length; index += 1) {
    const hash = validateHash(source[index], "ARTEFACT_HASH");
    if (previous !== undefined && compareUtf8(previous, hash) >= 0) {
      throw invalid("ARTEFACT_HASH_ORDER");
    }
    previous = hash;
    result[index] = hash;
  }
  return Object.freeze(result);
}

function validateRegistrationService(value) {
  const source = exactObject(value, REGISTRATION_SERVICE_KEYS, "SERVICE_FIELDS");
  validateOpaque(source.service_id, "SERVICE_ID");
  validateOpaque(source.taxonomy_version, "TAXONOMY_VERSION");
  validateOpaque(source.release_version, "RELEASE_VERSION");
  const hashes = validateHashArray(source.artefact_sha256s);
  if (!["NOT_STARTED", "SUBMITTED", "APPROVED"].includes(source.enrolment_state)) {
    throw invalid("SERVICE_ENROLMENT_STATE");
  }
  if (!["NOT_STARTED", "RUNNING", "PASSED"].includes(source.conformance_state)) {
    throw invalid("SERVICE_CONFORMANCE_STATE");
  }
  if (source.enrolment_state !== "APPROVED" && source.conformance_state !== "NOT_STARTED") {
    throw invalid("SERVICE_TRANSITION");
  }
  return Object.freeze({ ...source, artefact_sha256s: hashes });
}

function validateRegistrationServices(value) {
  const source = exactArray(value, 1, MAX_SBR_SERVICES, "SERVICES");
  const result = new Array(source.length);
  let previous;
  for (let index = 0; index < source.length; index += 1) {
    const service = validateRegistrationService(source[index]);
    if (previous !== undefined && compareUtf8(previous, service.service_id) >= 0) {
      throw invalid("SERVICE_ORDER");
    }
    previous = service.service_id;
    result[index] = service;
  }
  return Object.freeze(result);
}

function validateProductIdScope(value) {
  const source = exactObject(value, PRODUCT_ID_SCOPE_KEYS, "PRODUCT_ID_SCOPE_FIELDS");
  return Object.freeze({
    product_identifier: validateOpaque(source.product_identifier, "PRODUCT_IDENTIFIER"),
    service_id: validateOpaque(source.service_id, "PRODUCT_SERVICE_ID"),
  });
}

function validateEvteAccess(value) {
  const source = exactObject(value, EVTE_ACCESS_KEYS, "EVTE_ACCESS_FIELDS");
  if (!["NOT_REQUESTED", "REQUESTED", "APPROVED"].includes(source.state)) {
    throw invalid("EVTE_ACCESS_STATE");
  }
  const reference = validateNullableOpaque(source.external_reference, "EVTE_ACCESS_REFERENCE");
  const issuedAt = validateNullableTimestamp(source.issued_at, "EVTE_ACCESS_ISSUED_AT");
  const expiresAt = validateNullableTimestamp(source.expires_at, "EVTE_ACCESS_EXPIRES_AT");
  if (
    (source.state === "NOT_REQUESTED" &&
      (reference !== null || issuedAt !== null || expiresAt !== null)) ||
    (source.state === "REQUESTED" &&
      (reference === null || issuedAt !== null || expiresAt !== null)) ||
    (source.state === "APPROVED" && (reference === null || issuedAt === null || expiresAt === null))
  ) {
    throw invalid("EVTE_ACCESS_TRANSITION");
  }
  if (
    issuedAt !== null &&
    expiresAt !== null &&
    parseTrustedDate(expiresAt, "EVTE_ACCESS_EXPIRES_AT") <=
      parseTrustedDate(issuedAt, "EVTE_ACCESS_ISSUED_AT")
  ) {
    throw invalid("EVTE_ACCESS_WINDOW");
  }
  return Object.freeze({
    state: source.state,
    external_reference: reference,
    issued_at: issuedAt,
    expires_at: expiresAt,
  });
}

function validateEndpointReference(value) {
  const source = exactObject(value, ENDPOINT_REFERENCE_KEYS, "ENDPOINT_REFERENCE_FIELDS");
  validateOpaque(source.id, "ENDPOINT_PROFILE_ID");
  if (!Number.isSafeInteger(source.revision) || source.revision < 1) {
    throw invalid("ENDPOINT_PROFILE_REVISION");
  }
  validateHash(source.endpoint_profile_sha256, "ENDPOINT_PROFILE_HASH");
  validateTimestamp(source.issued_at, "ENDPOINT_PROFILE_ISSUED_AT");
  validateTimestamp(source.expires_at, "ENDPOINT_PROFILE_EXPIRES_AT");
  if (
    parseTrustedDate(source.expires_at, "ENDPOINT_PROFILE_EXPIRES_AT") <=
    parseTrustedDate(source.issued_at, "ENDPOINT_PROFILE_ISSUED_AT")
  ) {
    throw invalid("ENDPOINT_PROFILE_WINDOW");
  }
  return Object.freeze({ ...source });
}

function validateReview(value) {
  const source = exactObject(value, REVIEW_KEYS, "REVIEW_FIELDS");
  validateOpaque(source.reviewer_identity, "REVIEWER_IDENTITY");
  validateTimestamp(source.approved_at, "REVIEW_APPROVED_AT");
  validateDate(source.revalidation_date, "REVIEW_REVALIDATION_DATE");
  return Object.freeze({ ...source });
}

function validateRegistrationData(value) {
  const source = exactObject(value, REGISTRATION_KEYS, "FIELDS");
  if (source.schema_version !== 1) throw invalid("SCHEMA_VERSION");
  if (source.environment !== "EVTE") throw invalid("ENVIRONMENT");
  if (source.target !== "darwin/arm64") throw invalid("TARGET");
  const productIdScope = validateProductIdScope(source.product_id_scope);
  const services = validateRegistrationServices(source.services);
  if (!services.some((service) => service.service_id === productIdScope.service_id)) {
    throw invalid("PRODUCT_SERVICE_MISMATCH");
  }
  return Object.freeze({
    schema_version: 1,
    environment: "EVTE",
    target: "darwin/arm64",
    product_id_scope: productIdScope,
    dsp_registration: validateApproval(source.dsp_registration, "DSP_REGISTRATION"),
    product_registration: validateApproval(source.product_registration, "PRODUCT_REGISTRATION"),
    osf_assessment: validateOsfAssessment(source.osf_assessment),
    component: validateComponent(source.component),
    services,
    evte_access: validateEvteAccess(source.evte_access),
    endpoint_profile: validateEndpointReference(source.endpoint_profile),
    review: validateReview(source.review),
  });
}

function validateHostname(value, errorCode) {
  validateOpaque(value, errorCode);
  if (value.length > 253 || value !== value.toLowerCase() || !value.includes(".")) {
    throw invalid(errorCode);
  }
  const labels = value.split(".");
  if (labels.some((label) => !HOSTNAME_LABEL.test(label)) || /^\d+(?:\.\d+)+$/.test(value)) {
    throw invalid(errorCode);
  }
  return value;
}

function validateEndpointUrl(value) {
  if (
    typeof value !== "string" ||
    trustedReflectApply(trustedBufferByteLength, TrustedBuffer, [value, "utf8"]) >
      MAX_ENDPOINT_URL_BYTES ||
    value.includes("\\") ||
    value.includes("%")
  ) {
    throw invalid("ENDPOINT_URL");
  }
  for (const character of value) {
    const codePoint = character.codePointAt(0);
    if (codePoint < 0x21 || codePoint > 0x7e) throw invalid("ENDPOINT_URL");
  }
  let parsed;
  try {
    parsed = new URL(value);
  } catch {
    throw invalid("ENDPOINT_URL");
  }
  const authorityEnd = value.indexOf("/", "https://".length);
  const rawAuthority = value.slice(
    "https://".length,
    authorityEnd === -1 ? value.length : authorityEnd,
  );
  if (
    parsed.protocol !== "https:" ||
    parsed.username !== "" ||
    parsed.password !== "" ||
    rawAuthority.includes(":") ||
    parsed.port !== "" ||
    parsed.search !== "" ||
    parsed.hash !== "" ||
    parsed.href !== value
  ) {
    throw invalid("ENDPOINT_URL");
  }
  validateHostname(parsed.hostname, "ENDPOINT_HOSTNAME");
  return value;
}

function validateEndpointService(value) {
  const source = exactObject(value, ENDPOINT_SERVICE_KEYS, "ENDPOINT_SERVICE_FIELDS");
  validateOpaque(source.service_id, "ENDPOINT_SERVICE_ID");
  validateOpaque(source.endpoint_id, "ENDPOINT_ID");
  validateEndpointUrl(source.endpoint_url);
  validateHostname(source.tls_server_name, "TLS_SERVER_NAME");
  validateHash(source.certificate_sha256, "CERTIFICATE_HASH");
  return Object.freeze({ ...source });
}

function validateEndpointServices(value) {
  const source = exactArray(value, 1, MAX_SBR_SERVICES, "ENDPOINT_SERVICES");
  const result = new Array(source.length);
  let previous;
  for (let index = 0; index < source.length; index += 1) {
    const service = validateEndpointService(source[index]);
    if (previous !== undefined && compareUtf8(previous, service.service_id) >= 0) {
      throw invalid("ENDPOINT_SERVICE_ORDER");
    }
    previous = service.service_id;
    result[index] = service;
  }
  return Object.freeze(result);
}

function validateEndpointProfileData(value) {
  const source = exactObject(value, ENDPOINT_PROFILE_KEYS, "ENDPOINT_FIELDS");
  if (source.schema_version !== 1) throw invalid("ENDPOINT_SCHEMA_VERSION");
  if (source.environment !== "EVTE") throw invalid("ENDPOINT_ENVIRONMENT");
  validateOpaque(source.profile_id, "ENDPOINT_PROFILE_ID");
  if (!Number.isSafeInteger(source.revision) || source.revision < 1) {
    throw invalid("ENDPOINT_PROFILE_REVISION");
  }
  validateTimestamp(source.issued_at, "ENDPOINT_PROFILE_ISSUED_AT");
  validateTimestamp(source.expires_at, "ENDPOINT_PROFILE_EXPIRES_AT");
  if (
    parseTrustedDate(source.expires_at, "ENDPOINT_PROFILE_EXPIRES_AT") <=
    parseTrustedDate(source.issued_at, "ENDPOINT_PROFILE_ISSUED_AT")
  ) {
    throw invalid("ENDPOINT_PROFILE_WINDOW");
  }
  return Object.freeze({
    schema_version: 1,
    environment: "EVTE",
    profile_id: source.profile_id,
    revision: source.revision,
    issued_at: source.issued_at,
    expires_at: source.expires_at,
    services: validateEndpointServices(source.services),
  });
}

function sortedOwnStringKeys(value) {
  let keys;
  try {
    keys = Reflect.ownKeys(value);
  } catch {
    throw invalid("CANONICALIZATION");
  }
  if (keys.some((key) => typeof key !== "string")) throw invalid("CANONICALIZATION");
  for (let index = 1; index < keys.length; index += 1) {
    const key = keys[index];
    let cursor = index - 1;
    while (cursor >= 0 && keys[cursor] > key) {
      keys[cursor + 1] = keys[cursor];
      cursor -= 1;
    }
    keys[cursor + 1] = key;
  }
  return keys;
}

function canonicalJson(value) {
  if (value === null) return "null";
  if (typeof value === "string") return isolatedJsonStringify(value);
  if (typeof value === "number" && Number.isSafeInteger(value)) return String(value);
  let isArray;
  try {
    isArray = Array.isArray(value);
  } catch {
    throw invalid("CANONICALIZATION");
  }
  if (isArray) {
    let result = "[";
    for (let index = 0; index < value.length; index += 1) {
      if (index > 0) result += ",";
      result += canonicalJson(value[index]);
    }
    return `${result}]`;
  }
  if (value !== null && typeof value === "object") {
    const keys = sortedOwnStringKeys(value);
    let result = "{";
    for (let index = 0; index < keys.length; index += 1) {
      if (index > 0) result += ",";
      const key = keys[index];
      result += `${isolatedJsonStringify(key)}:${canonicalJson(value[key])}`;
    }
    return `${result}}`;
  }
  throw invalid("CANONICALIZATION");
}

function canonicalBytes(value) {
  try {
    return trustedReflectApply(trustedBufferFrom, TrustedBuffer, [canonicalJson(value), "utf8"]);
  } catch (error) {
    if (error instanceof Error && error.message.startsWith("SBR_REGISTRATION_INVALID:")) {
      throw error;
    }
    throw invalid("CANONICALIZATION");
  }
}

function unwrapEvidence(value, field) {
  if (value === null || typeof value !== "object") return value;
  let descriptor;
  try {
    descriptor = Object.getOwnPropertyDescriptor(value, field);
  } catch {
    throw invalid("CANONICALIZATION");
  }
  if (!descriptor) return value;
  if (!descriptor.enumerable || !("value" in descriptor)) {
    throw invalid("CANONICALIZATION");
  }
  return descriptor.value;
}

function validateAndBoundCanonical(value, validator, wrapperField) {
  const validated = validator(unwrapEvidence(value, wrapperField));
  const bytes = canonicalBytes(validated);
  if (bytes.byteLength > MAX_SBR_EVIDENCE_BYTES) {
    throw invalid("EVIDENCE_TOO_LARGE");
  }
  return { bytes, validated };
}

function parseRaw(input, validator, now) {
  assertClock(now);
  const raw = decodeRawJson(input);
  assertJsonStructure(raw);
  let value;
  try {
    value = JSON.parse(raw);
  } catch {
    throw invalid("JSON");
  }
  const validated = validator(value);
  const bytes = canonicalBytes(validated);
  if (bytes.byteLength > MAX_SBR_EVIDENCE_BYTES) throw invalid("EVIDENCE_TOO_LARGE");
  return { bytes, validated };
}

export function parseAndValidateSbrRegistrationManifest(input, { now = new TrustedDate() } = {}) {
  const { bytes, validated } = parseRaw(input, validateRegistrationData, now);
  return Object.freeze({
    manifest: validated,
    canonicalBytes: bytes,
    sha256: createHash("sha256").update(bytes).digest("hex"),
  });
}

export function canonicalizeSbrRegistrationManifest(manifest) {
  try {
    return validateAndBoundCanonical(manifest, validateRegistrationData, "manifest").bytes;
  } catch (error) {
    if (error instanceof Error && error.message.startsWith("SBR_REGISTRATION_INVALID:")) {
      if (
        error.message.endsWith(":CANONICALIZATION") ||
        error.message.endsWith(":EVIDENCE_TOO_LARGE")
      ) {
        throw error;
      }
      throw invalid("CANONICALIZATION");
    }
    throw invalid("CANONICALIZATION");
  }
}

export function parseAndValidateSbrEndpointProfile(input, { now = new TrustedDate() } = {}) {
  const { bytes, validated } = parseRaw(input, validateEndpointProfileData, now);
  return Object.freeze({
    profile: validated,
    canonicalBytes: bytes,
    sha256: createHash("sha256").update(bytes).digest("hex"),
  });
}

export function canonicalizeSbrEndpointProfile(profile) {
  try {
    return validateAndBoundCanonical(profile, validateEndpointProfileData, "profile").bytes;
  } catch (error) {
    if (error instanceof Error && error.message.startsWith("SBR_REGISTRATION_INVALID:")) {
      if (
        error.message.endsWith(":CANONICALIZATION") ||
        error.message.endsWith(":EVIDENCE_TOO_LARGE")
      ) {
        throw error;
      }
      throw invalid("CANONICALIZATION");
    }
    throw invalid("CANONICALIZATION");
  }
}

function readiness(ready, code) {
  return Object.freeze({ ready, code });
}

function approvalReadiness(approval, prefix, nowMilliseconds, today) {
  if (approval.state !== "APPROVED") return readiness(false, `${prefix}_NOT_APPROVED`);
  if (approval.decision_date > today) return readiness(false, `${prefix}_DECISION_IN_FUTURE`);
  if (
    approval.expires_at !== null &&
    parseTrustedDate(approval.expires_at, `${prefix}_EXPIRES_AT`) <= nowMilliseconds
  ) {
    return readiness(false, `${prefix}_EXPIRED`);
  }
  return undefined;
}

export function evaluateSbrRegistrationReadiness({ manifest, now = new TrustedDate(), phase }) {
  const value = validateAndBoundCanonical(manifest, validateRegistrationData, "manifest").validated;
  const nowMilliseconds = assertClock(now);
  if (phase !== "PRE_CONFORMANCE" && phase !== "POST_CONFORMANCE") {
    throw invalid("READINESS_PHASE");
  }
  const today = trustedIsoString(nowMilliseconds, "CLOCK").slice(0, 10);
  if (value.component.licence_state !== "APPROVED") {
    return readiness(false, "COMPONENT_LICENCE_NOT_APPROVED");
  }
  let failed = approvalReadiness(
    value.dsp_registration,
    "DSP_REGISTRATION",
    nowMilliseconds,
    today,
  );
  if (failed) return failed;
  failed = approvalReadiness(
    value.product_registration,
    "PRODUCT_REGISTRATION",
    nowMilliseconds,
    today,
  );
  if (failed) return failed;
  if (value.osf_assessment.state !== "APPROVED") {
    return readiness(false, "OSF_ASSESSMENT_NOT_APPROVED");
  }
  if (value.osf_assessment.decision_date > today) {
    return readiness(false, "OSF_DECISION_IN_FUTURE");
  }
  if (value.osf_assessment.revalidation_date < today) {
    return readiness(false, "OSF_REVALIDATION_EXPIRED");
  }
  if (value.evte_access.state !== "APPROVED") {
    return readiness(false, "EVTE_ACCESS_NOT_APPROVED");
  }
  if (parseTrustedDate(value.evte_access.issued_at, "EVTE_ACCESS_ISSUED_AT") > nowMilliseconds) {
    return readiness(false, "EVTE_ACCESS_NOT_YET_VALID");
  }
  if (parseTrustedDate(value.evte_access.expires_at, "EVTE_ACCESS_EXPIRES_AT") <= nowMilliseconds) {
    return readiness(false, "EVTE_ACCESS_EXPIRED");
  }
  if (
    parseTrustedDate(value.endpoint_profile.issued_at, "ENDPOINT_PROFILE_ISSUED_AT") >
    nowMilliseconds
  ) {
    return readiness(false, "ENDPOINT_PROFILE_NOT_YET_VALID");
  }
  if (
    parseTrustedDate(value.endpoint_profile.expires_at, "ENDPOINT_PROFILE_EXPIRES_AT") <=
    nowMilliseconds
  ) {
    return readiness(false, "ENDPOINT_PROFILE_EXPIRED");
  }
  let hasApprovedService = false;
  for (const service of value.services) {
    if (service.enrolment_state === "APPROVED") hasApprovedService = true;
  }
  if (!hasApprovedService) return readiness(false, "SERVICE_ENROLMENT_NOT_APPROVED");
  if (phase === "POST_CONFORMANCE") {
    for (const service of value.services) {
      if (service.enrolment_state !== "APPROVED") {
        return readiness(false, "SERVICE_ENROLMENT_NOT_APPROVED");
      }
      if (service.conformance_state !== "PASSED") {
        return readiness(false, "SERVICE_CONFORMANCE_NOT_PASSED");
      }
    }
  }
  if (parseTrustedDate(value.review.approved_at, "REVIEW_APPROVED_AT") > nowMilliseconds) {
    return readiness(false, "REVIEW_NOT_YET_VALID");
  }
  if (value.review.revalidation_date < today) {
    return readiness(false, "REVIEW_REVALIDATION_EXPIRED");
  }
  return readiness(true, `READY_${phase}`);
}

function decodePublicKey(publicKey) {
  const bytes =
    typeof publicKey === "string"
      ? copyBoundedString(publicKey, MAX_PUBLIC_KEY_BYTES, "PUBLIC_KEY_TOO_LARGE")
      : copyByteInput(publicKey, "PUBLIC_KEY_INPUT", MAX_PUBLIC_KEY_BYTES, "PUBLIC_KEY_TOO_LARGE");
  let pem;
  try {
    pem = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    throw invalid("PUBLIC_KEY_FORMAT");
  }
  if (!CANONICAL_PUBLIC_KEY_PEM.test(pem)) throw invalid("PUBLIC_KEY_FORMAT");
  try {
    const key = createPublicKey(pem);
    if (key.asymmetricKeyType !== "ed25519") throw invalid("PUBLIC_KEY_TYPE");
    if (key.export({ format: "pem", type: "spki" }) !== pem) throw invalid("PUBLIC_KEY_FORMAT");
    return key;
  } catch (error) {
    if (error instanceof Error && error.message.startsWith("SBR_REGISTRATION_INVALID:")) {
      throw error;
    }
    throw invalid("PUBLIC_KEY_FORMAT");
  }
}

function decodeSignature(signature) {
  const bytes =
    typeof signature === "string"
      ? copyBoundedString(signature, MAX_SIGNATURE_BYTES, "SIGNATURE_TOO_LARGE")
      : copyByteInput(signature, "SIGNATURE_ENCODING", MAX_SIGNATURE_BYTES, "SIGNATURE_TOO_LARGE");
  if (bytes.some((byte) => byte > 0x7f)) throw invalid("SIGNATURE_ENCODING");
  const encoded = bytes.toString("ascii");
  if (!STRICT_BASE64_SIGNATURE.test(encoded)) throw invalid("SIGNATURE_ENCODING");
  const decoded = trustedReflectApply(trustedBufferFrom, TrustedBuffer, [
    encoded.slice(0, -1),
    "base64",
  ]);
  if (decoded.byteLength !== 64 || `${decoded.toString("base64")}\n` !== encoded) {
    throw invalid("SIGNATURE_ENCODING");
  }
  return decoded;
}

// Low-level crypto primitive only: it authenticates canonical registration bytes against an
// explicitly supplied test/tooling key, but it cannot produce readiness or a preflight result.
export function verifySbrRegistrationSignature({ manifest, publicKey, signature }) {
  const verified = verify(
    null,
    canonicalizeSbrRegistrationManifest(manifest),
    decodePublicKey(publicKey),
    decodeSignature(signature),
  );
  if (!verified) throw invalid("SIGNATURE_MISMATCH");
  return true;
}

function assertCrossBinding({ component, endpoint, profile, registration }) {
  if (
    profile.environment !== "EVTE" ||
    profile.target !== "darwin/arm64" ||
    profile.component_manifest_sha256 === "NONE" ||
    profile.registration_manifest_sha256 === "NONE" ||
    profile.endpoint_profile_sha256 === "NONE"
  ) {
    throw invalid("PROFILE_SCOPE");
  }
  if (profile.registration_manifest_sha256 !== registration.sha256) {
    throw invalid("REGISTRATION_HASH_MISMATCH");
  }
  if (
    registration.manifest.component.component_manifest_sha256 !== component.sha256 ||
    profile.component_manifest_sha256 !== component.sha256
  ) {
    throw invalid("COMPONENT_HASH_MISMATCH");
  }
  if (
    registration.manifest.endpoint_profile.endpoint_profile_sha256 !== endpoint.sha256 ||
    profile.endpoint_profile_sha256 !== endpoint.sha256
  ) {
    throw invalid("ENDPOINT_HASH_MISMATCH");
  }
  if (
    !endpoint.profile.services.some(
      (service) => service.service_id === registration.manifest.product_id_scope.service_id,
    )
  ) {
    throw invalid("PRODUCT_SERVICE_MISMATCH");
  }
  if (registration.manifest.services.length !== endpoint.profile.services.length) {
    throw invalid("SERVICE_SET_MISMATCH");
  }
  for (let index = 0; index < registration.manifest.services.length; index += 1) {
    if (
      registration.manifest.services[index].service_id !==
      endpoint.profile.services[index].service_id
    ) {
      throw invalid("SERVICE_SET_MISMATCH");
    }
  }
  if (
    registration.manifest.component.name !== component.manifest.component_name ||
    registration.manifest.component.version !== component.manifest.component_version ||
    registration.manifest.component.target !== component.manifest.target
  ) {
    throw invalid("COMPONENT_METADATA_MISMATCH");
  }
  const reference = registration.manifest.endpoint_profile;
  if (
    reference.id !== endpoint.profile.profile_id ||
    reference.revision !== endpoint.profile.revision ||
    reference.issued_at !== endpoint.profile.issued_at ||
    reference.expires_at !== endpoint.profile.expires_at
  ) {
    throw invalid("ENDPOINT_METADATA_MISMATCH");
  }
}

function redactedPreflightResult({
  component,
  endpoint,
  readiness: readinessResult,
  registration,
}) {
  return Object.freeze({
    readiness: Object.freeze({ ...readinessResult }),
    fingerprints: Object.freeze({
      component_sha256: component.sha256,
      endpoint_sha256: endpoint.sha256,
      registration_sha256: registration.sha256,
    }),
    metadata: Object.freeze({
      environment: registration.manifest.environment,
      target: registration.manifest.target,
      endpoint_id: endpoint.profile.profile_id,
      endpoint_revision: endpoint.profile.revision,
    }),
  });
}

function invokeImportedSbr(operation) {
  try {
    return operation();
  } catch (error) {
    let message;
    try {
      message = error instanceof Error ? error.message : undefined;
    } catch {
      throw invalid("CANONICALIZATION");
    }
    if (/^SBR_(?:COMPONENT|PROFILE)_INVALID:[A-Z0-9_]+$/.test(message ?? "")) {
      throw error;
    }
    throw invalid("CANONICALIZATION");
  }
}

function canonicalizationFailure(errors) {
  for (const error of errors) {
    let message;
    try {
      message = error instanceof Error ? error.message : undefined;
    } catch {
      return invalid("CANONICALIZATION");
    }
    if (typeof message === "string" && message.endsWith(":CANONICALIZATION")) {
      return error;
    }
  }
  return undefined;
}

export function authenticateSbrEvteRegistration({
  componentManifestBytes,
  endpointProfileBytes,
  now = new TrustedDate(),
  phase,
  profileBytes,
  profileSignatureBytes,
  registrationBytes,
  registrationSignatureBytes,
}) {
  const nowMilliseconds = assertClock(now);
  const trustedNow = new TrustedDate(nowMilliseconds);
  const registration = parseAndValidateSbrRegistrationManifest(registrationBytes, {
    now: trustedNow,
  });
  const endpoint = parseAndValidateSbrEndpointProfile(endpointProfileBytes, { now: trustedNow });
  const component = invokeImportedSbr(() =>
    parseAndValidateSbrComponentManifest(componentManifestBytes),
  );
  const profile = invokeImportedSbr(() =>
    parseAndValidateSbrProfile(profileBytes, { now: trustedNow }),
  );
  assertCrossBinding({ component, endpoint, profile, registration });

  if (!EVTE_TRUST_ROOT_REGISTERED) {
    const signatureErrors = [];
    try {
      verifySbrRegistrationSignature({
        manifest: registration.manifest,
        publicKey: UNREGISTERED_EVTE_PUBLIC_KEY,
        signature: registrationSignatureBytes,
      });
    } catch (error) {
      signatureErrors.push(error);
    }
    try {
      invokeImportedSbr(() =>
        verifySbrProfileSignature({
          now: trustedNow,
          profile,
          publicKey: UNREGISTERED_EVTE_PUBLIC_KEY,
          signature: profileSignatureBytes,
        }),
      );
    } catch (error) {
      signatureErrors.push(error);
    }
    const unsafeInput = canonicalizationFailure(signatureErrors);
    if (unsafeInput) throw unsafeInput;
    return redactedPreflightResult({
      component,
      endpoint,
      readiness: readiness(false, "EVTE_TRUST_ROOT_UNREGISTERED"),
      registration,
    });
  }

  verifySbrRegistrationSignature({
    manifest: registration.manifest,
    publicKey: UNREGISTERED_EVTE_PUBLIC_KEY,
    signature: registrationSignatureBytes,
  });
  invokeImportedSbr(() =>
    verifySbrProfileSignature({
      now: trustedNow,
      profile,
      publicKey: UNREGISTERED_EVTE_PUBLIC_KEY,
      signature: profileSignatureBytes,
    }),
  );
  return redactedPreflightResult({
    component,
    endpoint,
    readiness: evaluateSbrRegistrationReadiness({
      manifest: registration.manifest,
      now: trustedNow,
      phase,
    }),
    registration,
  });
}
