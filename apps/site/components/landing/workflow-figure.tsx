"use client";

/*
 * One step's figure in "How it works". The server renders the finished state.
 * The choreography (before, the button pressed, the result) is CSS keyed off a
 * single attribute, data-play, and this island only ever sets that attribute:
 *
 *   (absent)  finished state: server, no script, reduced motion, jsdom
 *   "armed"   the "before" state, set only while the figure cannot be seen
 *   "run"     the demonstration plays once and holds its finished state
 *
 * A figure plays the first time it can be seen: when it scrolls into view in
 * the stacked layout, or when its caption becomes the active step in the
 * pinned layout. Replay, or a tap on the app button the step presses, plays it
 * again.
 */
import { type ReactNode, useEffect, useRef } from "react";
import { Icon } from "./kit";

/** Longer than the longest choreography in workflow-scenes.tsx. */
const FINISHED_AFTER_MS = 5200;

/** How much of a figure must be on screen before its step plays. */
const SEEN_RATIO = 0.55;
/** In the pinned layout the window must be nearly whole, so the button a step presses is on screen. */
const PINNED_SEEN_RATIO = 0.9;
/** On a short viewport a tall figure can never show 55% of itself, so filling 60% of the viewport's height, with at least 55% of its own width showing, also counts as seen. */
const SEEN_FILL = 0.6;
const SEEN_STEPS = [0, 0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.35, 0.4, 0.45, 0.5, SEEN_RATIO, 0.92];

export function WorkflowFigure({
  step,
  name,
  label,
  children,
}: {
  step: number;
  /** The step's short name, for the Replay button. */
  name: string;
  /** What the finished figure shows, for assistive technology. */
  label: string;
  children: ReactNode;
}) {
  const ref = useRef<HTMLElement>(null);

  useEffect(() => {
    const node = ref.current;
    if (!node || typeof window.matchMedia !== "function") return;
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;

    let finishedAt = 0;
    const play = () => {
      node.dataset.play = "armed";
      // Reading layout between the two writes restarts every CSS animation.
      void node.offsetWidth;
      node.dataset.play = "run";
      finishedAt = performance.now() + FINISHED_AFTER_MS;
    };
    // The finished state is the default state, so a figure that has finished
    // playing goes back to plain markup once it is out of sight.
    const rest = () => {
      if (node.dataset.play === "run" && performance.now() > finishedAt) node.removeAttribute("data-play");
    };

    const onClick = (event: MouseEvent) => {
      if (event.target instanceof Element && event.target.closest("[data-replay], [data-press]")) play();
    };
    node.addEventListener("click", onClick);
    // Replay needs this script, so the control is only shown once it is live.
    node.dataset.live = "";

    let viewport: IntersectionObserver | undefined;
    let activity: MutationObserver | undefined;
    if (typeof IntersectionObserver !== "undefined") {
      const caption = node.previousElementSibling;
      let seen = false;
      let first = true;
      // In the pinned layout every figure shares one sticky cell, so being in
      // view is not enough: the figure's own caption must be the active step.
      const pinned = () => window.getComputedStyle(node).position === "sticky";
      const waitingForStep = () => pinned() && !caption?.hasAttribute("data-active");
      const sync = () => {
        if (node.dataset.play === "armed" && seen && !waitingForStep()) play();
      };

      viewport = new IntersectionObserver(
        (entries) => {
          for (const entry of entries) {
            // A sliver of the next shelf card is tall but not wide: it must show most of its width as well.
            const fills =
              entry.intersectionRect.width >= entry.boundingClientRect.width * SEEN_RATIO &&
              entry.intersectionRect.height >= (entry.rootBounds?.height ?? Number.POSITIVE_INFINITY) * SEEN_FILL;
            seen =
              entry.isIntersecting &&
              (pinned() ? entry.intersectionRatio >= PINNED_SEEN_RATIO : entry.intersectionRatio >= SEEN_RATIO || fills);
            if (first) {
              first = false;
              // Only arm what cannot be seen yet, so nothing on screen blinks.
              if (!entry.isIntersecting || waitingForStep()) node.dataset.play = "armed";
            }
            if (!entry.isIntersecting) rest();
          }
          sync();
        },
        { rootMargin: "0px 0px -8% 0px", threshold: SEEN_STEPS },
      );
      viewport.observe(node);

      if (caption && typeof MutationObserver !== "undefined") {
        activity = new MutationObserver(sync);
        activity.observe(caption, { attributes: true, attributeFilter: ["data-active"] });
      }
    }

    return () => {
      node.removeEventListener("click", onClick);
      viewport?.disconnect();
      activity?.disconnect();
      node.removeAttribute("data-play");
      node.removeAttribute("data-live");
    };
  }, []);

  return (
    <figure ref={ref} className="lp-workflow-figure" data-fig={step} aria-label={label}>
      {children}
      <div className="lp-workflow-foot">
        <button type="button" className="lp-replay" data-replay="" aria-label={`Replay step ${step}, ${name}`}>
          <Icon name="replay" />
          Replay
        </button>
      </div>
    </figure>
  );
}
