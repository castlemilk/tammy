import path from "node:path";

import {
  type DesktopLaunchScenario,
  parseLaunchScenarioArgument,
  TAMMY_LAUNCH_SCENARIO_SWITCH,
} from "../shared/launch-scenario";
import { parseLocalLaunchArguments } from "./core-launch";

export { TAMMY_LAUNCH_SCENARIO_SWITCH } from "../shared/launch-scenario";

export interface DesktopLaunchAuthority {
  readonly kind: DesktopLaunchScenario;
  readonly userDataPath?: string;
}

const LOCAL_ROOT_PREFIX: Partial<Record<DesktopLaunchScenario, string>> = {
  "accounting-fresh": "tammy-accounting-fresh-",
  "sbr-doctor": "tammy-sbr-doctor-",
  "sbr-simulator": "tammy-sbr-simulator-",
};

export function parseDesktopLaunchScenario(
  arguments_: readonly string[],
): DesktopLaunchAuthority {
  const kind = parseLaunchScenarioArgument(arguments_);
  const local = parseLocalLaunchArguments(
    arguments_.filter((argument) => !argument.startsWith(TAMMY_LAUNCH_SCENARIO_SWITCH)),
  );
  const basename = local.userDataPath ? path.basename(local.userDataPath) : undefined;
  const looksLikeOwnedScenarioRoot = Object.values(LOCAL_ROOT_PREFIX).some((prefix) =>
    basename?.startsWith(prefix),
  );
  const expectedPrefix = LOCAL_ROOT_PREFIX[kind];

  if (
    (kind !== "accounting" && local.userDataPath === undefined) ||
    (kind === "accounting" && looksLikeOwnedScenarioRoot) ||
    (looksLikeOwnedScenarioRoot && expectedPrefix !== undefined && !basename?.startsWith(expectedPrefix))
  ) {
    throw new Error("LOCAL_SCENARIO_INVALID");
  }
  return local.userDataPath === undefined
    ? { kind }
    : { kind, userDataPath: local.userDataPath };
}

export function requiresSimulatorProfile(kind: DesktopLaunchScenario): boolean {
  return kind === "sbr-simulator" || kind === "sbr-doctor";
}

export function rendererLaunchScenarioArguments(
  kind: DesktopLaunchScenario,
): string[] {
  return kind === "accounting" ? [] : [`${TAMMY_LAUNCH_SCENARIO_SWITCH}${kind}`];
}
