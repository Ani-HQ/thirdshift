import { dirname } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));

const nextConfig = {
  basePath: process.env.NEXT_PUBLIC_BASE_PATH || "/internal-console",
  output: "standalone",
  outputFileTracingRoot: __dirname
};

export default nextConfig;
