"use client";

/*
 * The rail's inner track. On a phone the rail is a horizontally scrolling row
 * of chips and the default screen (Trial balance) sits seventh, off to the
 * right; this island keeps the checked chip in view. It only moves the chip
 * row's own scroll position, never the page, and does nothing where the rail
 * does not scroll (desktop, jsdom) or before hydration.
 */
import { type ReactNode, useEffect, useRef } from "react";

export function HeroRailTrack({ children }: { children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const track = ref.current;
    if (!track) return;

    const reveal = (smooth: boolean) => {
      if (track.scrollWidth <= track.clientWidth + 1) return;
      const chip = track.querySelector<HTMLInputElement>("input:checked")?.closest<HTMLElement>("label");
      if (!chip) return;
      const left = Math.max(0, chip.offsetLeft - (track.clientWidth - chip.offsetWidth) / 2);
      const calm =
        !smooth ||
        typeof window.matchMedia !== "function" ||
        window.matchMedia("(prefers-reduced-motion: reduce)").matches;
      if (typeof track.scrollTo === "function") track.scrollTo({ left, behavior: calm ? "auto" : "smooth" });
      else track.scrollLeft = left;
    };

    reveal(false);
    const onChange = () => reveal(true);
    track.addEventListener("change", onChange);
    return () => track.removeEventListener("change", onChange);
  }, []);

  return (
    <div ref={ref} className="lp-hero-rail-track">
      {children}
    </div>
  );
}
