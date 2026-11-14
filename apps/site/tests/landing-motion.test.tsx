import { act, cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ClosingMark } from "../components/landing/closing-mark";
import { LandingMotion, ScrollSteps } from "../components/landing/motion";

type Entry = { target: Element; isIntersecting: boolean; top?: number };

/** jsdom has no IntersectionObserver; this one lets a test deliver entries by hand. */
class FakeObserver {
  static all: FakeObserver[] = [];
  targets = new Set<Element>();
  constructor(private callback: (entries: unknown[], observer: FakeObserver) => void) {
    FakeObserver.all.push(this);
  }
  observe(target: Element) { this.targets.add(target); }
  unobserve(target: Element) { this.targets.delete(target); }
  disconnect() { this.targets.clear(); }
  /** `top` is the target's top edge in the viewport; jsdom's window is 768px tall. */
  deliver(entries: Entry[]) {
    const live = entries
      .filter((entry) => this.targets.has(entry.target))
      .map(({ target, isIntersecting, top = isIntersecting ? 100 : 5000 }) => ({ target, isIntersecting, boundingClientRect: { top, bottom: top + 200, height: 200 } }));
    act(() => this.callback(live, this));
  }
  static watching(target: Element) {
    const found = FakeObserver.all.find((observer) => observer.targets.has(target));
    if (!found) throw new Error("nothing observes this element");
    return found;
  }
}

beforeEach(() => {
  FakeObserver.all = [];
  vi.stubGlobal("IntersectionObserver", FakeObserver);
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

describe("landing motion primitives", () => {
  it("arms only scenes that start off screen, plays them once, and leaves on-screen scenes alone", () => {
    const { container } = render(
      <div className="landing">
        <div data-scene="" />
        <div data-scene="" />
        <div data-scene="" />
        <LandingMotion />
      </div>,
    );
    const [onScreen, atTheFold, below] = [...container.querySelectorAll<HTMLElement>("[data-scene]")] as [HTMLElement, HTMLElement, HTMLElement];
    const scenes = FakeObserver.watching(onScreen);
    scenes.deliver([
      { target: onScreen, isIntersecting: true },
      // In the bottom strip the root margin leaves out: showing, so never hidden.
      { target: atTheFold, isIntersecting: false, top: 700 },
      { target: below, isIntersecting: false },
    ]);
    expect(onScreen.dataset.scene).toBe("seen");
    expect(atTheFold.dataset.scene).toBe("seen");
    expect(below.dataset.scene).toBe("armed");
    scenes.deliver([{ target: below, isIntersecting: true }]);
    expect(below.dataset.scene).toBe("in");
    expect(scenes.targets.size).toBe(0);
  });

  it("reports static, then armed, then in, for a scripted scene that starts below the fold", () => {
    const { container } = render(<ClosingMark />);
    const mark = container.querySelector<HTMLElement>(".lp-closing-mark")!;
    expect(mark.dataset.phase).toBe("static");
    const observer = FakeObserver.watching(mark);
    observer.deliver([{ target: mark, isIntersecting: false }]);
    expect(mark.dataset.phase).toBe("armed");
    observer.deliver([{ target: mark, isIntersecting: true }]);
    expect(mark.dataset.phase).toBe("in");
  });

  it("keeps a scripted scene static when any of it is on screen at load", () => {
    for (const first of [{ isIntersecting: true }, { isIntersecting: false, top: 700 }]) {
      const { container, unmount } = render(<ClosingMark />);
      const mark = container.querySelector<HTMLElement>(".lp-closing-mark")!;
      FakeObserver.watching(mark).deliver([{ target: mark, ...first }]);
      expect(mark.dataset.phase).toBe("static");
      unmount();
    }
  });

  it("activates the last step whose top has reached mid-viewport, after any jump, and cleans up", () => {
    const tops: Record<string, number> = { "1": 100, "2": 900, "3": 1700 };
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
      return { top: tops[this.dataset.step ?? ""] ?? 0 } as DOMRect;
    });
    const { container, unmount } = render(
      <ScrollSteps className="steps">
        <div data-step="1" />
        <div data-step="2" />
        <div data-step="3" />
      </ScrollSteps>,
    );
    const root = container.querySelector<HTMLElement>(".steps")!;
    const steps = [...root.querySelectorAll<HTMLElement>("[data-step]")];
    expect(root.dataset.steps).toBe("on");
    expect(root.dataset.activeStep).toBe("1");
    // An instant jump past every step: only the last one reports a change.
    Object.assign(tops, { "1": -3000, "2": -2200, "3": -1400 });
    FakeObserver.watching(steps[2]!).deliver([{ target: steps[2]!, isIntersecting: true }]);
    expect(root.dataset.activeStep).toBe("3");
    expect(steps.map((step) => step.hasAttribute("data-active"))).toEqual([false, false, true]);
    // Back up to between steps 1 and 2.
    Object.assign(tops, { "1": -200, "2": 600, "3": 1400 });
    FakeObserver.watching(steps[1]!).deliver([{ target: steps[1]!, isIntersecting: false }]);
    expect(root.dataset.activeStep).toBe("1");
    unmount();
    expect(root.dataset.steps).toBeUndefined();
  });
});
