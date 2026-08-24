import { describe, expect, it } from "vitest";

import {
  TAMMY_LAUNCH_SCENARIO_SWITCH,
  parseDesktopLaunchScenario,
  rendererLaunchScenarioArguments,
  requiresSimulatorProfile,
} from "./launch-scenario";

describe("desktop launch scenario authority", () => {
  it("keeps an ordinary launch free of simulator authority", () => {
    expect(parseDesktopLaunchScenario(["Tammy"])).toEqual({ kind: "accounting" });
  });

  it("loads the simulator profile only for simulator-authorized scenarios", () => {
    expect(requiresSimulatorProfile("accounting")).toBe(false);
    expect(requiresSimulatorProfile("accounting-fresh")).toBe(false);
    expect(requiresSimulatorProfile("sbr-simulator")).toBe(true);
    expect(requiresSimulatorProfile("sbr-doctor")).toBe(true);
  });

  it("adds renderer authority only for explicit non-accounting scenarios", () => {
    expect(rendererLaunchScenarioArguments("accounting")).toEqual([]);
    expect(rendererLaunchScenarioArguments("accounting-fresh")).toEqual([
      "--tammy-launch-scenario=accounting-fresh",
    ]);
    expect(rendererLaunchScenarioArguments("sbr-simulator")).toEqual([
      "--tammy-launch-scenario=sbr-simulator",
    ]);
    expect(rendererLaunchScenarioArguments("sbr-doctor")).toEqual([
      "--tammy-launch-scenario=sbr-doctor",
    ]);
  });

  it("binds isolated roots to the matching explicit scenario", () => {
    expect(
      parseDesktopLaunchScenario([
        "Tammy",
        "--user-data-dir=/private/tmp/tammy-accounting-fresh-fixed",
        `${TAMMY_LAUNCH_SCENARIO_SWITCH}accounting-fresh`,
      ]),
    ).toEqual({
      kind: "accounting-fresh",
      userDataPath: "/private/tmp/tammy-accounting-fresh-fixed",
    });
    expect(
      parseDesktopLaunchScenario([
        "Tammy",
        "--user-data-dir=/private/tmp/tammy-sbr-simulator-fixed",
        `${TAMMY_LAUNCH_SCENARIO_SWITCH}sbr-simulator`,
      ]),
    ).toEqual({
      kind: "sbr-simulator",
      userDataPath: "/private/tmp/tammy-sbr-simulator-fixed",
    });
  });

  it("rejects missing, duplicated, unknown, or mismatched authority", () => {
    const invalid = [
      ["Tammy", "--user-data-dir=/private/tmp/tammy-sbr-simulator-fixed"],
      ["Tammy", `${TAMMY_LAUNCH_SCENARIO_SWITCH}sbr-simulator`],
      [
        "Tammy",
        "--user-data-dir=/private/tmp/tammy-sbr-simulator-fixed",
        `${TAMMY_LAUNCH_SCENARIO_SWITCH}accounting-fresh`,
      ],
      ["Tammy", `${TAMMY_LAUNCH_SCENARIO_SWITCH}unknown`],
      [
        "Tammy",
        "--user-data-dir=/private/tmp/tammy-sbr-simulator-fixed",
        `${TAMMY_LAUNCH_SCENARIO_SWITCH}sbr-simulator`,
        `${TAMMY_LAUNCH_SCENARIO_SWITCH}sbr-simulator`,
      ],
    ];
    for (const arguments_ of invalid) {
      expect(() => parseDesktopLaunchScenario(arguments_)).toThrow("LOCAL_SCENARIO_INVALID");
    }
  });
});
