import type { Metadata } from "next";
import { publicContent } from "../content/public-content.generated";
import "./globals.css";

const { identity } = publicContent;
const publicOrigin = "https://tammy-accounting.castlemilk.chatgpt.site";
const socialImage = `${publicOrigin}/og.png`;

export const metadata: Metadata = {
  metadataBase: new URL(publicOrigin),
  title: {
    default: `${identity.appStoreName} — Local accounting for Australia`,
    template: `%s — ${identity.appStoreName}`,
  },
  description: `${identity.installedName} is private desktop accounting software for encrypted workspaces, journals, source-document review, bank reconciliation, and local GST workpapers.`,
  openGraph: {
    type: "website",
    locale: identity.locale.replace("-", "_"),
    url: publicOrigin,
    siteName: identity.appStoreName,
    title: `${identity.appStoreName} — Local accounting for Australia`,
    description: `${identity.installedName} is private desktop accounting software for Australian businesses.`,
    images: [
      {
        url: socialImage,
        width: 1732,
        height: 908,
        alt: `${identity.appStoreName} — Local accounting for Australia`,
      },
    ],
  },
  twitter: {
    card: "summary_large_image",
    title: `${identity.appStoreName} — Local accounting for Australia`,
    description: `${identity.installedName} is private desktop accounting software for Australian businesses.`,
    images: [socialImage],
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang={identity.locale}>
      <body>{children}</body>
    </html>
  );
}
