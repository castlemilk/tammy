import type { Metadata } from 'next';
import { publicContent } from '../content/public-content.generated';
import './globals.css';

const { identity } = publicContent;

export const metadata: Metadata = {
  title: `${identity.appStoreName} — Local accounting for Australia`,
  description: `${identity.installedName} is private desktop accounting software for encrypted workspaces, journals, source-document review, bank reconciliation, and local GST workpapers.`,
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
