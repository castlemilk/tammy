"use client";

/*
 * The two-leaf mark, drawn large on the forest ground. It is the same pair of
 * leaves as the header's wordmark (components/site-brand.tsx), scaled by one
 * unit, --lp-closing-u.
 *
 * The flourish: when the mark starts below the fold, the leaves wait nearly
 * shut, each turned a few degrees in place, and open once to their resting
 * angles as the band comes into view. The server, reduced motion, jsdom and a
 * mark already on screen at load all get the open, resting mark. The island
 * only reports the phase; the choreography is CSS in closing.css.
 */
import { useScenePhase } from "./motion";

export function ClosingMark() {
  const [ref, phase] = useScenePhase<HTMLSpanElement>("0px 0px -32% 0px");

  return (
    <span ref={ref} className="lp-closing-mark" data-phase={phase} aria-hidden="true">
      <span className="lp-closing-leaf" />
      <span className="lp-closing-leaf" />
    </span>
  );
}
