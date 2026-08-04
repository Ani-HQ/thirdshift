import type { MetadataRoute } from "next";

const siteUrl = "https://thirdshift.ani.computer";

export default function sitemap(): MetadataRoute.Sitemap {
  return [
    {
      url: `${siteUrl}/status`,
      lastModified: new Date(),
      changeFrequency: "daily",
      priority: 1
    },
    {
      url: siteUrl,
      lastModified: new Date(),
      changeFrequency: "weekly",
      priority: 0.6
    }
  ];
}
