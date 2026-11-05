import type { ReactNode } from "react";

export type JournalLine = {
  readonly account: string;
  readonly debitCents?: number;
  readonly creditCents?: number;
};

export function JournalEntry({ title, lines, note }: {
  readonly title: string;
  readonly lines: readonly JournalLine[];
  readonly note?: ReactNode;
}) {
  const valid = lines.length >= 2 && lines.every((line) =>
    line.account.length > 0 &&
    [line.debitCents ?? 0, line.creditCents ?? 0].every(
      (value) => Number.isSafeInteger(value) && value >= 0) &&
    !((line.debitCents ?? 0) > 0 && (line.creditCents ?? 0) > 0));
  const debit = lines.reduce((sum, line) => sum + (line.debitCents ?? 0), 0);
  const credit = lines.reduce((sum, line) => sum + (line.creditCents ?? 0), 0);
  if (!valid || !Number.isSafeInteger(debit) || debit !== credit || debit <= 0)
    throw new Error("JournalEntry must contain a balanced debit and credit with valid cents");
  const money = (cents: number) => new Intl.NumberFormat("en-AU", {
    style: "currency", currency: "AUD",
  }).format(cents / 100);

  return <figure className="tammy-mdx-journal-entry">
    <table>
      <caption>{title}. Fictional double-entry example in Australian dollars.</caption>
      <thead><tr><th scope="col">Account</th><th scope="col">Debit</th><th scope="col">Credit</th></tr></thead>
      <tbody>{lines.map((line, index) => <tr key={line.account + index}>
        <th scope="row">{line.account}</th>
        <td>{line.debitCents ? money(line.debitCents) : "—"}</td>
        <td>{line.creditCents ? money(line.creditCents) : "—"}</td>
      </tr>)}</tbody>
      <tfoot><tr><th scope="row">Total</th><td>{money(debit)}</td><td>{money(credit)}</td></tr></tfoot>
    </table>
    {note && <figcaption>{note}</figcaption>}
  </figure>;
}
