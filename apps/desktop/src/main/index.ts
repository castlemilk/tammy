import { startDesktopApplication } from "./index-lifecycle";
import { createProductionDependencies } from "./index-production";

export * from "./index-lifecycle";

if (process.versions.electron) {
  void startDesktopApplication(createProductionDependencies()).catch(() => undefined);
}
