import type { ReactNode } from "react";
import "./tools.css";
import "./library.css";
import "./quote.css";

export default function ToolsLayout({ children }: { readonly children: ReactNode }) {
  return <>{children}</>;
}
