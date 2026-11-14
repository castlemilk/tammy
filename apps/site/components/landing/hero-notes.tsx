/*
 * The margin note beside the hero window: one per rail item, plus one for the
 * locked state. Each is one claim in plain words, one limit, and where it
 * helps a link to the band that shows it working. hero.css shows the note
 * whose rail radio is checked.
 */
import type { ReactNode } from "react";
import { Icon, type IconName, Note } from "./kit";
import type { ScreenId } from "./hero-screens";

type HeroNote = {
  readonly id: ScreenId | "locked";
  readonly icon: IconName;
  readonly label: string;
  readonly claim: string;
  readonly limit: ReactNode;
  readonly link?: { readonly href: string; readonly label: string };
};

const ledgerLink = { href: "#workflow", label: "How the books come together" } as const;

const notes: readonly HeroNote[] = [
  {
    id: "overview",
    icon: "overview",
    label: "Overview",
    claim: "Income before GST, payments received and invoices owing, for the period you choose.",
    limit: "A summary of posted records. Not a profit and loss or cash-flow statement.",
    link: { href: "#features", label: "What the workspace does" },
  },
  {
    id: "documents",
    icon: "document",
    label: "Documents",
    claim: "Supplier PDFs and receipt images stay in the workspace, and you check the details yourself.",
    limit: "Reviewed is not posted. A purchase reaches the ledger only when you confirm it.",
    link: ledgerLink,
  },
  {
    id: "invoices",
    icon: "invoice",
    label: "Invoices",
    claim: "Issuing an invoice records the amount owing, the income and the GST in one step.",
    limit: "Nothing is sent for you. You export the PDF and deliver it yourself.",
    link: { href: "#features", label: "Invoicing in detail" },
  },
  {
    id: "banking",
    icon: "bank",
    label: "Banking",
    claim: "Import bank-statement rows, match each one yourself, then complete the reconciliation.",
    limit: "No bank feed, and nothing is matched for you.",
    link: ledgerLink,
  },
  {
    id: "chart",
    icon: "chart",
    label: "Chart of accounts",
    claim: "An Australian small-business chart is installed at setup. You add your own ordinary accounts.",
    limit: "Protected accounts cannot be posted to by hand, and there is no screen to edit or archive an account.",
  },
  {
    id: "journals",
    icon: "journal",
    label: "Journals",
    claim: "Every posting is a balanced journal you can open and read, whichever screen it came from.",
    limit: "A manual journal takes two ordinary accounts. No reversals, opening balances or period close.",
    link: ledgerLink,
  },
  {
    id: "trial-balance",
    icon: "balance",
    label: "Trial balance",
    claim: "Debits and credits as at any date you choose.",
    limit: "Not a profit and loss or balance sheet.",
    link: ledgerLink,
  },
  {
    id: "gst",
    icon: "gst",
    label: "GST & BAS",
    claim: "A local workpaper of G1, 1A, 1B and net GST, with the records behind each figure.",
    limit: "Always a draft. Tammy does not send anything to the ATO.",
    link: { href: "#bas", label: "The GST workpaper" },
  },
  {
    id: "audit",
    icon: "audit",
    label: "Audit trail",
    claim: "Journals, bank lines, documents and the GST draft, listed in date order.",
    limit: "A convenience view of loaded records, not a verified audit log.",
  },
  {
    id: "locked",
    icon: "lock",
    label: "As stored",
    claim: "On disk the workspace is encrypted. It opens when you unlock it on your Mac.",
    limit: "Encryption cannot protect records from someone using your unlocked Mac.",
    link: { href: "#privacy-first", label: "How it is kept private" },
  },
];

export function HeroNotes() {
  return (
    <div className="lp-hero-notes">
      <p className="lp-hero-hint">Choose a screen, or lock the example.</p>
      {notes.map((note) => (
        <div className="lp-hero-note" data-note={note.id} key={note.id}>
          <p className="lp-hero-note-label">
            <Icon name={note.icon} />
            {note.label}
          </p>
          <p className="lp-hero-note-claim">{note.claim}</p>
          <Note>{note.limit}</Note>
          {note.link ? (
            <a className="lp-link lp-hero-note-link" href={note.link.href}>
              {note.link.label}
            </a>
          ) : null}
        </div>
      ))}
    </div>
  );
}
