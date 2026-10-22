export function consumeExpectedCspViolations(
  messages: readonly string[],
  urls: readonly string[],
): string[] {
  const remaining = [...messages];
  for (const url of urls) {
    const prefix = `Connecting to '${url}'`;
    const directive = `Content Security Policy directive: "connect-src 'none'"`;
    consumeSingleMatch(
      remaining,
      (message) => message.includes(prefix) && message.includes(directive),
    );
    const fetchRejection = `Fetch API cannot load ${url}. Refused to connect because it violates the document's Content Security Policy.`;
    consumeSingleMatch(remaining, (message) => message === fetchRejection);
  }
  return remaining;
}

function consumeSingleMatch(messages: string[], matches: (message: string) => boolean): void {
  const matching = messages.flatMap((message, index) => (matches(message) ? [index] : []));
  const [matchingIndex] = matching;
  if (matching.length !== 1 || matchingIndex === undefined) {
    throw new Error("CSP_VIOLATION_EVIDENCE_INVALID");
  }
  messages.splice(matchingIndex, 1);
}
