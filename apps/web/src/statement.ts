import { parseAmount } from "./money";

export function parseStatementRows(
  value: string,
): { date: string; description: string; amountMinor: number }[] {
  const rows = value
    .split(/\r?\n/)
    .map((row) => row.trim())
    .filter(Boolean);
  if (rows.length === 0 || rows.length > 1000)
    throw new Error("Enter between 1 and 1,000 statement rows.");
  return rows.map((row, index) => {
    const first = row.indexOf(",");
    const last = row.lastIndexOf(",");
    const date = row.slice(0, first).trim();
    const description = row.slice(first + 1, last).trim();
    const amountMinor = parseAmount(row.slice(last + 1));
    const parsedDate = new Date(`${date}T00:00:00Z`);
    if (
      first < 0 ||
      last <= first ||
      !/^\d{4}-\d{2}-\d{2}$/.test(date) ||
      Number.isNaN(parsedDate.getTime()) ||
      parsedDate.toISOString().slice(0, 10) !== date ||
      !description ||
      description.length > 256 ||
      amountMinor === undefined
    ) {
      throw new Error(
        `Check statement row ${index + 1}. Use YYYY-MM-DD, description, signed amount.`,
      );
    }
    return { date, description, amountMinor };
  });
}
