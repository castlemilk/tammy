export interface InspectorAwareQuitApplication {
  readonly app: {
    quit(): void;
    once(event: "before-quit" | "will-quit", listener: () => void): unknown;
  };
  readonly __closeInspectorForTest?: InspectorClose;
  readonly __writeMarkerForTest?: ShutdownMarkerWriter;
}

export type InspectorClose = () => void;
export type ShutdownMarkerWriter = (marker: string) => void;

// Playwright calls app.quit() before closing its Node inspector transport. Close
// that inspector first; Electron and Core still follow their normal quit path.
export function installInspectorAwareQuit(electron: InspectorAwareQuitApplication): void {
  const app = electron.app;
  const originalQuit = app.quit;
  const closeInspector =
    electron.__closeInspectorForTest ??
    (() => {
      const inspector = process.getBuiltinModule("node:inspector");
      if (!inspector) throw new Error("NODE_INSPECTOR_UNAVAILABLE");
      inspector.close();
    });
  const writeMarker =
    electron.__writeMarkerForTest ?? ((marker: string) => process.stderr.write(`${marker}\n`));
  let quitIntercepted = false;
  const marker = (value: string): void => {
    try {
      writeMarker(value);
    } catch {
      // Test diagnostics must not prevent Electron's normal quit path.
    }
  };

  marker("LOCAL_ENGINE_E2E_SHUTDOWN_QUIT_INTERCEPTOR_INSTALLED");
  app.once("before-quit", () => marker("LOCAL_ENGINE_E2E_SHUTDOWN_BEFORE_QUIT"));
  app.once("will-quit", () => marker("LOCAL_ENGINE_E2E_SHUTDOWN_WILL_QUIT"));
  app.quit = () => {
    if (quitIntercepted) return;
    quitIntercepted = true;
    app.quit = originalQuit;
    marker("LOCAL_ENGINE_E2E_SHUTDOWN_QUIT_INTERCEPTED");
    marker("LOCAL_ENGINE_E2E_SHUTDOWN_INSPECTOR_CLOSE_STARTED");
    try {
      closeInspector();
    } finally {
      marker("LOCAL_ENGINE_E2E_SHUTDOWN_INSPECTOR_CLOSE_SETTLED");
      originalQuit.call(app);
    }
  };
}
