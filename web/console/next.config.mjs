import { dirname } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));

const configuredBasePath = process.env.NEXT_PUBLIC_BASE_PATH || "";

const nextConfig = {
  output: "standalone",
  outputFileTracingRoot: __dirname
};

if (configuredBasePath) {
  nextConfig.basePath = configuredBasePath;
}

export default nextConfig;
