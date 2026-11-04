import type { ReactNode } from "react";
import "./tools.css";
import "./library.css";

export default function ToolsLayout({ children }: { readonly children: ReactNode }) {
  return <>{children}</>;
}
