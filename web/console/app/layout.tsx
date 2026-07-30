import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Thirdshift Console",
  description: "Thirdshift alpha operator console and public network status"
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
