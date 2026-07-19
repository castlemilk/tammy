// @vitest-environment node

import { describe, expect, it } from "vitest";

import { consumeExpectedCspViolations } from "../../tests/e2e/csp-console";

const urls = ["https://example.com/tammy-csp-probe", "http://example.com/tammy-csp-probe"] as const;

function violation(url: string) {
  return `Refused to connect to '${url}' because it violates the following Content Security Policy directive: "connect-src 'none'".`;
}

describe("consumeExpectedCspViolations", () => {
  it("consumes exactly one narrow connect-src none violation for each exact probe URL", () => {
    expect(
      consumeExpectedCspViolations(
        [violation(urls[1]), "unexpected renderer error", violation(urls[0])],
        urls,
      ),
    ).toEqual(["unexpected renderer error"]);
  });

  it.each([
    ["missing URL", [violation(urls[0])]],
    [
      "wrong directive",
      [
        violation(urls[0]),
        `Refused to connect to '${urls[1]}' because it violates the following Content Security Policy directive: "default-src 'self'".`,
      ],
    ],
    ["duplicate evidence", [violation(urls[0]), violation(urls[1]), violation(urls[1])]],
  ])("rejects %s instead of broadly suppressing console errors", (_name, messages) => {
    expect(() => consumeExpectedCspViolations(messages, urls)).toThrow(
      "CSP_VIOLATION_EVIDENCE_INVALID",
    );
  });
});
