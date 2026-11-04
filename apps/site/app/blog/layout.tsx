import type { ReactNode } from "react";
import "../tools/tools.css";
import "./blog.css";

export default function BlogLayout({ children }: { readonly children: ReactNode }) {
  return <>{children}</>;
}
