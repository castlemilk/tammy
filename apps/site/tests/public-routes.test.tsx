import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { metadata } from "../app/layout";
import Home from "../app/page";
import PrivacyPage from "../app/privacy/page";
import SupportPage from "../app/support/page";

afterEach(cleanup);

describe("Tammy public routes", () => {
  it("states the supported platform and preparation-only boundary", () => {
    render(<Home />);

    expect(screen.getByRole("heading", { name: "Local accounting for Australia" })).toBeTruthy();
    expect(screen.getByText(/macOS 14 or later.*Apple silicon/i)).toBeTruthy();
    expect(screen.getByText(/preparation-only.*not lodged/i)).toBeTruthy();
    expect(screen.getByText("Gamma Systems Pty Ltd")).toBeTruthy();
    expect(
      screen.getByText(
        /encrypted workspace.*journals.*source documents.*bank transactions.*GST workpaper/i,
      ),
    ).toBeTruthy();
    expect(screen.queryByText(/company (?:EOFY|tax return)/i)).toBeNull();
    expect(String(metadata.description)).not.toMatch(/company\s+EOFY/i);
    expect(String(metadata.description)).not.toMatch(/tax return preparation/i);
  });

  it("renders the canonical app/site/support privacy boundaries", () => {
    render(<PrivacyPage />);

    expect(screen.getByRole("heading", { name: "Privacy policy" })).toBeTruthy();
    expect(screen.getByText(/does not automatically send your workspace records/i)).toBeTruthy();
    expect(screen.getByRole("navigation", { name: "Privacy policy contents" })).toBeTruthy();
    expect(screen.getByText(/Relationship Authorisation Manager, or RAM/)).toBeTruthy();
    expect(screen.getByText(/synthetic, network-disabled simulator/)).toBeTruthy();
    expect(
      screen.getByText(
        /hosting provider may process technical request information/i,
      ),
    ).toBeTruthy();
    expect(screen.getByText(/Google processes it as the email service provider/i)).toBeTruthy();
    expect(screen.getByRole("link", { name: "ABN Lookup page" }).getAttribute("href")).toBe(
      "https://abr.business.gov.au/",
    );
    expect(
      screen.getByRole("link", { name: "Office of the Australian Information Commissioner" }),
    ).toBeTruthy();
  });

  it("renders only the canonical support and deletion inventory", () => {
    render(<SupportPage />);

    expect(
      screen.getByRole("link", { name: /ben\.ebsworth@gmail\.com/i }).getAttribute("href"),
    ).toBe("mailto:ben.ebsworth@gmail.com");
    expect(screen.getByText(/com\.tammy\.workspace/)).toBeTruthy();
    expect(screen.queryByText(/simulator-v2/)).toBeNull();
    expect(screen.getByText(/version 0\.1\.0/i)).toBeTruthy();
    expect(screen.getByText(/app deletion alone does not remove/i)).toBeTruthy();
    expect(screen.getByText(/privacy, access, or correction request/i)).toBeTruthy();
    expect(screen.getByText(/synthetic, network-disabled simulator/)).toBeTruthy();
  });
});
