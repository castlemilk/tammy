/* oxlint-disable next/no-html-link-for-pages -- Vinext serves these real same-origin routes. */
/* oxlint-disable next/no-img-element -- Editorial figures use committed local artwork, not remote pixels. */
import type { CSSProperties, ElementType, ReactNode } from "react";

type CalloutTone = "note" | "caution" | "method";

export function Callout({ tone = "note", title, children }: {
  readonly tone?: CalloutTone;
  readonly title: string;
  readonly children: ReactNode;
}) {
  const headings = { note: "Worth noting", caution: "Check the exception", method: "The method" };
  return <aside className={"tammy-mdx-callout tammy-mdx-callout-" + tone} aria-label={title}>
    <span className="tammy-mdx-callout-kind">{headings[tone]}</span>
    <strong className="tammy-mdx-callout-title">{title}</strong>
    <div>{children}</div>
  </aside>;
}

export function WorkedExample({ title, children }: {
  readonly title: string;
  readonly children: ReactNode;
}) {
  return <section className="tammy-mdx-example" aria-label={title}>
    <h3>{title}</h3>
    <div>{children}</div>
    <small>Illustrative example. The amounts are assumptions, not customer records.</small>
  </section>;
}

export function Formula({ label, expression, result }: {
  readonly label: string;
  readonly expression: string;
  readonly result?: string;
}) {
  return <figure className="tammy-mdx-formula">
    <figcaption>{label}</figcaption>
    <div className="tammy-mdx-formula-expression">{expression}</div>
    {result && <p><strong>{result}</strong></p>}
  </figure>;
}

type TableCell = string | number;
export function MoneyTable({ caption, columns, rows }: {
  readonly caption: string;
  readonly columns: readonly string[];
  readonly rows: readonly (readonly TableCell[])[];
}) {
  if (!columns.length || rows.some((row) => row.length !== columns.length))
    throw new Error("MoneyTable rows must have the same number of cells as columns");
  return <figure className="tammy-mdx-table-wrap">
    <div className="tammy-mdx-table-scroll">
      <table className="tammy-mdx-table">
        <caption>{caption}</caption>
        <thead><tr>{columns.map((column, i) => <th key={i} scope="col">{column}</th>)}</tr></thead>
        <tbody>{rows.map((row, rowIndex) => <tr key={rowIndex}>
          {row.map((cell, colIndex) => colIndex === 0
            ? <th key={colIndex} scope="row">{cell}</th>
            : <td key={colIndex}>{cell}</td>)}
        </tr>)}</tbody>
      </table>
    </div>
  </figure>;
}

export interface ChartItem {
  readonly label: string;
  readonly value: number;
  readonly detail?: string;
}
export function BarChart({ title, unit, items }: {
  readonly title: string;
  readonly unit: string;
  readonly items: readonly ChartItem[];
}) {
  if (!items.length || items.length > 8 || items.some((item) =>
    !Number.isFinite(item.value) || item.value < 0)) {
    throw new Error("BarChart requires 1–8 non-negative finite values");
  }
  const max = Math.max(...items.map((item) => item.value), 1);
  const format = new Intl.NumberFormat("en-AU", { maximumFractionDigits: 2 });
  return <figure className="tammy-mdx-chart">
    <figcaption>{title}</figcaption>
    <div className="tammy-mdx-chart-bars" aria-hidden="true">
      {items.map((item, i) => <div className="tammy-mdx-chart-row" key={item.label}>
        <span className="tammy-mdx-chart-label">{item.label}</span>
        <div className="tammy-mdx-chart-track">
          <span className="tammy-mdx-chart-fill" data-series={i % 3}
            style={{ "--bar-width": (item.value / max * 100).toFixed(3) + "%" } as CSSProperties}/>
        </div>
        <span className="tammy-mdx-chart-value">{format.format(item.value)} {unit}</span>
      </div>)}
    </div>
    <table className="tammy-mdx-chart-data">
      <caption>{title}. Data values in {unit}.</caption>
      <thead><tr><th scope="col">Scenario</th><th scope="col">Value</th><th scope="col">Context</th></tr></thead>
      <tbody>{items.map((item) => <tr key={item.label}>
        <th scope="row">{item.label}</th>
        <td>{format.format(item.value)} {unit}</td>
        <td>{item.detail ?? "Illustrative"}</td>
      </tr>)}</tbody>
    </table>
    <p className="tammy-mdx-chart-note">Illustrative values, not observed business performance.</p>
  </figure>;
}

export function Figure({ src, alt, caption }: {
  readonly src: string;
  readonly alt: string;
  readonly caption: string;
}) {
  if (!/^\/assets\/[a-z0-9-]+\.svg$/.test(src))
    throw new Error("Figure expects a local /assets/*.svg illustration");
  return <figure className="tammy-mdx-figure">
    <img src={src} alt={alt} width="1200" height="720" loading="lazy"/>
    <figcaption>{caption}</figcaption>
  </figure>;
}

export function ToolLink({ href, label, description }: {
  readonly href: string;
  readonly label: string;
  readonly description?: string;
}) {
  if (!/^\/tools\/[a-z-]+$/.test(href)) throw new Error("ToolLink only accepts Tammy tools");
  return <a className="tammy-mdx-tool-link" href={href}>
    <span><strong>{label}</strong>{description && <small>{description}</small>}</span>
    <svg viewBox="0 0 24 24" width="21" height="21" fill="none" aria-hidden="true">
      <path d="M5 12h14m-6-6 6 6-6 6" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round"/>
    </svg>
  </a>;
}

export function SourceNote({ url, label, context }: {
  readonly url: string;
  readonly label: string;
  readonly context?: string;
}) {
  if (!url.startsWith("https://")) throw new Error("SourceNote URL must be HTTPS");
  return <aside className="tammy-mdx-source">
    <span>Source</span>
    <a href={url} target="_blank" rel="noopener noreferrer">{label}</a>
    {context && <p>{context}</p>}
  </aside>;
}

/** All MDX components are deliberately registered in one place.
 * Body files cannot import arbitrary application code.
 */
export const tammyMdxComponents: Record<string, ElementType> = {
  Callout,
  WorkedExample,
  Formula,
  MoneyTable,
  BarChart,
  Figure,
  ToolLink,
  SourceNote,
};
