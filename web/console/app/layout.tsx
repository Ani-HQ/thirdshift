import type { Metadata } from "next";
import { GoogleAnalytics } from "../components/GoogleAnalytics";
import "./globals.css";

const siteUrl = "https://thirdshift.ani.computer";

export const metadata: Metadata = {
  metadataBase: new URL(siteUrl),
  title: {
    default: "Thirdshift",
    template: "%s · Thirdshift"
  },
  description: "Limitless AI in an AI-less world.",
  verification: {
    google: process.env.NEXT_PUBLIC_GOOGLE_SITE_VERIFICATION || undefined
  }
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <head>
        <GoogleAnalytics />
      </head>
      <body>{children}</body>
    </html>
  );
}
