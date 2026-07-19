// @vitest-environment node

import { expect, it } from "vitest";

import playwrightConfig from "../../playwright.config";

it("retains Playwright output only for failed packaged tests", () => {
  expect(playwrightConfig.preserveOutput).toBe("failures-only");
});
