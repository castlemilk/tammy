"use client";

import { useState } from "react";

export interface Scenario {
  readonly label: string;
  readonly context: string;
  readonly rows: readonly (readonly [string, string])[];
  readonly conclusion: string;
}

export function ScenarioExplorer({ title, scenarios }: {
  readonly title: string;
  readonly scenarios: readonly Scenario[];
}) {
  const [selected, setSelected] = useState(0);
  if (scenarios.length < 2 || scenarios.length > 4 || scenarios.some((s) => !s.label || !s.rows.length)) {
    throw new Error("ScenarioExplorer needs 2–4 scenarios with named rows");
  }
  const active = scenarios[selected] ?? scenarios[0];

  return <section className="tammy-mdx-scenarios" aria-label={title}>
    <div className="tammy-mdx-scenarios-heading"><h3>{title}</h3><span>Illustrative comparison</span></div>
    <fieldset className="tammy-mdx-scenario-options">
      <legend>Choose the scenario</legend>
      {scenarios.map((scenario, index) => <button type="button"
        key={scenario.label} aria-pressed={index === selected}
        onClick={() => setSelected(index)}>{scenario.label}</button>)}
    </fieldset>
    <div className="tammy-mdx-scenario-data" aria-live="polite" aria-atomic="true">
      <p>{active.context}</p>
      <dl>{active.rows.map(([label, value]) => <div key={label}>
        <dt>{label}</dt><dd>{value}</dd>
      </div>)}</dl>
      <p className="tammy-mdx-scenario-conclusion">{active.conclusion}</p>
    </div>
    <small>No records are changed or saved. The example is not a BAS calculation or a tax filing.</small>
  </section>;
}
