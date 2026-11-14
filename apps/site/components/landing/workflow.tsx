/*
 * How it works: five steps a person takes, from a supplier's PDF to a trial
 * balance that agrees. Each step is a caption followed by its own figure in
 * its finished state; that is the page without script, on phones and under
 * reduced motion. With script, at desktop widths, ScrollSteps pins the
 * figures into one sticky window and the captions scroll past it
 * (workflow.css), and each figure plays its step once (workflow-figure.tsx).
 */
import { type CSSProperties, Fragment, type ReactNode } from "react";
import { Icon, type IconName, Note } from "./kit";
import { ScrollSteps } from "./motion";
import { purchase, statement, trialBalance } from "./scenario";
import { aud } from "../../lib/tools/calculations";
import { WorkflowFigure } from "./workflow-figure";
import { BankingScene, DocumentsScene, JournalScene, PurchaseScene, TrialBalanceScene } from "./workflow-scenes";

/** The name of an app button, as it is written on the button. */
function Key({ children }: { children: ReactNode }) {
  return <strong className="lp-workflow-key">{children}</strong>;
}

const steps: readonly {
  name: string;
  icon: IconName;
  heading: string;
  body: ReactNode;
  note: string;
  /** What the finished figure shows, for assistive technology. */
  label: string;
  scene: ReactNode;
}[] = [
  {
    name: "Documents",
    icon: "document",
    heading: "Reviewed is one step. Posted is another.",
    body: (
      <>
        You upload the supplier&apos;s PDF and Tammy keeps it in the workspace, marked Needs review. You check each
        detail against the text of the file, then press <Key>Save review</Key>. The document is now Reviewed and your
        ledger has not changed.
      </>
    ),
    note: "Text is read from the PDF on this Mac; there is no cloud OCR. Saving a review does not post accounting entries.",
    label: `Illustrative Documents screen. ${purchase.file} from ${purchase.supplier}, ${aud(purchase.totalCents)}, has been checked and is marked Reviewed.`,
    scene: <DocumentsScene />,
  },
  {
    name: "Purchases",
    icon: "plus",
    heading: "Post it once.",
    body: (
      <>
        A reviewed document stays out of your accounts until you press <Key>Add purchase to accounts</Key>, read what
        will be recorded and press <Key>Confirm purchase posting</Key>. Three lines post together: the expense, the GST
        and what you owe the supplier. Dated part or full payments are recorded afterwards.
      </>
    ),
    note: "You confirm each allocation. No bank payment is made.",
    label: `Illustrative purchase accounting panel. The reviewed invoice ${purchase.reference} has been added to accounts as three lines that total ${aud(purchase.totalCents)} on each side.`,
    scene: <PurchaseScene />,
  },
  {
    name: "Journals",
    icon: "journal",
    heading: "Every entry lands on both sides.",
    body: (
      <>
        For an entry with no source document, you open a manual entry, choose the account to debit and the account to
        credit, and type one amount. Tammy shows both totals and the difference between them before you press{" "}
        <Key>Post journal</Key>.
      </>
    ),
    note: "Manual journals use two ordinary accounts. Opening balances, period close and reversals are not in this release.",
    label: "Illustrative Journals screen. A manual entry debits Office expenses and credits Owner contributions by the same amount, the difference is nil, and the journal is Posted.",
    scene: <JournalScene />,
  },
  {
    name: "Banking",
    icon: "bank",
    heading: "You make every match.",
    body: (
      <>
        You import bank-statement rows and every one starts Unmatched. For each row you press <Key>Match</Key>, enter
        the reference of the record you reviewed and press <Key>Confirm match</Key>. Once every row is matched,{" "}
        <Key>Complete reconciliation</Key> marks them Reconciled and shows the closing balance.
      </>
    ),
    note: "There is no automatic matching, and a match creates no accounting entry.",
    label: `Illustrative Banking screen. Both imported statement lines have been matched by reference and are Reconciled, with a closing balance of ${aud(statement.closingCents)}.`,
    scene: <BankingScene />,
  },
  {
    name: "Trial balance",
    icon: "balance",
    heading: "Debits equal credits.",
    body: (
      <>
        You open <Key>Trial balance</Key> and everything posted so far is there in seven accounts, including the invoice
        you will meet in the next section. Both columns come to {aud(trialBalance.totalCents)}. It is the sheet you saw
        unlock at the top of this page.
      </>
    ),
    note: "A trial balance as at any date you choose.",
    label: `Illustrative Trial balance screen as at ${trialBalance.asAt}. Seven accounts; debits and credits both total ${aud(trialBalance.totalCents)}.`,
    scene: <TrialBalanceScene />,
  },
];

export function Workflow() {
  return (
    <section
      className="lp-section lp-workflow lp-perf"
      id="workflow"
      data-room="mac"
      aria-labelledby="workflow-heading"
      style={{ "--l-perf": "var(--l-oat)" } as CSSProperties}
    >
      <div className="lp-frame">
        <header className="lp-section-head lp-workflow-head">
          <h2 className="lp-h2" id="workflow-heading">
            From source records to a balanced ledger.
          </h2>
          <p className="lp-deck">
            <strong>Bring in the receipt. Check it. Post it once.</strong> Each of the five steps below is one you take
            yourself, and nothing posts by itself.
          </p>
        </header>

        <ScrollSteps className="lp-workflow-steps">
          {steps.map((step, index) => {
            const number = index + 1;
            return (
              <Fragment key={step.name}>
                <div className="lp-workflow-caption" data-step={number} style={{ "--step": number } as CSSProperties}>
                  <p className="lp-workflow-where">
                    <Icon name={step.icon} />
                    <span>
                      Step {number} of {steps.length}
                    </span>
                    {step.name}
                  </p>
                  <h3 className="lp-h3">{step.heading}</h3>
                  <p className="lp-body">{step.body}</p>
                  <Note>{step.note}</Note>
                </div>
                <WorkflowFigure step={number} name={step.name} label={step.label}>
                  {step.scene}
                </WorkflowFigure>
              </Fragment>
            );
          })}
        </ScrollSteps>
      </div>
    </section>
  );
}
