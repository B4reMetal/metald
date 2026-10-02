import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// The build lands where the Go package embeds it. `npm run dev` proxies the API to a running bot
// (set ADMIN_API, e.g. http://127.0.0.1:8766).
export default defineConfig({
  plugins: [svelte()],
  base: "./",
  build: { outDir: "../../internal/admin/dist", emptyOutDir: true, assetsInlineLimit: 0 },
  server: { proxy: { "/api": process.env.ADMIN_API ?? "http://127.0.0.1:8766" } },
});
