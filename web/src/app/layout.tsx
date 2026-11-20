import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "doorkey APM",
  description: "Latency, errors, fallbacks and cost for the doorkey LLM gateway.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className="h-full antialiased">
      <body className="min-h-full">{children}</body>
    </html>
  );
}
