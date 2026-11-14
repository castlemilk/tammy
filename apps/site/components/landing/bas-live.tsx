"use client";

/*
 * The GST workpaper's basis switch and stale-source demonstration are native
 * inputs that bas.css reads with :has(), so they work with no script at all.
 * This island adds what CSS cannot: it tells assistive technology what
 * changed, and it hands focus back when "Regenerate local draft" leaves the
 * page. It also sets two attributes for bas.css, so the figures arrive with a
 * small movement only when the visitor has replaced them:
 *   data-touched        the basis has been switched at least once
 *   data-regen="a|b"    the draft was just regenerated (alternates to replay)
 */
import { useEffect, useRef, useState } from "react";

export function BasLive({
  nonCash,
  cash,
  stale,
  fresh,
}: {
  nonCash: string;
  cash: string;
  stale: string;
  fresh: string;
}) {
  const ref = useRef<HTMLParagraphElement>(null);
  const [message, setMessage] = useState("");

  useEffect(() => {
    const section = ref.current?.closest<HTMLElement>(".lp-bas");
    if (!section) return;
    let timer = 0;

    // Below 1280px the handles sit above the sheet, so bring the figure or source
    // that just changed into view. "nearest" moves only when it is out of view.
    const reveal = (selector: string) => {
      if (typeof window.matchMedia !== "function" || !window.matchMedia("(max-width: 1279px)").matches) return;
      const target = Array.from(section.querySelectorAll<HTMLElement>(selector)).find((el) => el.offsetParent !== null);
      if (target && typeof target.scrollIntoView === "function") target.scrollIntoView({ block: "nearest" });
    };

    const onChange = (event: Event) => {
      const input = event.target;
      if (!(input instanceof HTMLInputElement)) return;
      if (input.name === "bas-basis") {
        section.dataset.touched = "";
        setMessage(input.value === "cash" ? cash : nonCash);
        reveal(`.lp-bas-figures[data-basis="${input.value}"]`);
      } else if (input.id === "bas-stale") {
        setMessage(input.checked ? stale : fresh);
        if (input.checked) reveal(".lp-bas-stale");
      }
    };

    // Tapping a figure lights its source row, which can sit well below it on a phone.
    const onClick = (event: MouseEvent) => {
      const figure = event.target instanceof Element ? event.target.closest<HTMLElement>(".lp-bas-sheet .lp-trace[data-key]") : null;
      const key = figure?.dataset.key;
      if (key) reveal(`.lp-bas-sheet .lp-trace-target[data-trace~="${key}"]`);
    };

    // The reset button is hidden once the draft is fresh again, so focus would
    // fall to the document. Return it to the control that started this.
    const onReset = () => {
      section.dataset.regen = section.dataset.regen === "a" ? "b" : "a";
      setMessage(fresh);
      window.clearTimeout(timer);
      timer = window.setTimeout(() => section.querySelector<HTMLInputElement>("#bas-stale")?.focus(), 0);
    };

    section.addEventListener("change", onChange);
    section.addEventListener("reset", onReset);
    section.addEventListener("click", onClick);
    return () => {
      window.clearTimeout(timer);
      section.removeEventListener("change", onChange);
      section.removeEventListener("reset", onReset);
      section.removeEventListener("click", onClick);
    };
  }, [nonCash, cash, stale, fresh]);

  return (
    <p ref={ref} className="lp-visually-hidden" aria-live="polite">
      {message}
    </p>
  );
}
