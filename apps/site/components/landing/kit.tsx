/*
 * The shared landing vocabulary as server-safe React pieces. Styles are in
 * app/landing.css. Nothing here is a client component, so these can be used
 * inside server sections and inside client islands alike.
 */
import type { ReactNode } from "react";
import { aud } from "../../lib/tools/calculations";

/** Joins class names. Landing classes are never Tailwind utilities, so nothing needs merging. */
export function cx(...parts: (string | undefined)[]): string {
  return parts.filter(Boolean).join(" ");
}

/** Diagonal arrow for primary actions. Decorative: the link owns its name. */
export function Arrow({ className }: { className?: string }) {
  return (
    <svg className={cx("lp-arrow", className)} viewBox="0 0 20 20" fill="none" aria-hidden="true" focusable="false">
      <path d="M5 15 15 5M6.5 5H15v8.5" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

/** The check a person leaves. Give it data-enter="draw" inside a scene to draw it. */
export function Tick({ className, label, ...rest }: { className?: string; label?: string } & Record<`data-${string}`, string | undefined>) {
  return (
    <svg
      className={cx("lp-tick", className)}
      viewBox="0 0 24 24"
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
      focusable="false"
      {...rest}
    >
      <path d="M4.2 13.4c2.1 1.5 3.5 3.1 4.9 5.2 2.6-5.4 5.9-9.6 10.9-13.4" pathLength={1} />
    </svg>
  );
}

export type PillTone = "plain" | "ok" | "warn" | "deep";

/** Record state label. tone: ok = sage/forest, warn = amber, plain = paper. */
export function Pill({
  tone = "plain",
  dot = false,
  tick = false,
  className,
  children,
  ...rest
}: {
  tone?: PillTone;
  dot?: boolean;
  tick?: boolean;
  className?: string;
  children: ReactNode;
} & Record<`data-${string}`, string | undefined>) {
  return (
    <span className={cx("lp-pill", className)} data-tone={tone} {...rest}>
      {dot ? <i className="lp-pill-dot" aria-hidden="true" /> : null}
      {tick ? <Tick /> : null}
      {children}
    </span>
  );
}

/** Whole-cent AUD amount in tabular figures. It arrives whole: never tween it. */
export function Money({ cents, className }: { cents: number; className?: string }) {
  return <span className={cx("lp-num", className)}>{aud(cents)}</span>;
}

/** Marginalia: a limit printed once, beside the claim it qualifies. */
export function Note({ children, className }: { children: ReactNode; className?: string }) {
  return <p className={cx("lp-note", className)}>{children}</p>;
}

/**
 * The illustrative application window, drawn in the desktop app's own style:
 * a paper bar with the breadcrumb and the "Local data" lock. It carries its
 * own label, so a scene needs no further "not a screenshot" disclaimer. No
 * macOS traffic lights: this must never pass for a capture.
 */
export function AppWindow({
  title,
  className,
  children,
  label = "Illustrative workspace · synthetic figures",
  local = "Local data",
  bar,
}: {
  title: ReactNode;
  className?: string;
  children: ReactNode;
  label?: string | null;
  local?: string | null;
  /** Extra controls for the right of the bar, such as a lock toggle. */
  bar?: ReactNode;
}) {
  return (
    <div className={cx("lp-window", className)}>
      <div className="lp-window-bar">
        <span className="lp-window-title">{title}</span>
        {label ? <span className="lp-window-label">{label}</span> : null}
        {local ? (
          <span className="lp-window-local">
            <Icon name="lock" />
            {local}
          </span>
        ) : null}
        {bar}
      </div>
      {children}
    </div>
  );
}

/** The standing caption for illustrative scenes. */
export function FigNote({ children, className }: { children?: ReactNode; className?: string }) {
  return (
    <figcaption className={cx("lp-fig-note", className)}>
      {children ?? "Illustrative interface. Synthetic examples, not a live account or actual application capture."}
    </figcaption>
  );
}

const iconPaths = {
  lock: "M7 11V8a5 5 0 0 1 10 0v3M6 11h12v9H6z",
  check: "m5 12.5 4.5 4.5L19 7.5",
  overview: "M4 5h7v7H4zM13 5h7v4h-7zM13 11h7v8h-7zM4 14h7v5H4z",
  document: "M7 3h7l4 4v14H7zM14 3v4h4M10 12h5M10 16h5",
  invoice: "M6 3h12v18l-3-2-3 2-3-2-3 2zM9 8h6M9 12h6",
  bank: "M4 10h16M6 10v8M10 10v8M14 10v8M18 10v8M3 20h18M12 4l8 5H4z",
  chart: "M5 5h14M5 10h9M5 15h14M5 20h6",
  journal: "M6 4h12v16H6zM6 9h12M6 14h12M12 9v11",
  balance: "M12 4v16M7 20h10M5 8h14M5 8l-2 6h4zM19 8l-2 6h4z",
  gst: "M6 3h12v18H6zM9 7h6M9 12h1.5M13.5 12H15M9 16h1.5M13.5 16H15",
  audit: "M5 6h14M5 12h14M5 18h9M18 16l1.5 1.5L22 15",
  upload: "M12 16V5M7.5 9.5 12 5l4.5 4.5M5 19h14",
  download: "M12 5v11M7.5 11.5 12 16l4.5-4.5M5 19h14",
  plus: "M12 5v14M5 12h14",
  replay: "M4 12a8 8 0 1 0 2.6-5.9M4 4v5h5",
  mac: "M4 5h16v11H4zM9 20h6M12 16v4",
  eye: "M2.5 12s3.5-6.5 9.5-6.5 9.5 6.5 9.5 6.5-3.5 6.5-9.5 6.5S2.5 12 2.5 12zM12 9.5a2.5 2.5 0 1 0 0 5 2.5 2.5 0 0 0 0-5z",
  calendar: "M5 6h14v14H5zM5 10h14M9 3v5M15 3v5",
} as const;

export type IconName = keyof typeof iconPaths;

/** 24px-grid line icons at the app's 1.8px stroke. Decorative by default. */
export function Icon({ name, className, label }: { name: IconName; className?: string; label?: string }) {
  return (
    <svg
      className={cx("lp-icon", className)}
      viewBox="0 0 24 24"
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
      focusable="false"
    >
      <path d={iconPaths[name]} />
    </svg>
  );
}
