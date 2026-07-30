import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Thirdshift Console",
  description: "Internal Thirdshift alpha operator console"
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
