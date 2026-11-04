import type { ReactNode } from "react";
import "../tools/tools.css";
import "./blog.css";
import "./mdx.css";

export default function BlogLayout({ children }: { readonly children: ReactNode }) {
  return <>{children}</>;
}
