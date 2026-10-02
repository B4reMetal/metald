/// <reference types="vitest/config" />
import { resolve } from "node:path";
import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// Two pages (listen, songs) built as static files into contrib/radio/www; relative asset paths, so the
// proxy can serve them at a site root or under a path. `npm run dev` proxies the API, the streams and
// downloads to RADIO_ORIGIN, the live radio.
const origin = process.env.RADIO_ORIGIN ?? "https://radio.example.com";

export default defineConfig({
  plugins: [svelte()],
  base: "./",
  build: {
    outDir: "../../contrib/radio/www",
    emptyOutDir: true,
    rollupOptions: { input: { index: resolve(__dirname, "index.html"), songs: resolve(__dirname, "songs.html") } },
  },
  server: { proxy: Object.fromEntries(["/api", "/hls", "/live", "/dl"].map((p) => [p, { target: origin, changeOrigin: true }])) },
  test: { environment: "node" },
});
