import { prefs } from "./prefs.svelte";
import { applyPalette, palette, themeById, type Palette } from "./themes";

/** The colours in force: the chosen theme, in its light or dark variant to match the system. */
class Appearance {
  prefersDark = $state(typeof matchMedia !== "undefined" && matchMedia("(prefers-color-scheme: dark)").matches);
  theme = $derived(themeById(prefs.theme));
  palette: Palette = $derived(palette(this.theme, this.prefersDark));
  /** Spectrum colours, top first. */
  spectrum: string[] = $derived(this.theme.spectrum ?? [this.palette.accent2, this.palette.accent]);

  /** Keep the page's colours and the system setting in step; call once from the root component. */
  follow(): () => void {
    const mq = matchMedia("(prefers-color-scheme: dark)");
    const onChange = (e: MediaQueryListEvent) => { this.prefersDark = e.matches; };
    mq.addEventListener("change", onChange);
    const stop = $effect.root(() => {
      $effect(() => applyPalette(this.palette));
      $effect(() => {
        if (this.theme.look) document.documentElement.dataset.look = this.theme.look;
        else delete document.documentElement.dataset.look;
      });
    });
    return () => { mq.removeEventListener("change", onChange); stop(); };
  }
}

export const appearance = new Appearance();
