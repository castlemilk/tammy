import type {
  PolicyInline,
  PolicySection,
} from '../content/public-content.generated';

function PolicyInlineContent({ inline }: { inline: PolicyInline }) {
  switch (inline.type) {
    case 'text':
      return inline.value;
    case 'emphasis':
      return <em>{inline.value}</em>;
    case 'code':
      return <code>{inline.value}</code>;
    case 'link':
      return <a href={inline.href}>{inline.text}</a>;
  }
}

function InlineSequence({ inlines }: { inlines: readonly PolicyInline[] }) {
  return inlines.map((inline, index) => (
    <PolicyInlineContent
      key={`${inline.type}-${'value' in inline ? inline.value : inline.href}-${index}`}
      inline={inline}
    />
  ));
}

export function PolicyDocument({
  sections,
}: {
  sections: readonly PolicySection[];
}) {
  return sections.map((section) => (
    <section className="policy-section" key={section.heading}>
      <h2>{section.heading}</h2>
      {section.blocks.map((block, blockIndex) => {
        if (block.kind === 'paragraph') {
          return (
            <p key={`${section.heading}-paragraph-${blockIndex}`}>
              <InlineSequence inlines={block.inlines} />
            </p>
          );
        }

        return (
          <ul key={`${section.heading}-list-${blockIndex}`}>
            {block.items.map((item, itemIndex) => (
              <li key={`${section.heading}-item-${itemIndex}`}>
                <InlineSequence inlines={item} />
              </li>
            ))}
          </ul>
        );
      })}
    </section>
  ));
}
