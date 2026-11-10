/**
 * Stable error codes by HTTP status, as documented in docs/development/web-api.md. The first code
 * for a status is its default; a supplied code is kept only when the contract lists it for that
 * status, so for example every 409 carries `conflict` and every 413 carries `request_too_large`.
 */
const CODES = Object.freeze({
  400: Object.freeze(["invalid_argument", "invalid_request"]),
  401: Object.freeze(["unauthenticated"]),
  403: Object.freeze(["forbidden"]),
  404: Object.freeze(["not_found"]),
  409: Object.freeze(["conflict"]),
  413: Object.freeze(["request_too_large"]),
  429: Object.freeze(["resource_exhausted"]),
  500: Object.freeze(["internal"]),
});

/** Return the documented code for an error status, keeping `code` only where the contract allows it. */
export function errorCode(status, code) {
  const allowed = CODES[status] ?? (status >= 500 ? CODES[500] : CODES[400]);
  return allowed.includes(code) ? code : allowed[0];
}

/** Create a client-safe error with an HTTP status and its documented code. */
export function httpError(message, status = 400, code = undefined) {
  return Object.assign(new Error(message), { status, code: errorCode(status, code) });
}
