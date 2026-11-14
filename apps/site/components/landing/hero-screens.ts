/*
 * The nine destinations of the desktop rail, in the app's own three groups
 * (apps/desktop/src/app/app-shell/navigation.tsx). The hero's rail, panels and
 * margin notes are all keyed by these ids; hero.css shows the matching panel
 * and note with :has(input:checked), so the ids must stay in step with it.
 */
import type { IconName } from "./kit";

export type ScreenId =
  | "overview"
  | "documents"
  | "invoices"
  | "banking"
  | "chart"
  | "journals"
  | "trial-balance"
  | "gst"
  | "audit";

export const DEFAULT_SCREEN: ScreenId = "trial-balance";

export const railGroups: readonly {
  readonly label: string;
  readonly items: readonly { readonly id: ScreenId; readonly label: string; readonly icon: IconName }[];
}[] = [
  {
    label: "Workspace",
    items: [
      { id: "overview", label: "Overview", icon: "overview" },
      { id: "documents", label: "Documents", icon: "document" },
      { id: "invoices", label: "Invoices", icon: "invoice" },
    ],
  },
  {
    label: "Accounting",
    items: [
      { id: "banking", label: "Banking", icon: "bank" },
      { id: "chart", label: "Chart of accounts", icon: "chart" },
      { id: "journals", label: "Journals", icon: "journal" },
      { id: "trial-balance", label: "Trial balance", icon: "balance" },
    ],
  },
  {
    label: "Activity",
    items: [
      { id: "gst", label: "GST & BAS", icon: "gst" },
      { id: "audit", label: "Audit trail", icon: "audit" },
    ],
  },
];
