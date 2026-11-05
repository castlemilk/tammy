import type { ReactNode } from "react";
import "./tools.css";
import "./library.css";
import "./gamma-theme.css";
import "./quote.css";
import "./tax.css";
import "./bas-workpaper/bas.css";

export default function ToolsLayout({ children }: { readonly children: ReactNode }) {
  return <>{children}</>;
}
