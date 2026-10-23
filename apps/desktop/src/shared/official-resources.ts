import { abnLookupUrl } from "./abn.ts";

export const TAMMY_PUBLIC_LINKS = Object.freeze({
  privacyPolicy: "https://tammy-accounting.castlemilk.chatgpt.site/privacy",
  support: "https://tammy-accounting.castlemilk.chatgpt.site/support",
});

export const OFFICIAL_RESOURCES = Object.freeze({
  ram: "https://softwaredevelopers.ato.gov.au/usingmygovidramandmachinecredentials",
  dsp: "https://softwaredevelopers.ato.gov.au/OnlineservicesforDSPs",
  securityFramework:
    "https://softwaredevelopers.ato.gov.au/operational_framework/meeting-requirements",
  abnLookup: "https://abr.business.gov.au/",
  abnWebServices: "https://abr.business.gov.au/Tools/WebServices",
  privacyComplaints: "https://www.oaic.gov.au/privacy/privacy-complaints",
});

export const APPROVED_PUBLIC_URLS = Object.freeze([
  ...Object.values(TAMMY_PUBLIC_LINKS),
  ...Object.values(OFFICIAL_RESOURCES),
]);

/** The browser handoff contains only the ABN the user explicitly chose to look up. */
export function isOfficialAbnLookupUrl(value: string): boolean {
  const match = /^https:\/\/abr\.business\.gov\.au\/ABN\/View\?abn=([0-9]{11})$/.exec(value);
  return Boolean(match?.[1] && abnLookupUrl(match[1]) === value);
}
