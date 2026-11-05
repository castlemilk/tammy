"use client";

import { useId, useState } from "react";

export function ReviewChecklist({ title, items }: {
  readonly title: string;
  readonly items: readonly string[];
}) {
  const [checked, setChecked] = useState<readonly number[]>([]);
  const id = useId();
  if (items.length < 2 || items.length > 12 || new Set(items).size !== items.length) {
    throw new Error("ReviewChecklist needs 2–12 distinct review steps");
  }
  const toggle = (index: number, next: boolean) =>
    setChecked((previous) => next
      ? [...previous.filter((item) => item !== index), index]
      : previous.filter((item) => item !== index));
  return <section className="tammy-mdx-review" aria-labelledby={id}>
    <div className="tammy-mdx-review-heading">
      <h3 id={id}>{title}</h3>
      <output aria-live="polite" aria-atomic="true">
        {checked.length} of {items.length} reviewed
      </output>
    </div>
    <div className="tammy-mdx-review-items">
      {items.map((item, index) => <label key={item}>
        <input type="checkbox" checked={checked.includes(index)}
          onChange={(event) => toggle(index, event.target.checked)}/>
        <span>{item}</span>
      </label>)}
    </div>
    <div className="tammy-mdx-review-footer">
      <p>Personal reading checklist only. Your selections are not saved and do not verify or submit records.</p>
      <button type="button" onClick={() => setChecked([])} disabled={checked.length === 0}>Reset checklist</button>
    </div>
  </section>;
}
