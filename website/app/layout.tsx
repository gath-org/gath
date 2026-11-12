import type { Metadata, Viewport } from "next";
import localFont from "next/font/local";
import "./globals.css";

const inter = localFont({
  src: "../public/fonts/inter-var.woff2",
  weight: "100 900",
  display: "swap",
  variable: "--font-gath-inter",
});

const mono = localFont({
  src: "../public/fonts/plex-mono-light.woff2",
  weight: "300",
  display: "swap",
  variable: "--font-gath-mono",
});

export const metadata: Metadata = {
  title: "Gath — a standalone proof-of-work currency",
  description:
    "Gath is open-source software for a standalone proof-of-work currency. No issuer, no admin key, no premine. In development; not a public network yet.",
};

export const viewport: Viewport = {
  themeColor: "#000000",
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
};

// Runs before paint: marks JS as present so the film can start from black.
const boot = `(function(){var d=document.documentElement;d.classList.add('js');})();`;

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={`${inter.variable} ${mono.variable}`}>
      <head>
        <script dangerouslySetInnerHTML={{ __html: boot }} />
      </head>
      <body>{children}</body>
    </html>
  );
}
