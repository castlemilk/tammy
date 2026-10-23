import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { PrivacyScreen, PrivacyStatement } from "./privacy-statement";

afterEach(() => {
  cleanup();
  vi.unstubAllEnvs();
});

describe("PrivacyStatement", () => {
  it("summarises local handling and opens the complete policy in Tammy", () => {
    const onPrivacy = vi.fn();
    render(<PrivacyStatement onPrivacy={onPrivacy} />);

    expect(screen.getByRole("heading", { name: "Privacy at a glance" })).toBeTruthy();
    expect(screen.getByText(/encrypted on this Mac/)).toBeTruthy();
    expect(screen.getByText(/does not include analytics, advertising, or tracking/)).toBeTruthy();
    expect(screen.getByText(/Files are read only when you choose them/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Read full privacy policy" }));
    expect(onPrivacy).toHaveBeenCalledOnce();
  });

  it("links the published privacy and support pages when release links are configured", () => {
    vi.stubEnv("VITE_TAMMY_PRIVACY_POLICY_URL", "https://example.com/tammy/privacy");
    vi.stubEnv("VITE_TAMMY_SUPPORT_URL", "https://example.com/tammy/support");

    render(<PrivacyStatement />);

    expect(
      screen.getByRole("link", { name: "Read the published privacy policy" }).getAttribute("href"),
    ).toBe("https://example.com/tammy/privacy");
    expect(
      screen.getByRole("link", { name: "Support and privacy requests" }).getAttribute("href"),
    ).toBe("https://example.com/tammy/support");
  });

  it("uses the canonical published pages when release URLs are not configured", () => {
    vi.stubEnv("VITE_TAMMY_PRIVACY_POLICY_URL", "");
    vi.stubEnv("VITE_TAMMY_SUPPORT_URL", "");

    render(<PrivacyStatement />);

    expect(
      screen.getByRole("link", { name: "Read the published privacy policy" }).getAttribute("href"),
    ).toBe("https://tammy-accounting.castlemilk.chatgpt.site/privacy");
    expect(
      screen.getByRole("link", { name: "Support and privacy requests" }).getAttribute("href"),
    ).toBe("https://tammy-accounting.castlemilk.chatgpt.site/support");
  });
});

describe("PrivacyScreen", () => {
  it("renders the canonical policy, section navigation, and official reference links", () => {
    render(<PrivacyScreen onBack={() => undefined} />);

    expect(screen.getByRole("heading", { name: "Privacy and support", level: 1 })).toBeTruthy();
    const contents = screen.getByRole("navigation", { name: "Privacy policy contents" });
    expect(within(contents).getByRole("link", { name: "ATO, RAM, and SBR" })).toBeTruthy();
    expect(
      screen.getByRole("heading", { name: "Information in your workspace", level: 2 }),
    ).toBeTruthy();
    expect(screen.getByRole("link", { name: "ABN Lookup page" }).getAttribute("href")).toBe(
      "https://abr.business.gov.au/",
    );
    expect(
      screen.getByRole("link", { name: "myID, RAM, and machine credentials" }).getAttribute("href"),
    ).toBe("https://softwaredevelopers.ato.gov.au/usingmygovidramandmachinecredentials");
    expect(
      screen
        .getByRole("link", { name: "Digital Service Provider security framework" })
        .getAttribute("href"),
    ).toBe("https://softwaredevelopers.ato.gov.au/operational_framework/meeting-requirements");
    expect(
      screen
        .getByRole("link", { name: "Office of the Australian Information Commissioner" })
        .getAttribute("href"),
    ).toBe("https://www.oaic.gov.au/privacy/privacy-complaints");
    expect(screen.getByText(/synthetic, network-disabled simulator/)).toBeTruthy();
    expect(
      screen.getByText(/does not send BAS or other tax submissions to the ATO or SBR/),
    ).toBeTruthy();
    expect(screen.getAllByText("ben.ebsworth@gmail.com")).toHaveLength(2);
    expect(screen.queryByRole("link", { name: "ben.ebsworth@gmail.com" })).toBeNull();
  });

  it("scrolls and focuses a policy section without changing the app route hash", () => {
    render(<PrivacyScreen onBack={() => undefined} />);

    const contents = screen.getByRole("navigation", { name: "Privacy policy contents" });
    const sectionLink = within(contents).getByRole("link", { name: "ATO, RAM, and SBR" });
    const sectionHeading = screen.getByRole("heading", {
      name: "ATO, RAM, and SBR",
      level: 2,
    });
    const scrollIntoView = vi.fn();
    Object.defineProperty(sectionHeading, "scrollIntoView", {
      configurable: true,
      value: scrollIntoView,
    });
    const currentHash = window.location.hash;

    fireEvent.click(sectionLink);

    expect(sectionLink.getAttribute("href")).toBe("#privacy-ato-ram-and-sbr");
    expect(scrollIntoView).toHaveBeenCalledWith({ block: "start" });
    expect(document.activeElement).toBe(sectionHeading);
    expect(window.location.hash).toBe(currentHash);
  });

  it("returns to the calling app route", () => {
    const onBack = vi.fn();
    render(<PrivacyScreen onBack={onBack} />);

    fireEvent.click(screen.getByRole("button", { name: "Back to Tammy" }));
    expect(onBack).toHaveBeenCalledOnce();
  });
});
