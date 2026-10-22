import { describe, expect, it, vi } from "vitest";
import { installInspectorAwareQuit } from "../../tests/e2e/electron-quit-sequencing";

describe("installInspectorAwareQuit", () => {
  it("closes the inspector before the original quit and restores app.quit once", () => {
    const events: string[] = [];
    const app = {
      once: vi.fn(),
      quit() {
        events.push("APP_QUIT");
      },
    };
    const originalQuit = app.quit;
    const closeInspector = vi.fn(() => events.push("INSPECTOR_CLOSE"));

    installInspectorAwareQuit({
      app,
      __closeInspectorForTest: closeInspector,
      __writeMarkerForTest: (marker) => events.push(marker),
    });

    const wrappedQuit = app.quit;
    expect(wrappedQuit).not.toBe(originalQuit);
    wrappedQuit.call(app);

    expect(events).toEqual([
      "LOCAL_ENGINE_E2E_SHUTDOWN_QUIT_INTERCEPTOR_INSTALLED",
      "LOCAL_ENGINE_E2E_SHUTDOWN_QUIT_INTERCEPTED",
      "LOCAL_ENGINE_E2E_SHUTDOWN_INSPECTOR_CLOSE_STARTED",
      "INSPECTOR_CLOSE",
      "LOCAL_ENGINE_E2E_SHUTDOWN_INSPECTOR_CLOSE_SETTLED",
      "APP_QUIT",
    ]);
    expect(closeInspector).toHaveBeenCalledOnce();
    expect(app.quit).toBe(originalQuit);

    app.quit();
    expect(events.at(-1)).toBe("APP_QUIT");
    expect(closeInspector).toHaveBeenCalledOnce();
  });

  it("still calls the original Electron quit when inspector closure fails", () => {
    const events: string[] = [];
    const app = {
      once: vi.fn(),
      quit() {
        events.push("APP_QUIT");
      },
    };
    const originalQuit = app.quit;
    const failure = new Error("inspector close failed");
    const closeInspector = vi.fn(() => {
      throw failure;
    });

    installInspectorAwareQuit({
      app,
      __closeInspectorForTest: closeInspector,
      __writeMarkerForTest: (marker) => events.push(marker),
    });

    expect(() => app.quit()).toThrow(failure);
    expect(events).toEqual([
      "LOCAL_ENGINE_E2E_SHUTDOWN_QUIT_INTERCEPTOR_INSTALLED",
      "LOCAL_ENGINE_E2E_SHUTDOWN_QUIT_INTERCEPTED",
      "LOCAL_ENGINE_E2E_SHUTDOWN_INSPECTOR_CLOSE_STARTED",
      "LOCAL_ENGINE_E2E_SHUTDOWN_INSPECTOR_CLOSE_SETTLED",
      "APP_QUIT",
    ]);
    expect(app.quit).toBe(originalQuit);
  });
});
