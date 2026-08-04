import type { Metadata } from "next";
import { AsciiLander } from "../components/AsciiLander";

export const metadata: Metadata = {
  title: "Thirdshift",
  description: "Limitless AI in an AI-less world."
};

export default function Page() {
  return <AsciiLander />;
}
