import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { SystemDiagnostics, TammyDesktopAPI } from "../shared/desktop-api";
import { App } from "./app";

const diagnostics: SystemDiagnostics = {
  apiVersion: "tammy.v1",
  coreVersion: "0.1.0",
  runtimeMode: "offline",
  networkRequired: false,
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve;
    reject = promiseReject;
  });

  return { promise, reject, resolve };
}

function installDesktopAPI(getSystemDiagnostics: TammyDesktopAPI["getSystemDiagnostics"]) {
  Object.defineProperty(window, "tammy", {
    configurable: true,
    value: Object.freeze({ getSystemDiagnostics } satisfies TammyDesktopAPI),
  });
}

afterEach(() => {
  Reflect.deleteProperty(window, "tammy");
});

describe("App", () => {
  it("announces the local engine while diagnostics are loading", () => {
    const pending = deferred<SystemDiagnostics>();
    installDesktopAPI(vi.fn(() => pending.promise));

    render(<App />);

    const status = screen.getByRole("status");
    expect(status.getAttribute("aria-live")).toBe("polite");
    expect(status.textContent).toContain("Starting local engine");
  });

  it("shows the offline-ready foundation without future modules", async () => {
    installDesktopAPI(vi.fn().mockResolvedValue(diagnostics));

    render(<App />);

    const status = await screen.findByRole("status");
    await waitFor(() => expect(status.textContent).toContain("Local engine ready"));
    expect(status.textContent).toContain("Offline");
    expect(screen.getByText("tammy.v1")).toBeTruthy();
    expect(screen.getByText("0.1.0")).toBeTruthy();
    expect(screen.getByText("No cloud required")).toBeTruthy();

    const setupAction = screen.getByRole("button", { name: "Workspace setup comes next" });
    expect(setupAction.hasAttribute("disabled")).toBe(true);

    const navigation = screen.getByRole("navigation", { name: "Workspace" });
    expect(within(navigation).getByRole("link", { name: "Overview" })).toBeTruthy();

    for (const futureModule of ["Accounts", "Journal", "BAS", "Submissions", "Audit"]) {
      expect(screen.queryByText(futureModule)).toBeNull();
    }
  });

  it("keeps failure copy safe and retries through the typed desktop method", async () => {
    const retry = deferred<SystemDiagnostics>();
    const getSystemDiagnostics = vi
      .fn<TammyDesktopAPI["getSystemDiagnostics"]>()
      .mockRejectedValueOnce(
        new Error("capability=secret-token certificatePin=sha256:raw 127.0.0.1:45000 readiness"),
      )
      .mockImplementationOnce(() => retry.promise);
    installDesktopAPI(getSystemDiagnostics);
    const user = userEvent.setup();

    render(<App />);

    const status = await screen.findByRole("status");
    await waitFor(() => expect(status.textContent).toContain("Local engine unavailable"));
    expect(document.body.textContent).not.toContain("secret-token");
    expect(document.body.textContent).not.toContain("certificatePin");
    expect(document.body.textContent).not.toContain("127.0.0.1");
    expect(document.body.textContent).not.toContain("readiness");

    const retryButton = screen.getByRole("button", { name: "Retry local engine" });
    await user.click(retryButton);
    expect(getSystemDiagnostics).toHaveBeenCalledTimes(2);
    expect(status.textContent).toContain("Starting local engine");

    retry.resolve(diagnostics);
    await waitFor(() => expect(status.textContent).toContain("Local engine ready"));
  });

  it("uses semantic landmarks, heading order, and keyboard focus order", async () => {
    installDesktopAPI(vi.fn().mockRejectedValue(new Error("unavailable")));
    const user = userEvent.setup();

    render(<App />);

    expect(screen.getByRole("navigation", { name: "Workspace" })).toBeTruthy();
    expect(screen.getByRole("main")).toBeTruthy();
    expect(screen.getByRole("heading", { level: 1, name: "Overview" })).toBeTruthy();
    expect(
      screen.getByRole("heading", { level: 2, name: "Local workspace foundation" }),
    ).toBeTruthy();

    await screen.findByRole("button", { name: "Retry local engine" });
    await user.tab();
    expect(document.activeElement).toBe(screen.getByRole("link", { name: "Overview" }));
    await user.tab();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Retry local engine" }));
  });
});
