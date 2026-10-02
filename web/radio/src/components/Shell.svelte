<script lang="ts">
  import type { Snippet } from "svelte";
  import { appearance } from "../lib/appearance.svelte";
  import Settings from "./Settings.svelte";
  let { page, children, visualizers }: {
    page: "listen" | "songs" | "station"; children: Snippet; visualizers?: { id: string; label: string }[];
  } = $props();
  $effect(() => appearance.follow());
</script>

<main>
  <header>
    <img class="logo" src="favicon.svg" alt="" />
    <nav>
      {#if page === "station"}
        <span class="on">Station</span>
      {:else}
      <a href="./" class:on={page === "listen"} aria-current={page === "listen" ? "page" : undefined}>Radio</a>
      <a href="songs.html" class:on={page === "songs"} aria-current={page === "songs" ? "page" : undefined}>Songs</a>
      {/if}
    </nav>
    <Settings {visualizers} />
  </header>
  {@render children()}
</main>

<style>
  main { max-width: 680px; margin: 0 auto; padding: 28px 16px 64px; }
  header { display: flex; align-items: center; gap: 10px; margin-bottom: 20px; }
  .logo { width: 28px; height: 28px; }
  nav { flex: 1; display: flex; gap: 14px; }
  nav a, nav span { font-size: 14px; letter-spacing: .12em; text-transform: uppercase; color: var(--muted); text-decoration: none; font-weight: 600; }
  nav .on, nav a:hover { color: var(--fg); }
</style>
