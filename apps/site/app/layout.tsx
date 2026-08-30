import type { Metadata } from 'next';
import { publicContent } from '../content/public-content.generated';
import './globals.css';

const { identity } = publicContent;

export const metadata: Metadata = {
  title: `${identity.appStoreName} — Local accounting for Australia`,
  description: `${identity.installedName} is private desktop accounting software for Australian company EOFY and tax return preparation.`,
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en-AU">
      <body>{children}</body>
    </html>
  );
}
