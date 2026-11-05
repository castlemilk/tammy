"use client";
/* oxlint-disable next/no-html-link-for-pages -- Vinext serves these same-origin routes. */
/* oxlint-disable next/no-img-element -- Local vector category icons have no raster payload. */
import { useState } from "react";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { matchTools, publicTools, toolCategories, type ToolCategory } from "../lib/tool-library";

export function ToolLibrary() {
  const [category, setCategory] = useState<ToolCategory | "all">("all");
  const [query, setQuery] = useState("");
  const results = matchTools(query, category);

  function reset() {
    setQuery("");
    setCategory("all");
  }

  return <>
    <div className="tools-library-controls">
      <fieldset className="tools-library-filter-group"><legend>Filter by business task</legend>
        <div className="tools-library-filters">
        {toolCategories.map((item) => {
          const count = item.id === "all"
            ? publicTools.length
            : publicTools.filter((tool) => tool.category === item.id).length;
          return <Button key={item.id} type="button" variant="ghost" className="tools-library-filter"
            aria-pressed={category === item.id} onClick={() => setCategory(item.id)}>
            {item.label}<span className="tools-library-filter-count">{count}</span>
          </Button>;
        })}
      </div>
      </fieldset>
      <label className="tools-library-search" htmlFor="tammy-tool-search">
        <span>Find a tool</span>
        <span className="tools-library-search-input">
          <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" width="19" height="19">
            <circle cx="10.5" cy="10.5" r="6.5" stroke="currentColor" strokeWidth="1.7"/>
            <path d="m15.3 15.3 4.4 4.4" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round"/>
          </svg>
          <Input id="tammy-tool-search" type="search" name="tool-search" value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="GST, pricing, invoices..." autoComplete="off" />
        </span>
      </label>
    </div>

    <output className="tools-library-count" aria-live="polite">
      {results.length === 1 ? "1 tool" : results.length + " tools"} available
      {category !== "all" || query.trim() ? " for this selection" : ""}
    </output>
    {results.length > 0 ? <div className="tools-card-grid tools-library-grid" id="tool-results">
      {results.map((tool) => <a className="tools-card tools-library-item"
          key={tool.href} href={tool.href} data-featured={tool.featured && category === "all" && !query.trim() ? "true" : undefined}>
        <div className="tools-library-top">
          <span className="tools-library-type">{tool.categoryLabel}</span>
          <span className="tools-library-icon" aria-hidden="true">
            <img src={"/assets/" + tool.icon + ".svg"} alt="" width="34" height="34" loading="lazy"/>
          </span>
        </div>
        <div className="tools-library-copy">
          <h3>{tool.name}</h3>
          <p>{tool.summary}</p>
        </div>
        {tool.featured && category === "all" && !query.trim() && <div className="tools-library-paper" aria-hidden="true">
          <img src="/assets/tammy-invoice-fold.svg" alt="" width="340" height="270" loading="lazy" />
        </div>}
        <span className="tools-card-foot">
          {tool.note}
          <span className="tools-library-direction" aria-hidden="true">
            <svg viewBox="0 0 24 24" width="20" height="20" fill="none">
              <path d="M5 12h14m-6-6 6 6-6 6" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"/>
            </svg>
          </span>
        </span>
      </a>)}
    </div> : <div className="tools-library-empty">
      <strong>No matching tools.</strong>
      <p>Try searching for an accounting task, or browse the complete library.</p>
      <Button type="button" onClick={reset}>Show all tools</Button>
    </div>}
  </>;
}
