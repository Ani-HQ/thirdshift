import type { Metadata } from "next";
import { PublicStatusPage } from "../../components/PublicStatusPage";

export const metadata: Metadata = {
  title: "Thirdshift — Limitless AI in an AI-less world",
  description:
    "A community network for affordable open-model AI outside the bubble. Open models on idle gaming PCs."
};

export default function Status() {
  return <PublicStatusPage />;
}
