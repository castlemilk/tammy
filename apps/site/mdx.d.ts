declare module "*.mdx" {
  import type { ComponentType, ElementType } from "react";
  const MDXContent: ComponentType<{ components?: Record<string, ElementType> }>;
  export default MDXContent;
}
