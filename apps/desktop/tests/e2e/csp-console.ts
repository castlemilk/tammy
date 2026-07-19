export function consumeExpectedCspViolations(
  messages: readonly string[],
  urls: readonly string[],
): string[] {
  const remaining = [...messages];
  for (const url of urls) {
    const prefix = `Refused to connect to '${url}'`;
    const directive = `Content Security Policy directive: "connect-src 'none'"`;
    const matching = remaining.flatMap((message, index) =>
      message.includes(prefix) && message.includes(directive) ? [index] : [],
    );
    const [matchingIndex] = matching;
    if (matching.length !== 1 || matchingIndex === undefined) {
      throw new Error("CSP_VIOLATION_EVIDENCE_INVALID");
    }
    remaining.splice(matchingIndex, 1);
  }
  return remaining;
}
