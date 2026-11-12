import type { NextConfig } from "next";

const basePath = process.env.GITHUB_PAGES === "1" ? "/gath" : "";

const nextConfig: NextConfig = {
  output: "export",
  images: { unoptimized: true },
  trailingSlash: process.env.GITHUB_PAGES === "1",
  basePath,
  assetPrefix: basePath || undefined,
  env: {
    NEXT_PUBLIC_BASE_PATH: basePath,
  },
  poweredByHeader: false,
};

export default nextConfig;
