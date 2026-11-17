import type { Metadata, Viewport } from "next";
import localFont from "next/font/local";
import "./globals.css";

const inter = localFont({
  src: "../public/fonts/inter-var.woff2",
  weight: "100 900",
  display: "swap",
  variable: "--font-sans",
});

const mono = localFont({
  src: "../public/fonts/plex-mono-light.woff2",
  weight: "300",
  display: "swap",
  variable: "--font-mono",
});

export const metadata: Metadata = {
  title: "Gath — a proof-of-work currency",
  description:
    "Gath is a proof-of-work currency. This repository is the full node, the CPU miner, the command-line wallet, and a terminal interface. It is no longer maintained, and the public seed node is offline.",
};

export const viewport: Viewport = {
  themeColor: "#050505",
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en-GB" className={`${inter.variable} ${mono.variable}`}>
      <body>{children}</body>
    </html>
  );
}
