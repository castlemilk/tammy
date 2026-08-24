export const TAMMY_LAUNCH_SCENARIO_SWITCH = "--tammy-launch-scenario=";

export type DesktopLaunchScenario =
  | "accounting"
  | "accounting-fresh"
  | "sbr-simulator"
  | "sbr-doctor";

const EXPLICIT_SCENARIOS = new Set<DesktopLaunchScenario>([
  "accounting-fresh",
  "sbr-simulator",
  "sbr-doctor",
]);

export function parseLaunchScenarioArgument(arguments_: readonly string[]): DesktopLaunchScenario {
  const candidates = arguments_.filter((argument) =>
    argument.startsWith(TAMMY_LAUNCH_SCENARIO_SWITCH),
  );
  if (candidates.length === 0) return "accounting";
  if (candidates.length !== 1) throw new Error("LOCAL_SCENARIO_INVALID");
  const scenario = candidates[0]?.slice(TAMMY_LAUNCH_SCENARIO_SWITCH.length);
  if (!EXPLICIT_SCENARIOS.has(scenario as DesktopLaunchScenario)) {
    throw new Error("LOCAL_SCENARIO_INVALID");
  }
  return scenario as DesktopLaunchScenario;
}
