<script lang="ts">
  import { session } from "../lib/session.svelte";
  import Card from "./Card.svelte";
  let token = $state("");

  function submit(e: SubmitEvent) {
    e.preventDefault();
    if (token.trim()) session.signIn(token);
    token = "";
  }
</script>

<Card title="Operator token">
  <form onsubmit={submit}>
    <input type="password" bind:value={token} autocomplete="current-password" placeholder="admintoken from config.yml" aria-label="Operator token" />
    <button type="submit">Sign in</button>
  </form>
</Card>

<style>
  form { display: flex; gap: 10px; }
  input { flex: 1; min-width: 0; }
</style>
