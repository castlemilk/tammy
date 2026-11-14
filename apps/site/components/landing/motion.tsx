"use client";

/*
 * Motion primitives for the landing page. The contract:
 *   - Server HTML is the complete, final, visible page.
 *   - Script only ever *adds* a hidden-before-reveal state ("armed"), and only
 *     for things that start below the fold, so nothing on screen blinks.
 *   - Every browser API is feature-detected: jsdom, older engines and
 *     reduced-motion readers keep the static page.
 */
import { type ReactNode, type RefObject, useEffect, useRef, useState } from "react";

const REDUCED_QUERY = "(prefers-reduced-motion: reduce)";

function motionAllowed(): boolean {
  return typeof window !== "undefined" && typeof window.matchMedia === "function" && !window.matchMedia(REDUCED_QUERY).matches;
}

/**
 * Mount once at the end of the landing page. It drives two things:
 *   1. [data-scene] containers: "armed" if they start below the fold, then "in"
 *      the first time they enter view. Scenes already on screen are left alone.
 *   2. .lp-header[data-scrolled] once the page has moved, and
 *      .lp-header[data-room] from the section currently under the header.
 */
export function LandingMotion() {
  useEffect(() => {
    const root = document.querySelector<HTMLElement>(".landing");
    if (!root) return;

    const header = root.querySelector<HTMLElement>(".lp-header");
    let frame = 0;
    const syncHeader = () => {
      frame = 0;
      header?.toggleAttribute("data-scrolled", window.scrollY > 8);
    };
    const onScroll = () => {
      if (!frame) frame = window.requestAnimationFrame(syncHeader);
    };
    syncHeader();
    window.addEventListener("scroll", onScroll, { passive: true });

    let observer: IntersectionObserver | undefined;
    if (typeof IntersectionObserver !== "undefined" && motionAllowed()) {
      const io = new IntersectionObserver(
        (entries) => {
          for (const entry of entries) {
            const scene = entry.target as HTMLElement;
            if (entry.isIntersecting) {
              if (scene.dataset.scene === "armed") scene.dataset.scene = "in";
              else if (!scene.dataset.scene) scene.dataset.scene = "seen";
              io.unobserve(scene);
            } else if (!scene.dataset.scene) {
              // The root margin leaves out the bottom strip of the viewport. A scene
              // showing there at load is already on screen: never hide it.
              const rect = entry.boundingClientRect;
              if (rect.bottom > 0 && rect.top < window.innerHeight) {
                scene.dataset.scene = "seen";
                io.unobserve(scene);
              } else {
                scene.dataset.scene = "armed";
              }
            }
          }
        },
        { rootMargin: "0px 0px -14% 0px", threshold: 0 },
      );
      root.querySelectorAll<HTMLElement>("[data-scene]").forEach((scene) => io.observe(scene));
      observer = io;
    }

    // The header tag names where the software on screen runs. A section opts in
    // with data-room="mac" | "browser" | "reading"; the one under the header wins.
    let rooms: IntersectionObserver | undefined;
    if (header && typeof IntersectionObserver !== "undefined") {
      const io = new IntersectionObserver(
        (entries) => {
          for (const entry of entries) {
            const room = (entry.target as HTMLElement).dataset.room;
            if (entry.isIntersecting && room) header.dataset.room = room;
          }
        },
        { rootMargin: "-72px 0px -85% 0px", threshold: 0 },
      );
      root.querySelectorAll<HTMLElement>("[data-room]").forEach((section) => io.observe(section));
      rooms = io;
    }

    return () => {
      window.removeEventListener("scroll", onScroll);
      if (frame) window.cancelAnimationFrame(frame);
      observer?.disconnect();
      rooms?.disconnect();
    };
  }, []);

  return null;
}

export type ScenePhase = "static" | "armed" | "in";

/**
 * For islands that run a scripted, play-once sequence.
 *   "static"  server, no script, jsdom, reduced motion, or already on screen at
 *             load: render the finished state.
 *   "armed"   started below the fold: render the starting state.
 *   "in"      has entered view: play.
 */
export function useScenePhase<T extends Element>(margin = "0px 0px -18% 0px"): [RefObject<T | null>, ScenePhase] {
  const ref = useRef<T>(null);
  const [phase, setPhase] = useState<ScenePhase>("static");

  useEffect(() => {
    const node = ref.current;
    if (!node || typeof IntersectionObserver === "undefined" || !motionAllowed()) return;
    let first = true;
    const io = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          const rect = entry.boundingClientRect;
          // On screen at all, including the strip the root margin leaves out.
          const showing = entry.isIntersecting || (rect.bottom > 0 && rect.top < window.innerHeight);
          if (first && showing) {
            io.disconnect();
            return;
          }
          if (first) setPhase("armed");
          else if (entry.isIntersecting) {
            setPhase("in");
            io.disconnect();
          }
          first = false;
        }
      },
      { rootMargin: margin, threshold: 0 },
    );
    io.observe(node);
    return () => io.disconnect();
  }, [margin]);

  return [ref, phase];
}

/**
 * Scroll-told sequences. Wrap the steps; each step carries data-step="n".
 * The step crossing the middle of the viewport becomes active:
 *   wrapper  data-steps="on"  data-active-step="n"
 *   step     data-active
 * Until script runs there is no data-steps attribute, so author the stacked,
 * everything-visible layout as the default and the pinned layout under
 * [data-steps="on"].
 */
export function ScrollSteps({ className, children }: { className?: string; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const root = ref.current;
    if (!root || typeof IntersectionObserver === "undefined") return;
    const steps = Array.from(root.querySelectorAll<HTMLElement>("[data-step]"));
    if (steps.length === 0) return;

    const activate = (step: HTMLElement) => {
      root.dataset.activeStep = step.dataset.step ?? "0";
      for (const other of steps) other.toggleAttribute("data-active", other === step);
    };
    // The active step is the last one whose top edge has reached the middle of
    // the viewport. Read from geometry, so it is right after a jump, a reload
    // with a restored scroll position, or arriving from below.
    const sync = () => {
      const middle = window.innerHeight / 2;
      let current = steps[0];
      for (const step of steps) {
        if (step.getBoundingClientRect().top <= middle) current = step;
      }
      if (current) activate(current);
    };
    // The root reaches far above the viewport, so a step "intersects" exactly
    // while its top edge is above the middle: any crossing reports, in any order.
    const io = new IntersectionObserver(sync, { rootMargin: "100000px 0px -50% 0px", threshold: 0 });
    root.dataset.steps = "on";
    sync();
    steps.forEach((step) => io.observe(step));

    return () => {
      io.disconnect();
      delete root.dataset.steps;
      delete root.dataset.activeStep;
      for (const step of steps) step.removeAttribute("data-active");
    };
  }, []);

  return (
    <div ref={ref} className={className}>
      {children}
    </div>
  );
}

/**
 * Text for a polite live region: empty until the visitor changes something,
 * then the latest text once it has been still for `ms`. Keeps a screen reader
 * from announcing every keystroke of an amount.
 */
export function useSettledText(text: string, active: boolean, ms = 800): string {
  const [said, setSaid] = useState("");

  useEffect(() => {
    if (!active) return;
    const id = window.setTimeout(() => setSaid(text), ms);
    return () => window.clearTimeout(id);
  }, [text, active, ms]);

  return said;
}
