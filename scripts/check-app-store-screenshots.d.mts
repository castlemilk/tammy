export function validateScreenshotFixture<T>(value: T): T;
export function scanScreenshotInputs<T>(
  value: T,
  options?: { readonly allowSetupOnlyAbn?: boolean },
): T;
export function normalizeAccessibilitySnapshot(snapshot: string): string[];
