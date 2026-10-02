import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
import fs from "node:fs";
import path from "node:path";

export default defineConfig({
  publicDir: false,
  define: { "process.env.NODE_ENV": JSON.stringify("production") },
  resolve: {
    alias: [
      {
        find: "./environment",
        replacement: path.resolve("src/plugin/environment.ts"),
      },
    ],
  },
  plugins: [
    react(),
    {
      name: "self-contained-mcp-panel",
      closeBundle() {
        const directory = path.resolve("../../internal/mcpassets/dist");
        const js = fs
          .readFileSync(path.join(directory, "panel.js"), "utf8")
          .replaceAll("</script", "<\\/script");
        const css = fs.readFileSync(path.join(directory, "panel.css"), "utf8");
        fs.writeFileSync(
          path.join(directory, "panel.html"),
          `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light dark"><title>Team Cross</title><style>${css}</style></head><body><div id="root"></div><script type="module">${js}</script></body></html>`,
        );
        fs.unlinkSync(path.join(directory, "panel.js"));
        fs.unlinkSync(path.join(directory, "panel.css"));
      },
    },
  ],
  build: {
    target: "esnext",
    minify: "esbuild",
    outDir: "../../internal/mcpassets/dist",
    emptyOutDir: true,
    lib: {
      entry: "src/main.tsx",
      formats: ["es"],
      fileName: () => "panel.js",
      cssFileName: "panel",
    },
    rollupOptions: { output: { inlineDynamicImports: true } },
  },
});
