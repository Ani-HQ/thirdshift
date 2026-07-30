import type { Metadata } from "next";
import { PublicStatusPage } from "../../components/PublicStatusPage";

export const metadata: Metadata = {
  title: "Thirdshift — open models on idle gaming PCs",
  description: "Model catalog, alpha pricing, and live network status for the Thirdshift community night network"
};

export default function Status() {
  return <PublicStatusPage />;
}
