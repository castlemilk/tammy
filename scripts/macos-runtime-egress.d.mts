export interface MacOSEgressPreflight {
  readonly auditSamples: number;
  readonly childInheritanceDenied: boolean;
  readonly dnsDenied: boolean;
  readonly loopbackAllowed: boolean;
  readonly nonLoopbackDenied: boolean;
}

export interface ExternalHandoffEvent {
  readonly occurredAt: string;
  readonly url: string;
  readonly userGesture: true;
}

export const MACOS_RUNTIME_EGRESS_SANDBOX_PROFILE: string;

export function detectMacOSEgressEnforcer(options?: {
  readonly platform?: string;
  readonly probe?: () => Promise<MacOSEgressPreflight>;
}): Promise<{ readonly kind: "sandbox-exec"; readonly preflight: MacOSEgressPreflight }>;

export function createExternalHandoffEvent(input: {
  readonly allowedUrls: readonly string[];
  readonly occurredAt: string;
  readonly url: string;
  readonly userGesture: boolean;
}): ExternalHandoffEvent;

export function validateMacOSRuntimeEgressEvidence(value: unknown): unknown;
