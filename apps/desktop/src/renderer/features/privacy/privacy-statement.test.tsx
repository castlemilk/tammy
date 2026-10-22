import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { PrivacyStatement } from "./privacy-statement";

describe("PrivacyStatement", () => {
  afterEach(() => vi.unstubAllEnvs());

  it("makes local storage, collection, file access and retention visible in-app", () => {
    render(<PrivacyStatement />);

    expect(screen.getByRole("heading", { name: "Privacy" })).toBeTruthy();
    expect(screen.getByText(/encrypted workspace on this Mac/)).toBeTruthy();
    expect(screen.getByText(/does not include analytics, advertising or tracking/)).toBeTruthy();
    expect(screen.getByText(/Files are read only when you choose them/)).toBeTruthy();
    expect(screen.getByText(/remain until you remove the workspace/)).toBeTruthy();
  });

  it("links the release privacy policy and support pages in-app", () => {
    vi.stubEnv("VITE_TAMMY_PRIVACY_POLICY_URL", "https://example.com/tammy/privacy");
    vi.stubEnv("VITE_TAMMY_SUPPORT_URL", "https://example.com/tammy/support");

    render(<PrivacyStatement />);

    expect(screen.getByRole("link", { name: "Privacy policy" }).getAttribute("href")).toBe(
      "https://example.com/tammy/privacy",
    );
    expect(screen.getByRole("link", { name: "Support" }).getAttribute("href")).toBe(
      "https://example.com/tammy/support",
    );
  });
});
