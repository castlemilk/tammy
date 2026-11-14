import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import Home from "../app/page";
import { metadata as rootMetadata } from "../app/layout";
import { publicContent } from "../content/public-content.generated";

/*
 * scripts/check-public-site.mjs records tammy.tax as App Store public-site evidence only
 * when the home page states these facts. Keep this list in step with its checks for "/".
 */
describe("home page facts required by the App Store public-site check", () => {
  it("states the app name, publisher, platform and reporting boundary", () => {
    const { container } = render(<Home />);
    const text = (container.textContent ?? "").replace(/\s+/g, " ");
    const title = String((rootMetadata.title as { default: string }).default);
    expect(title).toContain(publicContent.identity.appStoreName);
    expect(publicContent.identity.appStoreName).toBe("Tammy Accounting");
    for (const pattern of [
      /Gamma Systems Pty Ltd/i,
      /macOS 14/i,
      /arm64/i,
      /preparation-only/i,
      /not lodged/i,
    ])
      expect(text).toMatch(pattern);
    for (const forbidden of [/\bTODO\b/i, /lorem ipsum/i, /TestFlight/i, /lodges? with the ATO/i])
      expect(text).not.toMatch(forbidden);
    for (const route of ["/", "/privacy", "/support"])
      expect(container.querySelector(`a[href="${route}"]`)).not.toBeNull();
  });
});
