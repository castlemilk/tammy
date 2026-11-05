import type { ReactNode } from "react";
import "./tools.css";
import "./library.css";
import "./quote.css";
import "./tax.css";
import "./bas-workpaper/bas.css";
import "../../components/gamma/segmented-control.css";

export default function ToolsLayout({ children }: { readonly children: ReactNode }) {
  return <>{children}</>;
}
