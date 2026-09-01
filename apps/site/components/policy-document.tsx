import type { PolicyInline, PolicySection } from "../content/public-content.generated";

function PolicyInlineContent({ inline }: { inline: PolicyInline }) {
  switch (inline.type) {
    case "text":
      return inline.value;
    case "emphasis":
      return <em>{inline.value}</em>;
    case "code":
      return <code>{inline.value}</code>;
    case "link":
      return <a href={inline.href}>{inline.text}</a>;
  }
}

function InlineSequence({ inlines }: { inlines: readonly PolicyInline[] }) {
  return inlines.map((inline) => (
    <PolicyInlineContent key={`${inline.type}-${JSON.stringify(inline)}`} inline={inline} />
  ));
}

export function PolicyDocument({ sections }: { sections: readonly PolicySection[] }) {
  return sections.map((section) => (
    <section className="policy-section" key={section.heading}>
      <h2>{section.heading}</h2>
      {section.blocks.map((block) => {
        if (block.kind === "paragraph") {
          return (
            <p key={`${section.heading}-${JSON.stringify(block)}`}>
              <InlineSequence inlines={block.inlines} />
            </p>
          );
        }

        return (
          <ul key={`${section.heading}-${JSON.stringify(block)}`}>
            {block.items.map((item) => (
              <li key={`${section.heading}-${JSON.stringify(item)}`}>
                <InlineSequence inlines={item} />
              </li>
            ))}
          </ul>
        );
      })}
    </section>
  ));
}
