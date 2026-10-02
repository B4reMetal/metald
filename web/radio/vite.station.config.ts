import { resolve } from "node:path";
import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// The private station's page (contrib/station), built into contrib/station/www. It shares this
// project's themes, lyrics and visualizers. `npm run dev:station` proxies the API and the HLS stream
// to STATION_ORIGIN, signing in as STATION_AUTH ("user:password").
const origin = process.env.STATION_ORIGIN ?? "https://station.example.com";
const auth = process.env.STATION_AUTH ? { Authorization: "Basic " + Buffer.from(process.env.STATION_AUTH).toString("base64") } : undefined;

export default defineConfig({
  plugins: [svelte()],
  root: resolve(import.meta.dirname, "station"),
  publicDir: resolve(import.meta.dirname, "public"),
  base: "./",
  build: { outDir: resolve(import.meta.dirname, "../../contrib/station/www"), emptyOutDir: true, chunkSizeWarningLimit: 2000 },
  server: { proxy: Object.fromEntries(["/api", "/hls"].map((p) => [p, { target: origin, changeOrigin: true, headers: auth }])) },
});
