/** Validate the gateway boundary before reading request bodies or cookies. */
export function isTrustedRequest(request, config) {
  if (request.host !== config.host) return false;
  if (request.fetchSite === "cross-site") return false;
  if (request.origin !== undefined && request.origin !== config.origin) return false;
  if (request.method === "GET" || request.method === "HEAD") return true;
  return (
    request.origin === config.origin &&
    typeof request.contentType === "string" &&
    /^application\/json(?:\s*;\s*charset=utf-8)?$/i.test(request.contentType)
  );
}
