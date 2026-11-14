"use client";

/*
 * The Overview scene's one unprompted trace. It sets data-traced on the scene
 * the first time the "Invoices owing" figure and the first receivable line it
 * reads are on screen together; features.css then strikes the trace once.
 * No script, jsdom and reduced motion leave the scene as the server rendered it.
 */
import { useEffect } from "react";

export function FeaturesTrace() {
  useEffect(() => {
    const scene = document.querySelector<HTMLElement>(".lp-features-ov");
    if (!scene || typeof IntersectionObserver === "undefined" || typeof window.matchMedia !== "function") return;
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const parts = [
      scene.querySelector('.lp-trace[data-trace="owing"]'),
      scene.querySelector('.lp-trace-target.lp-amount[data-trace="owing"]'),
    ].filter((part): part is Element => part !== null);
    if (parts.length === 0) return;
    const whole = new Set<Element>();
    let frame = 0;
    // After a jump the scene arrives with these parts still fading in: strike once both are fully shown.
    const arriving = parts
      .map((part) => part.closest<HTMLElement>("[data-enter]"))
      .filter((part): part is HTMLElement => part !== null);
    const strike = () => {
      if (arriving.every((part) => getComputedStyle(part).opacity === "1")) scene.dataset.traced = "";
      else frame = window.requestAnimationFrame(strike);
    };
    const io = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.intersectionRatio >= 0.98) whole.add(entry.target);
          else whole.delete(entry.target);
        }
        if (whole.size === parts.length) {
          io.disconnect();
          strike();
        }
      },
      { rootMargin: "-64px 0px 0px 0px", threshold: [0, 0.98] },
    );
    for (const part of parts) io.observe(part);
    return () => {
      io.disconnect();
      window.cancelAnimationFrame(frame);
    };
  }, []);

  return null;
}
