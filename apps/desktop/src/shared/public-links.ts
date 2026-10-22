export interface PublicLinks {
  readonly privacyPolicy?: string;
  readonly support?: string;
}

function httpsUrl(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    return undefined;
  }
  if (
    parsed.protocol !== "https:" ||
    parsed.username !== "" ||
    parsed.password !== "" ||
    parsed.hash !== ""
  ) {
    return undefined;
  }
  return parsed.href;
}

export function readPublicLinks(
  environment: Readonly<Record<string, unknown>> = import.meta.env,
): PublicLinks {
  const privacyPolicy = httpsUrl(environment.VITE_TAMMY_PRIVACY_POLICY_URL);
  const support = httpsUrl(environment.VITE_TAMMY_SUPPORT_URL);
  return Object.freeze({
    ...(privacyPolicy ? { privacyPolicy } : {}),
    ...(support ? { support } : {}),
  });
}
