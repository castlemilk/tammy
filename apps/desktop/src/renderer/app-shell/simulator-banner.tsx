import type { DesktopLaunchScenario } from "../../shared/launch-scenario";

export function SimulatorBanner({ scenario }: { readonly scenario: DesktopLaunchScenario }) {
  if (scenario !== "sbr-simulator" && scenario !== "sbr-doctor") return null;
  return (
    <div
      aria-live="polite"
      className="bg-warning px-4 py-2 text-center text-[11px] font-bold tracking-[0.08em] text-warning-foreground"
      role="status"
    >
      SIMULATOR — NOT FOR ATO LODGMENT
    </div>
  );
}
