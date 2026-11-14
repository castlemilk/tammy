/*
 * The line: one contour meaning the edge of this Mac, with its three gates.
 * It is introduced here and reused where the GST export halts and at the local
 * API gate. Two compositions of the same drawing are rendered (wide for
 * desktop, tall for tablets and phones) and CSS shows one, so every label is
 * real text at a readable size at every width.
 *
 * Every crossing drawn is an explicit import or export. Nothing points at the
 * internet. The three absences sit outside the line, joined to nothing.
 */
import type { CSSProperties, ReactNode } from "react";

const description =
  "A drawing of the edge of this Mac as one closed line. Inside the line are Tammy, the encrypted workspace and the macOS Keychain. " +
  "The line has three gates. In: files you choose, shown as a document crossing inward. " +
  "Out: copies you save, shown as a PDF and a CSV resting just outside the line; exported copies sit outside the encrypted workspace. " +
  "Local API: a closed gate, shut until you enable it, and then for clients on this Mac only. " +
  "Outside the line, joined to nothing, are three things that are not there: no Tammy cloud, no bank feed and no ATO submission.";

const absences = ["No Tammy cloud", "No bank feed", "No ATO submission"] as const;

const after = (ms: number) => ({ "--d": `${ms}ms` }) as CSSProperties;

const place = (x: number, y: number, scale = 1) => `translate(${x} ${y})${scale === 1 ? "" : ` scale(${scale})`}`;

/** A sheet of paper, 36 by 46: ruled for a document, gridded for a table. */
function Sheet({ x, y, scale, kind = "text" }: { x: number; y: number; scale?: number; kind?: "text" | "table" }) {
  return (
    <g transform={place(x, y, scale)}>
      <path className="lp-privacy-g-paper" d="M3 0h23l10 10v33a3 3 0 0 1-3 3H3a3 3 0 0 1-3-3V3a3 3 0 0 1 3-3z" />
      <path className="lp-privacy-g-fold" d="M26 0v7a3 3 0 0 0 3 3h7" />
      <path className="lp-privacy-g-rule" d={kind === "text" ? "M7 20h22M7 27h22M7 34h13" : "M7 19h22v17H7zM7 27.5h22M17 19v17"} />
    </g>
  );
}

/** Tammy, as a small ledger window with its total band, 260 by 124. */
function TammyWindow({ x, y, scale }: { x: number; y: number; scale?: number }) {
  return (
    <g transform={place(x, y, scale)}>
      <rect className="lp-privacy-g-paper" width="260" height="124" rx="12" />
      <path className="lp-privacy-g-band" d="M0 94h260v18a12 12 0 0 1-12 12H12a12 12 0 0 1-12-12z" />
      <path className="lp-privacy-g-hair" d="M0 28h260" />
      <path className="lp-privacy-g-skel" d="M14 14h64M14 46h96M14 62h72M14 78h108M172 46h26M220 62h26M172 78h26" />
      <path className="lp-privacy-g-done" d="M14 109h44M172 109h26M220 109h26" />
      <path className="lp-privacy-g-lock" d="M235 12.5v-2.2a3.6 3.6 0 0 1 7.2 0v2.2M233.4 12.5h10.4v7.2h-10.4z" />
    </g>
  );
}

/** The encrypted workspace: a database with a lock on it, 132 by 122. */
function Vault({ x, y, scale }: { x: number; y: number; scale?: number }) {
  return (
    <g transform={place(x, y, scale)}>
      <path className="lp-privacy-g-vault" d="M0 16v90a66 16 0 0 0 132 0V16" />
      <ellipse className="lp-privacy-g-vault" cx="66" cy="16" rx="66" ry="16" />
      <path className="lp-privacy-g-ring" d="M0 46a66 16 0 0 0 132 0M0 76a66 16 0 0 0 132 0" />
      <path className="lp-privacy-g-shackle" d="M56 64v-8a10 10 0 0 1 20 0v8" />
      <rect className="lp-privacy-g-padlock" x="49" y="63" width="34" height="27" rx="6" />
      <path className="lp-privacy-g-keyhole" d="M66 73v6" />
    </g>
  );
}

/** The macOS Keychain: a key on a tile, 114 by 114. */
function Keychain({ x, y, scale }: { x: number; y: number; scale?: number }) {
  return (
    <g transform={place(x, y, scale)}>
      <rect className="lp-privacy-g-vault" width="114" height="114" rx="27" />
      <circle className="lp-privacy-g-key" cx="71" cy="43" r="14" />
      <path className="lp-privacy-g-key" d="M61 53 31 83M41 73l10 10M31 83l9 9" />
    </g>
  );
}

/**
 * A label set on the line. The first copy is painted in the ground colour with
 * a wide stroke, so the line breaks around the words whatever font renders
 * them; the second is the label itself, which arrives after the line is drawn.
 * (The .lp-privacy-gap path clears the gate openings and the body of each
 * label; the halo fits the break to the ends of the words.)
 */
function OnLine({ x, y, wait, className = "", children }: { x: number; y: number; wait: number; className?: string; children: ReactNode }) {
  return (
    <>
      <text className={`lp-privacy-t lp-privacy-halo ${className}`} x={x} y={y}>
        {children}
      </text>
      <text className={`lp-privacy-t ${className}`} x={x} y={y} data-enter="fade" style={after(wait)}>
        {children}
      </text>
    </>
  );
}

function Wide() {
  return (
    // oxlint-disable-next-line jsx-a11y/prefer-tag-over-role -- An inline drawing with live, themed text: an img element cannot carry it.
    <svg className="lp-privacy-svg lp-privacy-svg--wide" viewBox="0 0 1320 436" role="img" aria-label={description}>
      {/* What the line encloses. Present from the start: the line is drawn around it. */}
      <TammyWindow x={84} y={106} />
      <Vault x={430} y={106} />
      <Keychain x={800} y={110} />
      <text className="lp-privacy-t" x="84" y="268">Tammy</text>
      <text className="lp-privacy-t" x="430" y="268">Encrypted workspace</text>
      <text className="lp-privacy-t" x="800" y="268">macOS Keychain</text>

      <path
        className="lp-line lp-privacy-contour"
        data-enter="draw"
        pathLength={1}
        style={after(200)}
        d="M170 52H1018a28 28 0 0 1 28 28V302a28 28 0 0 1-28 28H30a28 28 0 0 1-28-28V80a28 28 0 0 1 28-28z"
      />
      <path className="lp-privacy-gap" d="M50 52h96M236 52h269M58 330h84M640 330h286" />

      <OnLine x={56} y={59} wait={760} className="lp-privacy-t--line">This Mac</OnLine>

      {/* In: a document crossing inward. */}
      <path className="lp-privacy-post" d="M236 41v22M300 41v22" data-enter="fade" style={after(860)} />
      <g className="lp-privacy-cross lp-privacy-cross--in" data-enter="fade" style={after(960)}>
        <Sheet x={250} y={22} />
        <path className="lp-privacy-post" d="M268 73v17M261.5 83.5l6.5 6.5 6.5-6.5" />
      </g>
      <OnLine x={316} y={59} wait={960}>In: files you choose</OnLine>

      {/* Local API: a closed gate. Its label is the HTML link laid over the drawing. */}
      <g data-enter="fade" style={after(1100)}>
        <path className="lp-privacy-post" d="M58 319v22M122 319v22" />
        <path className="lp-privacy-bolt" d="M58 330h64" />
      </g>
      <text className="lp-privacy-t lp-privacy-t--quiet" x="142" y="378" data-enter="fade" style={after(1320)}>
        For clients on this Mac only.
      </text>

      {/* Out: copies resting just outside. */}
      <path className="lp-privacy-post" d="M640 319v22M704 319v22M672 308v32M665 333l7 7 7-7" data-enter="fade" style={after(1320)} />
      <OnLine x={720} y={337} wait={1400}>Out: copies you save</OnLine>
      <g className="lp-privacy-cross lp-privacy-cross--out" data-enter="fade" style={after(1540)}>
        <Sheet x={632} y={356} />
        <Sheet x={672} y={356} kind="table" />
      </g>
      <g data-enter="fade" style={after(1680)}>
        <text className="lp-privacy-t" x="720" y="374">PDF or CSV</text>
        <text className="lp-privacy-t lp-privacy-t--quiet" x="720" y="400">Exported copies sit outside</text>
        <text className="lp-privacy-t lp-privacy-t--quiet" x="720" y="426">the encrypted workspace.</text>
      </g>

      {/* Outside the line, joined to nothing. */}
      {absences.map((absence, index) => (
        <g key={absence} data-enter="fade" style={after(1860 + index * 110)}>
          <rect className="lp-privacy-absent" x="1092" y={77 + index * 86} width="226" height="56" rx="14" />
          <text className="lp-privacy-t lp-privacy-t--quiet" x="1205" y={112 + index * 86} textAnchor="middle">
            {absence}
          </text>
        </g>
      ))}
    </svg>
  );
}

function Tall() {
  return (
    // oxlint-disable-next-line jsx-a11y/prefer-tag-over-role -- An inline drawing with live, themed text: an img element cannot carry it.
    <svg className="lp-privacy-svg lp-privacy-svg--tall" viewBox="0 0 340 620" role="img" aria-label={description}>
      <text className="lp-privacy-t lp-privacy-t--line" x="30" y="100">This Mac</text>
      <TammyWindow x={30} y={124} scale={0.32} />
      <Vault x={44} y={176} scale={0.42} />
      <Keychain x={47.5} y={236} scale={0.42} />
      <text className="lp-privacy-t" x="128" y="150">Tammy</text>
      <text className="lp-privacy-t" x="128" y="208">Encrypted workspace</text>
      <text className="lp-privacy-t" x="128" y="266">macOS Keychain</text>

      <path
        className="lp-line lp-privacy-contour"
        data-enter="draw"
        pathLength={1}
        style={after(200)}
        d="M98 46H306a24 24 0 0 1 24 24V380a24 24 0 0 1-24 24H34a24 24 0 0 1-24-24V70a24 24 0 0 1 24-24z"
      />
      <path className="lp-privacy-gap" d="M36 46h216M10 298v50M36 404h230" />

      {/* In */}
      <path className="lp-privacy-post" d="M36 36v20M86 36v20" data-enter="fade" style={after(860)} />
      <g className="lp-privacy-cross lp-privacy-cross--in" data-enter="fade" style={after(960)}>
        <Sheet x={45} y={18} scale={0.89} />
        <path className="lp-privacy-post" d="M61 63v13M55.5 70.5l5.5 5.5 5.5-5.5" />
      </g>
      <OnLine x={98} y={52} wait={960}>In: files you choose</OnLine>

      {/* Local API: a closed gate on the side. Its label is the HTML link. */}
      <g data-enter="fade" style={after(1100)}>
        <path className="lp-privacy-post" d="M0 298h20M0 348h20" />
        <path className="lp-privacy-bolt" d="M10 298v50" />
      </g>
      <text className="lp-privacy-t lp-privacy-t--quiet" x="32" y="374" data-enter="fade" style={after(1320)}>
        For clients on this Mac only.
      </text>

      {/* Out */}
      <path className="lp-privacy-post" d="M36 394v20M86 394v20M61 384v29M55 407l6 6 6-6" data-enter="fade" style={after(1320)} />
      <OnLine x={98} y={410} wait={1400}>Out: copies you save</OnLine>
      <g className="lp-privacy-cross lp-privacy-cross--out" data-enter="fade" style={after(1540)}>
        <Sheet x={31} y={428} scale={0.76} />
        <Sheet x={61.5} y={428} scale={0.76} kind="table" />
      </g>
      <g data-enter="fade" style={after(1680)}>
        <text className="lp-privacy-t" x="98" y="442">PDF or CSV</text>
        <text className="lp-privacy-t lp-privacy-t--quiet" x="98" y="466">Exported copies sit</text>
        <text className="lp-privacy-t lp-privacy-t--quiet" x="98" y="488">outside the encrypted</text>
        <text className="lp-privacy-t lp-privacy-t--quiet" x="98" y="510">workspace.</text>
      </g>

      {/* Outside the line, joined to nothing. */}
      <g data-enter="fade" style={after(1860)}>
        <rect className="lp-privacy-absent" x="10" y="536" width="160" height="38" rx="12" />
        <text className="lp-privacy-t lp-privacy-t--quiet" x="90" y="561" textAnchor="middle">{absences[0]}</text>
      </g>
      <g data-enter="fade" style={after(1970)}>
        <rect className="lp-privacy-absent" x="180" y="536" width="138" height="38" rx="12" />
        <text className="lp-privacy-t lp-privacy-t--quiet" x="249" y="561" textAnchor="middle">{absences[1]}</text>
      </g>
      <g data-enter="fade" style={after(2080)}>
        <rect className="lp-privacy-absent" x="10" y="582" width="186" height="38" rx="12" />
        <text className="lp-privacy-t lp-privacy-t--quiet" x="103" y="607" textAnchor="middle">{absences[2]}</text>
      </g>
    </svg>
  );
}

/** The drawing, with the one label that is a link laid over it as HTML. */
export function PrivacyLine() {
  return (
    <figure className="lp-privacy-map" data-scene="">
      <div className="lp-privacy-map-canvas">
        <Wide />
        <Tall />
        <a className="lp-privacy-api" href="#integrations">
          <span data-enter="fade" style={after(1200)}>
            Local API: shut <br />
            until you enable it
          </span>
        </a>
      </div>
    </figure>
  );
}
