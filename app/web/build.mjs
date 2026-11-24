import { copyFileSync, mkdirSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import esbuild from "esbuild";

const require = createRequire(import.meta.url);
const highlightsDir = path.dirname(require.resolve("@pierre/highlights/package.json"));

/**
 * Strips unused Shiki grammars/themes/WASM and non-Stratum highlight themes
 * from the single-file IIFE bundle when using @pierre/diffs with the
 * "highlights" backend and pierre-dark / pierre-light themes.
 */
const trimHighlighterPlugin = {
  name: "trim-unused-highlighters",
  setup(build) {
    build.onResolve({ filter: /^@pierre\/highlights\/themes\/loader$/ }, (args) => ({
      path: args.path,
      namespace: "pierre-theme-subset",
    }));

    build.onLoad({ filter: /.*/, namespace: "pierre-theme-subset" }, () => ({
      contents: `
        export const themes = {
          "pierre-dark": () => import("@pierre/highlights/themes/pierre-dark"),
          "pierre-light": () => import("@pierre/highlights/themes/pierre-light"),
        };
      `,
      loader: "js",
      resolveDir: highlightsDir,
    }));

    build.onResolve(
      { filter: /^(shiki(\/.*)?|@pierre\/theming\/themes)$/ },
      (args) => ({
        path: args.path,
        namespace: "stub-shiki",
      }),
    );

    build.onResolve({ filter: /^\.\/backends\/shiki\.js$/ }, (args) => {
      if (args.importer.includes("@pierre/diffs")) {
        return { path: args.path, namespace: "stub-shiki" };
      }
      return undefined;
    });

    build.onLoad({ filter: /.*/, namespace: "stub-shiki" }, () => ({
      contents: `
        export const bundledLanguages = {};
        export const themes = { getTheme() { return undefined; } };
        export class ShikiHighlighter {}
        export function normalizeTheme(t) { return t; }
        export function createCssVariablesTheme(t) { return t; }
        export default {};
      `,
      loader: "js",
    }));
  },
};

mkdirSync("dist", { recursive: true });

const metafileArg = process.argv.find((arg) => arg.startsWith("--metafile="));
const result = await esbuild.build({
  entryPoints: ["src/main.ts"],
  bundle: true,
  format: "iife",
  minify: true,
  outfile: "dist/bundle.js",
  metafile: Boolean(metafileArg),
  plugins: [trimHighlighterPlugin],
});

if (metafileArg && result.metafile) {
  const { writeFileSync } = await import("node:fs");
  writeFileSync(metafileArg.slice("--metafile=".length), JSON.stringify(result.metafile, null, 2));
}

copyFileSync("src/index.html", "dist/index.html");
copyFileSync("src/style.css", "dist/style.css");
