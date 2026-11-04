declare module "*.mdx" {
  import type { ComponentType } from "react";
  const MDXContent: ComponentType<{
    components?: Record<string, React.ComponentType<any> | keyof React.JSX.IntrinsicElements>;
  }>;
  export default MDXContent;
}
