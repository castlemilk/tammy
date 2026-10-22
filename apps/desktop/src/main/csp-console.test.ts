// @vitest-environment node

import { describe, expect, it } from "vitest";

import { consumeExpectedCspViolations } from "../../tests/e2e/csp-console";

const urls = ["https://example.com/tammy-csp-probe", "http://example.com/tammy-csp-probe"] as const;

function violation(url: string) {
  return `Connecting to '${url}' violates the following Content Security Policy directive: "connect-src 'none'". The action has been blocked.`;
}

function fetchRejection(url: string) {
  return `Fetch API cannot load ${url}. Refused to connect because it violates the document's Content Security Policy.`;
}

describe("consumeExpectedCspViolations", () => {
  it("consumes one narrow connect-src violation and its exact fetch rejection for each probe", () => {
    expect(
      consumeExpectedCspViolations(
        [
          fetchRejection(urls[1]),
          violation(urls[1]),
          "unexpected renderer error",
          violation(urls[0]),
          fetchRejection(urls[0]),
        ],
        urls,
      ),
    ).toEqual(["unexpected renderer error"]);
  });

  it.each([
    [
      "missing companion fetch rejection",
      [violation(urls[0]), fetchRejection(urls[0]), violation(urls[1])],
    ],
    [
      "wrong directive",
      [
        violation(urls[0]),
        fetchRejection(urls[0]),
        `Connecting to '${urls[1]}' violates the following Content Security Policy directive: "default-src 'self'". The action has been blocked.`,
        fetchRejection(urls[1]),
      ],
    ],
    [
      "duplicate evidence",
      [
        violation(urls[0]),
        fetchRejection(urls[0]),
        violation(urls[1]),
        violation(urls[1]),
        fetchRejection(urls[1]),
      ],
    ],
  ])("rejects %s instead of broadly suppressing console errors", (_name, messages) => {
    expect(() => consumeExpectedCspViolations(messages, urls)).toThrow(
      "CSP_VIOLATION_EVIDENCE_INVALID",
    );
  });
});
