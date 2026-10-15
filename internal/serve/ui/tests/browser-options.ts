import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { resolve } from "node:path";

// Use an installed headless shell: desktop Chrome can hang during shutdown on macOS.
const cache = process.env.PLAYWRIGHT_BROWSERS_PATH ?? resolve(homedir(), "Library/Caches/ms-playwright");
const shell = process.platform === "darwin" && existsSync(cache)
  ? [...new Bun.Glob("chromium_headless_shell-*/**/{headless_shell,chrome-headless-shell}").scanSync({ cwd: cache, absolute: true })]
    .sort((left, right) => left.localeCompare(right, undefined, { numeric: true })).at(-1)
  : undefined;

export const chromeLaunchOptions = {
  ...(shell ? { executablePath: shell } : { channel: "chrome" }),
  headless: true,
  args: ["--headless=new"],
  ignoreDefaultArgs: ["--headless=old", "--use-angle"],
};
