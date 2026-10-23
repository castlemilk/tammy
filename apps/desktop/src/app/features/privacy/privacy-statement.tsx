import {
  type PolicyInline,
  type PolicySection,
  publicContent,
} from "../../../../../site/content/public-content.generated";
import { TAMMY_PUBLIC_LINKS } from "../../../shared/official-resources";
import { readPublicLinks } from "../../../shared/public-links";
import { Button } from "../../components/ui/button";

const { policy } = publicContent;

function sectionId(heading: string): string {
  return `privacy-${heading
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "")}`;
}

function PolicyLink({
  inline,
}: {
  readonly inline: Extract<PolicyInline, { readonly type: "link" }>;
}) {
  if (inline.href.startsWith("mailto:")) {
    return <span>{inline.text}</span>;
  }
  return (
    <a
      className="font-medium text-forest underline underline-offset-2"
      href={inline.href}
      rel="noopener noreferrer"
      target="_blank"
    >
      {inline.text}
    </a>
  );
}

function InlineContent({ inline }: { readonly inline: PolicyInline }) {
  switch (inline.type) {
    case "text":
      return inline.value;
    case "emphasis":
      return <em>{inline.value}</em>;
    case "code":
      return <code className="rounded bg-muted px-1 py-0.5">{inline.value}</code>;
    case "link":
      return <PolicyLink inline={inline} />;
  }
}

function InlineSequence({ inlines }: { readonly inlines: readonly PolicyInline[] }) {
  return inlines.map((inline) => (
    <InlineContent key={`${inline.type}-${JSON.stringify(inline)}`} inline={inline} />
  ));
}

function PolicySections({ sections }: { readonly sections: readonly PolicySection[] }) {
  return sections.map((section) => (
    <section
      aria-labelledby={`${sectionId(section.heading)}-heading`}
      className="scroll-mt-8 border-b border-border py-7 last:border-b-0"
      id={sectionId(section.heading)}
      key={section.heading}
    >
      <h2
        className="font-serif text-[21px] font-semibold tracking-[-0.02em] text-foreground"
        id={`${sectionId(section.heading)}-heading`}
        tabIndex={-1}
      >
        {section.heading}
      </h2>
      <div className="mt-3 grid gap-3 text-[14px] leading-7 text-muted-foreground">
        {section.blocks.map((block) => {
          if (block.kind === "paragraph") {
            return (
              <p key={`${section.heading}-${JSON.stringify(block)}`}>
                <InlineSequence inlines={block.inlines} />
              </p>
            );
          }
          return (
            <ul
              className="list-disc space-y-2 pl-5"
              key={`${section.heading}-${JSON.stringify(block)}`}
            >
              {block.items.map((item) => (
                <li key={`${section.heading}-${JSON.stringify(item)}`}>
                  <InlineSequence inlines={item} />
                </li>
              ))}
            </ul>
          );
        })}
      </div>
    </section>
  ));
}

function PolicyContents({ sections }: { readonly sections: readonly PolicySection[] }) {
  return (
    <nav
      aria-label="Privacy policy contents"
      className="rounded-xl border border-border bg-surface p-5"
    >
      <h2 className="text-[12px] font-semibold uppercase tracking-[0.1em] text-forest">
        In this policy
      </h2>
      <ol className="mt-3 grid gap-x-6 gap-y-2 text-[12px] leading-5">
        {sections.map((section) => (
          <li key={section.heading}>
            <a
              className="font-medium text-forest underline underline-offset-2"
              href={`#${sectionId(section.heading)}`}
              onClick={(event) => {
                event.preventDefault();
                const target = document.getElementById(`${sectionId(section.heading)}-heading`);
                target?.scrollIntoView?.({ block: "start" });
                target?.focus({ preventScroll: true });
              }}
            >
              {section.heading}
            </a>
          </li>
        ))}
      </ol>
    </nav>
  );
}

export function PrivacyStatement({ onPrivacy }: { readonly onPrivacy?: () => void }) {
  const links = { ...TAMMY_PUBLIC_LINKS, ...readPublicLinks() };
  return (
    <section
      aria-labelledby="privacy-summary-heading"
      className="rounded-xl border border-border bg-surface p-5"
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-[10px] font-semibold uppercase tracking-[0.12em] text-forest">
            Your information
          </p>
          <h2
            className="mt-1 text-[15px] font-semibold text-foreground"
            id="privacy-summary-heading"
          >
            Privacy at a glance
          </h2>
        </div>
        <span className="rounded-full border border-border px-2.5 py-1 text-[10px] font-medium text-muted-foreground">
          Effective {policy.effectiveDate}
        </span>
      </div>
      <div className="mt-4 grid gap-3 text-[13px] leading-6 text-muted-foreground">
        <p>
          Your accounting workspace and selected evidence stay encrypted on this Mac. Tammy has no
          cloud workspace or cross-device sync and does not automatically send those records to
          Gamma Systems.
        </p>
        <p>
          Files are read only when you choose them. Reports you export go to the location you
          choose. Local API and MCP access is off until you explicitly enable it; an enabled local
          client with the private connection file can read permitted organisation data and stage
          invoice proposals for review in Tammy.
        </p>
        <p>
          Tammy does not include analytics, advertising, or tracking. The full policy explains
          imported evidence, ABN checks, local integrations, retention, access and correction
          requests, and privacy complaints.
        </p>
      </div>
      <div className="mt-4 flex flex-wrap items-center gap-x-5 gap-y-2 text-[12px]">
        {onPrivacy ? (
          <Button onClick={onPrivacy} type="button" variant="outline">
            Read full privacy policy
          </Button>
        ) : null}
        {links.privacyPolicy ? (
          <a
            className="font-medium text-forest underline underline-offset-2"
            href={links.privacyPolicy}
            rel="noopener noreferrer"
            target="_blank"
          >
            Read the published privacy policy
          </a>
        ) : null}
        {links.support ? (
          <a
            className="font-medium text-forest underline underline-offset-2"
            href={links.support}
            rel="noopener noreferrer"
            target="_blank"
          >
            Support and privacy requests
          </a>
        ) : null}
      </div>
    </section>
  );
}

export function PrivacyScreen({ onBack }: { readonly onBack: () => void }) {
  return (
    <main className="min-h-screen bg-background px-5 py-8 text-foreground">
      <div className="mx-auto grid w-full max-w-[1120px] gap-7">
        <header className="border-b border-border pb-5">
          <Button onClick={onBack} type="button" variant="outline">
            Back to Tammy
          </Button>
          <p className="mt-6 font-serif text-[16px] font-bold text-forest">Tammy</p>
          <h1 className="mt-1 font-serif text-[30px] font-semibold tracking-[-0.035em]">
            Privacy and support
          </h1>
          <p className="mt-2 text-[12px] text-muted-foreground">
            Effective {policy.effectiveDate}. Published by Gamma Systems Pty Ltd.
          </p>
        </header>
        <div className="grid items-start gap-7 lg:grid-cols-[250px_minmax(0,1fr)]">
          <div className="lg:sticky lg:top-6">
            <PolicyContents sections={policy.sections} />
          </div>
          <article className="min-w-0">
            <PolicySections sections={policy.sections} />
            <p className="pt-5 text-[13px] leading-6 text-muted-foreground">
              Privacy and access or correction requests: ben.ebsworth@gmail.com. Do not send
              accounting records or credentials by email.
            </p>
          </article>
        </div>
      </div>
    </main>
  );
}
