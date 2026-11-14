"use client";

/*
 * The gate island for the Local API and MCP band.
 *
 * It owns one piece of state and writes it to data-state on the figure:
 *   off → enabling → reading → staged → saved
 * The choreography (gate opening, transcript lines arriving, the editor
 * filling) is CSS keyed off that attribute; the two automatic steps are driven
 * by the animations ending, not by a chain of timers.
 *
 * The server HTML, no-script, reduced-motion and jsdom state is "staged": the
 * whole transcript and the filled editor, badge "Not saved". Only when the band
 * starts below the fold, with script and motion allowed, does it start "off"
 * and wait for a press. It never plays by itself: access is off until a person
 * enables it.
 */
import { type AnimationEvent, type CSSProperties, type ReactNode, useEffect, useRef, useState } from "react";
import { AppWindow, FigNote, Icon, Pill } from "./kit";
import { useScenePhase } from "./motion";

type GateState = "off" | "enabling" | "reading" | "staged" | "saved";
/** Why access is off: never enabled, revoked mid-read, revoked with a draft staged, or revoked after saving. */
type OffKind = "fresh" | "cut" | "discarded" | "kept";
type FocusTarget = "enable" | "revoke" | "replay" | "notice";

const TERMS =
  "Clients on this computer with the private connection file can read your current organisation's accounting data and stage editable invoice proposals. You must review and save proposals in Tammy.";

const READOUT: Record<Exclude<GateState, "off"> | `off-${OffKind}`, { title: string; body: string }> = {
  "off-fresh": {
    title: "Access is off.",
    body: "Nothing on this Mac can read these books through the local API until you enable it in Tammy.",
  },
  "off-cut": {
    title: "Access is off.",
    body: "You revoked it before anything was staged. The client can read nothing more.",
  },
  "off-discarded": {
    title: "Access is off.",
    body: "You revoked it. The staged proposal was only held in memory, so it is gone. Nothing was saved.",
  },
  "off-kept": {
    title: "Access is off again.",
    body: "You revoked it. The draft you saved stays in Tammy, and the client can read nothing.",
  },
  enabling: {
    title: "Access is on.",
    body: "You pressed Enable access. Your client reads through the gate, then stages one proposal.",
  },
  reading: {
    title: "Access is on.",
    body: "You pressed Enable access. Your client reads through the gate, then stages one proposal.",
  },
  staged: {
    title: "A draft is staged.",
    body: "You chose Use MCP draft and the editor filled. Nothing is saved: that press is yours.",
  },
  saved: {
    title: "Saved, by you.",
    body: "The client proposed and you pressed Save draft. It never had that button.",
  },
};

/** When each mark crosses the gate, after reading starts: out with a call, back with its answer. */
const CROSSINGS = [
  { dir: "out", at: 100 },
  { dir: "in", at: 460 },
  { dir: "out", at: 1340 },
  { dir: "in", at: 1700 },
] as const;

const part = (index: number) => ({ "--p": index }) as CSSProperties;

function motionAllowed(): boolean {
  return typeof window.matchMedia === "function" && !window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

function NoticeIcon({ done }: { done: boolean }) {
  return (
    <svg className="lp-icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d={done ? "M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM8 12.4l2.8 2.8L16 9.6" : "M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM12 7.8v5.4M12 16.4h.01"} />
    </svg>
  );
}

export function IntegrationsGate({
  windowTitle,
  invoiceNumber,
  request,
  exchange,
  list,
  listKept,
  editor,
}: {
  windowTitle: string;
  invoiceNumber: string;
  /** The visitor's request line: the only line a client shows while access is off. */
  request: ReactNode;
  /** The rest of the transcript. */
  exchange: ReactNode;
  /** The Invoices list, and the same list holding the draft the visitor saved. */
  list: ReactNode;
  listKept: ReactNode;
  /** The editor's fields, line and totals. */
  editor: ReactNode;
}) {
  const [sceneRef, phase] = useScenePhase<HTMLElement>();
  const [chosen, setChosen] = useState<GateState | null>(null);
  const [offKind, setOffKind] = useState<OffKind>("fresh");
  const state: GateState = chosen ?? (phase === "static" ? "staged" : "off");

  const enableRef = useRef<HTMLButtonElement>(null);
  const revokeRef = useRef<HTMLButtonElement>(null);
  const replayRef = useRef<HTMLButtonElement>(null);
  const noticeRef = useRef<HTMLParagraphElement>(null);
  const focusNext = useRef<FocusTarget | null>(null);
  const safety = useRef<number | undefined>(undefined);

  // Keep focus on the control that replaced the one just pressed.
  useEffect(() => {
    const target = focusNext.current;
    if (!target) return;
    focusNext.current = null;
    const node =
      target === "enable"
        ? enableRef.current
        : target === "revoke"
          ? revokeRef.current
          : target === "replay"
            ? replayRef.current
            : noticeRef.current;
    node?.focus({ preventScroll: target !== "notice" });
  }, [state, offKind]);

  useEffect(() => () => window.clearTimeout(safety.current), []);

  const enable = () => {
    focusNext.current = "revoke";
    setOffKind("fresh");
    window.clearTimeout(safety.current);
    if (!motionAllowed()) {
      setChosen("staged");
      return;
    }
    setChosen("enabling");
    // The animations ending move the scene on. This only catches an engine that never reports them.
    safety.current = window.setTimeout(() => {
      setChosen((current) => (current === "enabling" || current === "reading" ? "staged" : current));
    }, 6000);
  };

  const save = () => {
    focusNext.current = "notice";
    setChosen("saved");
  };

  const revoke = () => {
    window.clearTimeout(safety.current);
    const keep = state === "saved";
    focusNext.current = keep ? "replay" : "enable";
    setOffKind(keep ? "kept" : state === "staged" ? "discarded" : "cut");
    setChosen("off");
  };

  const replay = () => {
    focusNext.current = "enable";
    setOffKind("fresh");
  };

  const onAnimationEnd = (event: AnimationEvent<HTMLElement>) => {
    if (event.animationName === "lp-integrations-gate-open") {
      setChosen((current) => (current === "enabling" ? "reading" : current));
    } else if (event.animationName === "lp-integrations-arrive-last") {
      setChosen((current) => (current === "reading" ? "staged" : current));
    }
  };

  const off = state === "off";
  const saved = state === "saved";
  const kept = off && offKind === "kept";
  const readout = state === "off" ? READOUT[`off-${offKind}` as const] : READOUT[state];

  return (
    <figure
      ref={sceneRef}
      className="lp-integrations-scene"
      data-state={state}
      data-off={off ? offKind : undefined}
      aria-label="Local API and MCP access, demonstrated with synthetic figures"
      onAnimationEnd={onAnimationEnd}
    >
      {/* The caption says what just happened and who pressed what. It is the scene's live region. */}
      <output className="lp-integrations-readout">
        <span className="lp-integrations-readout-title">{readout.title}</span>
        <span className="lp-small">{readout.body}</span>
      </output>

      <div className="lp-integrations-rule" aria-hidden="true" />

      <div className="lp-integrations-access">
        <p className="lp-label">
          <span className="lp-integrations-owner">Tammy · </span>Integrations → {off ? "Enable local API & MCP…" : "Revoke local API & MCP access"}
        </p>
        <div className="lp-integrations-access-body">
          <div>
            <p className="lp-integrations-access-title">
              {kept ? "Local API & MCP access is off" : off ? "Enable local API & MCP access?" : "Local API & MCP enabled"}
            </p>
            {/* The terms keep their place when they no longer apply, so revoking moves nothing. */}
            <div className="lp-integrations-terms">
              <p className="lp-small" data-spent={kept ? "" : undefined}>
                {TERMS}
              </p>
              {kept ? (
                <p className="lp-small">The private connection file is removed. Clients on this computer can no longer read these books.</p>
              ) : null}
            </div>
          </div>
          <div className="lp-integrations-access-action lp-integrations-scripted">
            {off && !kept ? (
              <button ref={enableRef} type="button" className="lp-btn lp-btn--light" onClick={enable}>
                Enable access
              </button>
            ) : null}
            {kept ? (
              <button ref={replayRef} type="button" className="lp-replay" onClick={replay}>
                <Icon name="replay" />
                Replay
              </button>
            ) : null}
            {off ? null : (
              <button ref={revokeRef} type="button" className="lp-btn lp-btn--ghost lp-btn--sm" onClick={revoke}>
                Revoke access
              </button>
            )}
          </div>
        </div>
      </div>

      <div className="lp-integrations-client">
        <p className="lp-integrations-client-bar">
          <strong>Your MCP client</strong>
          <span>On this Mac, not part of Tammy</span>
        </p>
        <ol className="lp-integrations-log">
          {request}
          {off ? (
            <li className="lp-integrations-idle" data-who="none">
              Access is off.
            </li>
          ) : null}
          {exchange}
        </ol>
      </div>

      <div className="lp-integrations-gateway" aria-hidden="true">
        <span className="lp-integrations-seg" />
        <span className="lp-integrations-gate">
          <svg viewBox="0 0 88 72" focusable="false">
            <path className="lp-line" d="M44 0V12M44 60V72M33 12H55M33 60H55" />
            <path className="lp-line lp-integrations-leaf" d="M44 12V60" />
          </svg>
          {CROSSINGS.map((crossing) => (
            <i key={crossing.at} className="lp-integrations-dash" data-dir={crossing.dir} style={{ "--at": `${crossing.at}ms` } as CSSProperties} />
          ))}
        </span>
        <span className="lp-integrations-gate-word">{off ? "Shut" : "Open"}</span>
        <span className="lp-integrations-seg" />
      </div>

      <AppWindow className="lp-integrations-window" title={windowTitle}>
        <div className="lp-integrations-views">
          <div className="lp-integrations-list">{kept ? listKept : list}</div>
          <div className="lp-integrations-editor">
            <div className="lp-integrations-edhead" data-part="" style={part(0)}>
              <p className="lp-app-title">{invoiceNumber}</p>
              {saved ? (
                <Pill key="saved" tone="ok" className="lp-integrations-badge">
                  Saved draft
                </Pill>
              ) : (
                <Pill key="unsaved" tone="warn">
                  Not saved
                </Pill>
              )}
              <p className="lp-integrations-sub">Review the details and save your draft before issuing.</p>
            </div>
            <p ref={noticeRef} tabIndex={-1} className="lp-integrations-notice" data-tone={saved ? "ok" : "warn"} data-part="" style={part(1)}>
              <NoticeIcon done={saved} />
              {saved ? "Draft saved." : "Review this draft before saving."}
            </p>
            {editor}
            <div className="lp-integrations-foot" data-part="last" style={part(5)}>
              <button
                type="button"
                className="lp-btn lp-btn--solid lp-btn--sm lp-integrations-save lp-integrations-scripted"
                onClick={save}
                disabled={state !== "staged"}
              >
                Save draft
              </button>
              {/* Without script the button above is not shown; the control is still drawn, inert. */}
              <span className="lp-integrations-chip lp-integrations-unscripted" aria-hidden="true">
                Save draft
              </span>
              <span className="lp-integrations-chip" aria-hidden="true">
                Issue invoice
              </span>
              <p className="lp-integrations-footnote">Issuing is a further, separate press, and a person makes it.</p>
            </div>
          </div>
        </div>
      </AppWindow>

      <FigNote className="lp-integrations-fignote">
        Illustrative exchange, synthetic figures. A real client words its replies its own way; the tool names are the real ones.
      </FigNote>
    </figure>
  );
}
