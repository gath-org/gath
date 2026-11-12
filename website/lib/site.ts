/** Public GitHub repository. */
export const REPO_URL = "https://github.com/gath-org/gath";
export const REPO_LABEL = REPO_URL.replace(/^https:\/\//, "");

/** GitHub Pages serves this project site under /gath/. Empty for a local preview at /. */
export const BASE_PATH = process.env.NEXT_PUBLIC_BASE_PATH ?? "";

export function asset(path: string): string {
  const p = path.startsWith("/") ? path : `/${path}`;
  return `${BASE_PATH}${p}`;
}
